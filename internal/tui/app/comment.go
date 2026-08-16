package app

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// commentedMsg is a comment that landed, with the changeset re-derived after it.
type commentedMsg struct{ r Reload }

// commenting is the comment c scopes, bodyless, and false when the cursor has
// nothing under it. A selection beats the focus, the way esc does.
func (m Model) commenting() (review.Note, bool) {
	if as, on := m.diff.Selected(); on {
		a := head(as)
		return review.NoteOnLines(m.diff.Path(), a.Side, a.Range, ""), true
	}

	if m.focus == focusTree {
		return m.fileNote(m.tree.Path())
	}

	f := m.fileAt(m.diff.Path())
	if f == nil {
		return review.Note{}, false
	}

	// A binary file and a bare rename are one thing to read and one thing to
	// comment on. The pane draws them as a row saying why there is no diff.
	if len(f.Hunks) == 0 {
		return review.NoteOnFile(*f, ""), true
	}

	if as, on := m.diff.Line(); on {
		a := head(as)
		return review.NoteOnLines(f.Diff.Path, a.Side, a.Range, ""), true
	}

	// A heading, a card and the blank between two hunks are all inside a hunk,
	// and the hunk is what the reader is looking at from any of them.
	h, ok := m.hunkAt(*f)
	if !ok {
		return review.Note{}, false
	}
	return review.NoteOnHunk(f.Diff.Path, h, ""), true
}

// fileNote is a comment on the file at a path, and false for a path the
// changeset does not hold, which is what a directory row in the tree is.
func (m Model) fileNote(path string) (review.Note, bool) {
	f := m.fileAt(path)
	if f == nil {
		return review.Note{}, false
	}
	return review.NoteOnFile(*f, ""), true
}

// head is the side a comment anchors to: the head wherever the lines have one,
// because that is the code the next agent rewrites, and the base for a removal.
func head(as []review.Anchor) review.Anchor {
	for _, a := range as {
		if a.Side == store.SideHead {
			return a
		}
	}
	return as[0]
}

// commentOn opens the box where the card will hang. The note is held rather
// than derived again at the save key, so the box and the write name one thing.
func (m *Model) commentOn() (tea.Cmd, bool) {
	n, ok := m.commenting()
	if !ok {
		return nil, false
	}

	m.pending = n
	if cmd, up := m.diff.Compose(store.Comment{
		Side:      n.Side,
		Scope:     n.Scope,
		LineRange: store.LineRange{Start: n.Range.Start, End: n.Range.End},
	}); up {
		return cmd, true
	}

	// No room for it beside the code, so it goes over the frame where C's box
	// is drawn. A box that cannot be drawn at all is a reader typing at air.
	return m.compose.Open(commentTitle(n), ""), true
}

// crossOver moves a box the pane has lost the room for onto the frame, with
// what was typed into it. Nothing is lost by a terminal getting smaller.
func (m *Model) crossOver() tea.Cmd {
	if !m.diff.Composing() || m.diff.FitsBox() {
		return nil
	}

	body := m.diff.Draft()
	m.diff.CloseDraft()
	return m.compose.Open(commentTitle(m.pending), body)
}

// commentTitle says what the words are for, which the box beside the code says
// by hanging there. Only the base is named: head numbers are the gutter's.
func commentTitle(n review.Note) string {
	at := comp.Safe(n.Path)
	if n.Scope != store.ScopeFile {
		at += ":" + strconv.Itoa(n.Range.Start)
		if n.Range.End != n.Range.Start {
			at += "-" + strconv.Itoa(n.Range.End)
		}
	}
	if n.Side == store.SideBase {
		at += " (base)"
	}
	return "Comment on " + at
}

// drafting routes a key into the box, answering the two it owns first. Those
// are the composer's: two boxes with one way out of them.
func (m Model) drafting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		// The way out of anywhere. Raw mode sends no interrupt, so without this
		// the box is the one place where ctrl+c does nothing at all.
		case key.Matches(press, m.keys.Interrupt):
			return m, tea.Quit

		case key.Matches(press, m.compose.Keys.Discard):
			m.shut()
			return m, nil

		case key.Matches(press, m.compose.Keys.Save):
			// Neutral, because busy covers a mark and a resolve as well as a reload.
			if m.busy {
				m.note = notice{text: "still writing"}
				return m, nil
			}
			return m, m.saveComment(m.diff.Draft())
		}
	}

	var cmd tea.Cmd
	m.diff, cmd = m.diff.Update(msg)
	return m, cmd
}

// saveComment writes what was typed. An empty body takes the box down rather
// than writing: the engine refuses one, and nothing typed is nothing to lose.
func (m *Model) saveComment(body string) tea.Cmd {
	if strings.TrimSpace(body) == "" {
		m.shut()
		return nil
	}

	src, g, n := m.src, m.gen, m.pending
	n.Body = body
	m.busy = true

	return func() tea.Msg {
		r, err := src.AddComment(g, n)
		if err != nil {
			return failed(err)
		}
		return commentedMsg{r: r}
	}
}
