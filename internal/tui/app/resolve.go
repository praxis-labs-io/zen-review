package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// resolvedMsg is a comment settled and the changeset re-derived after it, at the
// generation it named. Nothing moved in git, so the reader keeps their row.
type resolvedMsg struct{ r Reload }

// settling is the comment x acts on: the card under the cursor, unless it is one
// already settled, which is where the card stops offering the key too.
func (m Model) settling() (string, bool) {
	id, on := m.diff.Comment()
	if !on {
		return "", false
	}

	// ResolveComment refuses a settled comment and is right to: freezing one
	// twice re-records an anchor that stopped moving a generation ago.
	for _, c := range m.unresolved() {
		if c.ID == id {
			return id, true
		}
	}
	return "", false
}

// resolve is the write x asks for, against the card the cursor is on.
func (m *Model) resolve(id string) tea.Cmd {
	src, g := m.src, m.gen
	m.busy = true

	return func() tea.Msg {
		r, err := src.ResolveComment(g, id)
		if err != nil {
			return failed(err)
		}
		return resolvedMsg{r: r}
	}
}

// answered is how far down the comments the write left the reader, the way
// progress is how far down the hunks.
func (m Model) answered() string {
	total := len(m.comments)
	return strconv.Itoa(total-len(m.unresolved())) + "/" + strconv.Itoa(total) + " settled"
}
