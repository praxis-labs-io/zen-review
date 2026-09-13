package app

import (
	"errors"
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
)

type wroteMsg struct {
	r       Reload
	at      stop
	row     int
	advance bool
}

type staleMsg struct{ err error }

type savedMsg struct{ err error }

type intent struct {
	whole bool
	undo  bool
}

func (i intent) advances() bool { return !i.undo }

type writeFailedMsg struct{ err error }

func (m Model) marking(i intent) (func(Source) (Reload, error), bool) {
	whole, undo := i.whole, i.undo

	f := m.fileAt(m.cursor.path)
	if f == nil {
		return nil, false
	}
	g := m.gen

	if whole || len(f.Hunks) == 0 {
		if undo {
			return func(s Source) (Reload, error) { return s.UnmarkFile(g, *f) }, true
		}
		return func(s Source) (Reload, error) { return s.MarkFile(g, *f) }, true
	}

	if m.focus == focusDiff && m.diff.OffHunk() {
		return nil, false
	}

	h, ok := m.hunkAt(*f)
	if !ok {
		return nil, false
	}
	if undo {
		return func(s Source) (Reload, error) { return s.UnmarkHunk(g, f.Diff.Path, h) }, true
	}
	return func(s Source) (Reload, error) { return s.MarkHunk(g, f.Diff.Path, h) }, true
}

func (m Model) marked(msg tea.KeyPressMsg) (intent, bool) {
	switch {
	case key.Matches(msg, m.keys.Mark):
		return intent{}, true
	case key.Matches(msg, m.keys.MarkFile):
		return intent{whole: true}, true
	case key.Matches(msg, m.keys.Unmark):
		return intent{undo: true}, true
	case key.Matches(msg, m.keys.UnmarkFile):
		return intent{whole: true, undo: true}, true
	}
	return intent{}, false
}

// applyWrite advances only from the row r was pressed on, because a reader who moved has chosen where to be.
func (m *Model) applyWrite(msg wroteMsg) {
	m.apply(msg.r)

	if msg.advance && m.cursor.same(msg.at) && m.diff.Cursor() == msg.row {
		if s, ok := m.onward(unreadStop); ok {
			m.land(s)
		}
	}
	m.note = notice{text: m.progress()}
}

func (m Model) progress() string {
	return strconv.Itoa(m.changeset.Reviewed) + "/" + strconv.Itoa(m.changeset.Items) + " read"
}

func (m Model) hunkAt(f review.File) (review.Hunk, bool) {
	for _, h := range f.Hunks {
		if side, line := h.Name(); side == m.cursor.side && line == m.cursor.line {
			return h, true
		}
	}
	return review.Hunk{}, false
}

func (m Model) write(do func(Source) (Reload, error), at stop, row int, advance bool) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		r, err := do(src)
		if err != nil {
			return failed(err)
		}
		return wroteMsg{r: r, at: at, row: row, advance: advance}
	}
}

func failed(err error) tea.Msg {
	var stale *review.StaleGenerationError
	switch {
	case errors.Is(err, ErrSaved):
		return savedMsg{err: err}
	case errors.As(err, &stale):
		return staleMsg{err: err}
	}
	return writeFailedMsg{err: err}
}

func (m *Model) start(i intent) (tea.Cmd, bool) {
	do, ok := m.marking(i)
	if !ok {
		return nil, false
	}
	m.busy = true
	return m.write(do, m.cursor, m.diff.Cursor(), i.advances()), true
}
