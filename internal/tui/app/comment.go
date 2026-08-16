package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// commentedMsg is a comment that landed, with the changeset re-derived after it.
// The body comes back so the box knows whether it is still holding it.
type commentedMsg struct {
	r    Reload
	body string
}

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

// commentOn opens the box over what c scoped. The note is held rather than
// derived again at the save key, so the title and the write name one thing.
func (m *Model) commentOn() (tea.Cmd, bool) {
	n, ok := m.commenting()
	if !ok {
		return nil, false
	}

	m.pending = n
	return m.compose.Open(commentTitle(n), ""), true
}

// commentTitle says what the words are for, in the form the CLI prints a
// location in. Only the base is named: a head anchor's numbers are the gutter's.
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
		return commentedMsg{r: r, body: body}
	}
}
