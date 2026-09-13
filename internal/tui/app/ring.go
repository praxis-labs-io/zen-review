package app

import (
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// stop names a hunk by side and first line rather than position, so an inserted hunk cannot shift it onto other code.
type stop struct {
	path  string
	side  store.Side
	line  int
	state review.State
}

func (s stop) unread() bool { return s.state != review.Reviewed }

func (s stop) same(o stop) bool {
	return s.path == o.path && s.side == o.side && s.line == o.line
}

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

func at(stops []stop, cur stop) int {
	for i, s := range stops {
		if s.same(cur) {
			return i
		}
	}
	return 0
}

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

// onward does not wrap, so one held mark key cannot claim the whole changeset was read.
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

func wrap(i, n int) int { return ((i % n) + n) % n }

// file is not ring with a path test, which stepping back would land on the previous file's last hunk.
func (m Model) file(by int) (stop, bool) {
	s, ok := m.ring(by, func(s stop) bool { return s.path != m.cursor.path })
	if !ok {
		return stop{}, false
	}
	return m.firstOf(s.path)
}

func (m *Model) land(s stop) {
	m.cursor = s
	m.tree.Select(s.path)

	if s.path != m.diff.Path() {
		m.diff.SetFile(m.fileAt(s.path), m.comments, m.replaced, m.gen.ID)
	}
	m.diff.Select(s.side, s.line)
}

func (m Model) unresolved() []store.Comment {
	out := make([]store.Comment, 0, len(m.comments))
	for _, c := range m.comments {
		if c.State != store.CommentResolved {
			out = append(out, c)
		}
	}
	return out
}

func (m Model) reachable() []store.Comment {
	out := make([]store.Comment, 0, len(m.comments))
	for _, c := range m.unresolved() {
		if m.fileOwning(c) != nil {
			out = append(out, c)
		}
	}
	return out
}

func (m Model) commentRing(by int) (store.Comment, bool) {
	all := m.reachable()
	if len(all) == 0 {
		return store.Comment{}, false
	}

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

	if _, _, ok := m.diff.Hunk(); ok {
		m.syncCursor()
		return
	}

	if s, ok := m.firstOf(f.Diff.Path); ok {
		m.cursor = s
	}
}

func (m *Model) fileOwning(c store.Comment) *review.File {
	for i := range m.changeset.Files {
		if m.changeset.Files[i].Owns(c) {
			return &m.changeset.Files[i]
		}
	}
	return nil
}

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

func (m Model) firstOf(path string) (stop, bool) {
	for _, s := range m.stops() {
		if s.path == path {
			return s, true
		}
	}
	return stop{}, false
}

func anyStop(stop) bool { return true }

func unreadStop(s stop) bool { return s.unread() }
