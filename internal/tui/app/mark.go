package app

import (
	"errors"
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/zen-review/zen-review/internal/review"
)

// wroteMsg is a write that landed, carrying the changeset review derived after
// it. advance is the r key asking for the next unread hunk.
type wroteMsg struct {
	r       Reload
	advance bool
}

// staleMsg is a write refused because a refresh landed first. Nothing was
// written and nothing was lost.
type staleMsg struct{ seq int }

// writeFailedMsg is a write that did not happen. The changeset is left alone.
type writeFailedMsg struct{ err error }

// marking is the write a mark key asks for, or false when the cursor has
// nothing under it. A file with no hunks is one stop and is marked whole.
func (m Model) marking(whole, undo bool) (func(Source) (Reload, error), bool) {
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
func (m Model) marked(msg tea.KeyPressMsg) (do func(Source) (Reload, error), advance, mine bool) {
	var whole, undo bool
	switch {
	case key.Matches(msg, m.keys.Mark):
		advance = true
	case key.Matches(msg, m.keys.MarkFile):
		whole = true
	case key.Matches(msg, m.keys.Unmark):
		undo = true
	case key.Matches(msg, m.keys.UnmarkFile):
		whole, undo = true, true
	default:
		return nil, false, false
	}

	do, ok := m.marking(whole, undo)
	if !ok {
		return nil, false, true
	}
	return do, advance, true
}

// applyWrite puts a write on screen, then moves on if the key asked to. The
// advance runs after, so it sees what the mark did rather than what was there.
func (m *Model) applyWrite(msg wroteMsg) {
	m.apply(msg.r)

	if msg.advance {
		if s, ok := m.ring(1, unreadStop); ok {
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
func (m Model) write(do func(Source) (Reload, error), advance bool) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		r, err := do(src)
		if err == nil {
			return wroteMsg{r: r, advance: advance}
		}

		// A refresh landing mid-press is not a failure. It is answered with the
		// reload key rather than by pressing the same one again.
		var stale *review.StaleGenerationError
		if errors.As(err, &stale) {
			return staleMsg{seq: stale.Current}
		}
		return writeFailedMsg{err: err}
	}
}
