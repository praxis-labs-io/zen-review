package app

import (
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
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
		m.diff.SetFile(m.fileAt(s.path))
	}
	m.diff.Select(s.side, s.line)
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
