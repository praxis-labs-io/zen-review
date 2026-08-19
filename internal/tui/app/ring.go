package app

import (
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// stop is one thing the ring lands on: a hunk, or a whole file that has none.
//
// It names the hunk the way review does, by the side and the first line it
// touches, and never by position. An agent inserting a hunk above the cursor
// would otherwise leave it on different code wearing the same number.
//
// A file with no hunks is one stop with no line. Changeset.Items counts a binary
// file as one item, so a ring that stepped over it would leave n unable to walk
// the burn-down to zero.
type stop struct {
	path  string
	side  store.Side
	line  int
	state review.State
}

// unread is whether this is a stop the n key is looking for.
func (s stop) unread() bool { return s.state != review.Reviewed }

// same is whether two stops name the same hunk. State is not identity: a mark
// changes it, and the hunk the reader was on is still the hunk they were on.
func (s stop) same(o stop) bool {
	return s.path == o.path && s.side == o.side && s.line == o.line
}

// stops is every landing place in the changeset, in the order the tree draws
// them, which is the order review.Derive hands the files back in.
func (m Model) stops() []stop {
	out := make([]stop, 0, m.changeset.Items)
	for _, f := range m.changeset.Files {
		if len(f.Hunks) == 0 {
			out = append(out, stop{path: f.Diff.Path, state: f.State})
			continue
		}
		for _, h := range f.Hunks {
			side, line := h.Name()
			out = append(out, stop{path: f.Diff.Path, side: side, line: line, state: h.State})
		}
	}
	return out
}

// at is where the cursor sits in a list of stops, resolved from its identity
// every time rather than stored. It is 0 for a cursor the changeset no longer
// holds, so a ring key still moves rather than doing nothing.
func at(stops []stop, cur stop) int {
	for i, s := range stops {
		if s.same(cur) {
			return i
		}
	}
	return 0
}

// ring steps from the cursor to the next stop matching want, wrapping. It comes
// back false when there is nowhere to go: an empty changeset, or an n press with
// nothing left unread.
//
// The walk starts one past the cursor and takes in the cursor itself last, so a
// ring of one stop lands back on it rather than reporting no such stop.
func (m Model) ring(by int, want func(stop) bool) (stop, bool) {
	stops := m.stops()
	if len(stops) == 0 {
		return stop{}, false
	}

	from := at(stops, m.cursor)
	for n := 1; n <= len(stops); n++ {
		s := stops[wrap(from+by*n, len(stops))]
		if want(s) {
			return s, true
		}
	}
	return stop{}, false
}

// onward is the next stop matching want after the cursor, and does not wrap.
//
// It is what a mark advances by. The ring wraps because hunting for something
// unread is what n is for; a walk that came back to the top would let one held
// key claim the whole changeset had been read.
func (m Model) onward(want func(stop) bool) (stop, bool) {
	stops := m.stops()
	if len(stops) == 0 {
		return stop{}, false
	}

	for i := at(stops, m.cursor) + 1; i < len(stops); i++ {
		if want(stops[i]) {
			return stops[i], true
		}
	}
	return stop{}, false
}

// wrap is i modulo n, brought back into range from either end. Go's % keeps the
// sign of its left operand, so a step back off the front lands negative.
func wrap(i, n int) int { return ((i % n) + n) % n }

// file steps to the next or previous file and lands on its first hunk.
//
// It is not ring with a path test: stepping back that way finds the previous
// file's last hunk, and a key that moves a whole file should open it at the top
// the same way going forward does.
func (m Model) file(by int) (stop, bool) {
	s, ok := m.ring(by, func(s stop) bool { return s.path != m.cursor.path })
	if !ok {
		return stop{}, false
	}
	return m.firstOf(s.path)
}

// land puts the cursor on a stop: the file into the pane and the tree, and the
// hunk under the pane's own cursor.
//
// The tree follows the pane rather than the other way round, so the two never
// disagree about which file is open. It is selected on every landing and not
// only when the file changes, because the tree's cursor can be somewhere the
// pane is not: a directory row leaves the pane on the file before it.
func (m *Model) land(s stop) {
	m.cursor = s
	m.tree.Select(s.path)

	if s.path != m.diff.Path() {
		m.diff.SetFile(m.fileAt(s.path), m.comments, m.replaced, m.gen.ID)
	}
	m.diff.Select(s.side, s.line)
}

// unresolved is every comment somebody still has to answer, in review's order.
// This ring is the comments' half of the burn-down, the way n is the hunks'.
func (m Model) unresolved() []store.Comment {
	out := make([]store.Comment, 0, len(m.comments))
	for _, c := range m.comments {
		if c.State != store.CommentResolved {
			out = append(out, c)
		}
	}
	return out
}

// reachable is the unresolved comments the changeset still holds a file for. One
// whose file was reverted out is a stop that lands nowhere and never moves past.
func (m Model) reachable() []store.Comment {
	out := make([]store.Comment, 0, len(m.comments))
	for _, c := range m.unresolved() {
		if m.fileOwning(c) != nil {
			out = append(out, c)
		}
	}
	return out
}

// commentRing steps from the comment the cursor is on to the next, wrapping. It
// is false when there is nothing to step to.
func (m Model) commentRing(by int) (store.Comment, bool) {
	all := m.reachable()
	if len(all) == 0 {
		return store.Comment{}, false
	}

	// A cursor on no card at all is most presses of these two keys, and there is
	// no place to step from. Forward takes the first and back takes the last.
	from, on := 0, false
	if id, ok := m.diff.Comment(); ok {
		for i, c := range all {
			if c.ID == id {
				from, on = i, true
				break
			}
		}
	}
	if !on {
		if by > 0 {
			return all[0], true
		}
		return all[len(all)-1], true
	}
	return all[wrap(from+by, len(all))], true
}

// landComment puts the file holding a comment in the pane and the cursor on its
// card. The ring follows the row, the same way it follows j.
func (m *Model) landComment(c store.Comment) {
	f := m.fileOwning(c)
	if f == nil {
		return
	}

	if f.Diff.Path != m.diff.Path() {
		m.diff.SetFile(f, m.comments, m.replaced, m.gen.ID)
	}
	m.tree.Select(f.Diff.Path)
	m.diff.SelectComment(c.ID)

	// A file comment and a stray sit outside every hunk, so there is none under
	// the cursor to follow and the ring takes the file's first instead.
	if _, _, ok := m.diff.Hunk(); ok {
		m.syncCursor()
		return
	}

	// Left alone the ring stays in the file just left, where r marks something
	// nothing on screen names.
	if s, ok := m.firstOf(f.Diff.Path); ok {
		m.cursor = s
	}
}

// fileOwning is the changeset file a comment was written against, and nil when
// the changeset no longer holds it.
func (m *Model) fileOwning(c store.Comment) *review.File {
	for i := range m.changeset.Files {
		if m.changeset.Files[i].Owns(c) {
			return &m.changeset.Files[i]
		}
	}
	return nil
}

// opening is where the reader starts: the first stop that has not been read, or
// the first stop when the whole thing has been.
//
// Vacuously done is still done, and dropping the reader at the top of a finished
// changeset is the same place they would go to check it.
func (m Model) opening() (stop, bool) {
	stops := m.stops()
	if len(stops) == 0 {
		return stop{}, false
	}

	for _, s := range stops {
		if s.unread() {
			return s, true
		}
	}
	return stops[0], true
}

// firstOf is the file's own first stop, which is where the tree's cursor moving
// onto a file puts the ring.
//
// Its first, not its first unread: the reader picked the file to read it, and n
// is the key that walks the burn-down.
func (m Model) firstOf(path string) (stop, bool) {
	for _, s := range m.stops() {
		if s.path == path {
			return s, true
		}
	}
	return stop{}, false
}

// anyStop matches every stop, for the keys that step by hunk rather than by
// what has been read.
func anyStop(stop) bool { return true }

// unreadStop matches a stop the reader has not finished, which is what n walks.
func unreadStop(s stop) bool { return s.unread() }
