package app

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

// commentedMsg is a comment that landed, with the changeset re-derived after it.
type commentedMsg struct{ r Reload }

// editedMsg is one whose words were rewritten. The anchor did not move for it.
type editedMsg struct{ r Reload }

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
	c := boxed(n)
	if cmd, up := m.diff.Compose(c); up {
		return cmd, true
	}

	// No room for it beside the code, so it goes over the frame where C's box
	// is drawn. A box that cannot be drawn at all is a reader typing at air.
	return m.compose.Open(commentTitle(c), ""), true
}

// editOn opens the box over the card the cursor is on, holding what it says. A
// row with no card under it is a press with nothing to do.
func (m *Model) editOn() (tea.Cmd, bool) {
	id, on := m.diff.Comment()
	if !on {
		return nil, false
	}
	c, ok := m.commentAt(id)
	if !ok {
		return nil, false
	}

	m.editing = id
	if cmd, up := m.diff.Edit(c); up {
		return cmd, true
	}
	return m.compose.Open(editTitle(c), c.Body), true
}

// commentAt is one of the session's comments by id, and false for an id nothing
// answers to, an empty one included.
func (m Model) commentAt(id string) (store.Comment, bool) {
	if id == "" {
		return store.Comment{}, false
	}
	for _, c := range m.comments {
		if c.ID == id {
			return c, true
		}
	}
	return store.Comment{}, false
}

// crossOver moves a box the pane has lost the room for onto the frame, with
// what was typed into it. Nothing is lost by a terminal getting smaller.
func (m *Model) crossOver() tea.Cmd {
	if !m.diff.Composing() || m.diff.FitsBox() {
		return nil
	}

	body := m.diff.Draft()
	m.diff.CloseDraft()
	return m.compose.Open(m.boxTitle(), body)
}

// boxTitle names what the open box is scoped to, whichever key opened it.
func (m Model) boxTitle() string {
	if c, ok := m.commentAt(m.editing); ok {
		return editTitle(c)
	}
	return commentTitle(boxed(m.pending))
}

// boxed is the card a note is about to become, which is what the pane draws the
// box as and what the title is read off.
func boxed(n review.Note) store.Comment {
	return store.Comment{
		Path:      n.Path,
		Side:      n.Side,
		Scope:     n.Scope,
		LineRange: store.LineRange{Start: n.Range.Start, End: n.Range.End},
	}
}

func commentTitle(c store.Comment) string { return "Comment on " + where(c) }

func editTitle(c store.Comment) string { return "Edit comment on " + where(c) }

// where says what the words are for, which a box beside the code says by hanging
// there. Only the base is named: head numbers are the gutter's.
func where(c store.Comment) string {
	at := comp.Safe(c.Path)
	if c.Scope != store.ScopeFile {
		at += ":" + strconv.Itoa(c.Start)
		if c.End != c.Start {
			at += "-" + strconv.Itoa(c.End)
		}
	}
	if c.Side == store.SideBase {
		at += " (base)"
	}
	return at
}

// drafting routes a key into the box, answering the two it owns first. Those
// are the composer's: two boxes with one way out of them.
func (m Model) drafting(msg tea.Msg) (tea.Model, tea.Cmd) {
	// One press long, the way it is outside the box. A line that outlived the
	// press that put it there is one more thing to read past mid-sentence.
	if !m.busy {
		m.note = notice{}
	}

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
			// Called before the return, for the reason the resize is: m is written
			// through by save, and the order against it is the spec's to choose.
			cmd := m.save(m.diff.Draft())
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.diff, cmd = m.diff.Update(msg)
	return m, cmd
}

// save is the write the box makes: a rewrite where it is standing in for a card,
// and a new comment where it is not.
func (m *Model) save(body string) tea.Cmd {
	// Trailing whitespace goes, the way it does off stdin: the enter somebody
	// finished on is not a line of the comment, and the card would draw it.
	body = strings.TrimRight(body, " \t\r\n")

	// Wiping a comment is not saving it, and it is not deleting one either: that
	// is a key of its own, and one the box would take as a keystroke anyway.
	if m.editing != "" && strings.TrimSpace(body) == "" {
		m.note = notice{text: "cannot save an empty comment"}
		return nil
	}

	if m.editing != "" {
		return m.saveEdit(body)
	}
	return m.saveComment(body)
}

// saveEdit rewrites what a comment says, which is the whole of an edit: the
// anchor it moves under is not this key's business.
func (m *Model) saveEdit(body string) tea.Cmd {
	src, g, id := m.src, m.gen, m.editing
	m.busy = true

	return func() tea.Msg {
		r, err := src.EditComment(g, id, body)
		if err != nil {
			return failed(err)
		}
		return editedMsg{r: r}
	}
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
