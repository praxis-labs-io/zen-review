package app

import (
	"errors"
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/zen-review/zen-review/internal/review"
)

// wroteMsg is a write that landed, with the changeset re-derived after it. at
// and row are where r was pressed, because the reader moves while it is out.
type wroteMsg struct {
	r       Reload
	at      stop
	row     int
	advance bool
}

// staleMsg is a write refused because a refresh landed first. Nothing was
// written and nothing was lost.
type staleMsg struct{ err error }

// intent is a mark key's ask, kept rather than the closure it builds. A press
// held while a write is out is run against the cursor the write left behind.
type intent struct {
	whole bool
	undo  bool
}

// advances is whether this ask moves on afterwards, which only r does.
func (i intent) advances() bool { return !i.whole && !i.undo }

// writeFailedMsg is a write that did not happen. The changeset is left alone.
type writeFailedMsg struct{ err error }

// marking is the write a mark key asks for, or false when the cursor has
// nothing under it. A file with no hunks is one stop and is marked whole.
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

	h, ok := m.hunkAt(*f)
	if !ok {
		return nil, false
	}
	if undo {
		return func(s Source) (Reload, error) { return s.UnmarkHunk(g, f.Diff.Path, h) }, true
	}
	return func(s Source) (Reload, error) { return s.MarkHunk(g, f.Diff.Path, h) }, true
}

// marked is the write a press asks for. The third value says whether the press
// was a mark key at all, which tells "nothing to write" from "not mine".
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

// applyWrite puts a write on screen, then moves on if the key asked to. It
// advances only from the row r was pressed on: a reader who moved has spoken.
func (m *Model) applyWrite(msg wroteMsg) {
	m.apply(msg.r)

	if msg.advance && m.cursor.same(msg.at) && m.diff.Cursor() == msg.row {
		if s, ok := m.onward(unreadStop); ok {
			m.land(s)
		}
	}
	m.note = notice{text: m.progress()}
}

// progress is how far down the burn-down the write left the reader.
func (m Model) progress() string {
	return strconv.Itoa(m.changeset.Reviewed) + "/" + strconv.Itoa(m.changeset.Items) + " read"
}

// hunkAt is the hunk the cursor names, found by identity the way the ring finds
// everything else.
func (m Model) hunkAt(f review.File) (review.Hunk, bool) {
	for _, h := range f.Hunks {
		if side, line := h.Name(); side == m.cursor.side && line == m.cursor.line {
			return h, true
		}
	}
	return review.Hunk{}, false
}

// write runs a write off the update loop, the same way a reload runs. The
// source is lifted out first, because Update goes on writing the model.
func (m Model) write(do func(Source) (Reload, error), at stop, row int, advance bool) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		r, err := do(src)
		if err == nil {
			return wroteMsg{r: r, at: at, row: row, advance: advance}
		}

		// A refresh landing mid-press is not a failure. It is answered with the
		// reload key rather than by pressing the same one again.
		var stale *review.StaleGenerationError
		if errors.As(err, &stale) {
			return staleMsg{err: err}
		}
		return writeFailedMsg{err: err}
	}
}

// start is the command a mark asks for, and false when the cursor names
// nothing to write against.
func (m *Model) start(i intent) (tea.Cmd, bool) {
	do, ok := m.marking(i)
	if !ok {
		return nil, false
	}
	m.busy = true
	return m.write(do, m.cursor, m.diff.Cursor(), i.advances()), true
}
