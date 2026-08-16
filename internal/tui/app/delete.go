package app

import (
	tea "charm.land/bubbletea/v2"
)

// deletedMsg is a comment removed and the changeset re-derived after it. Nothing
// moved in git, so the reader keeps their row.
type deletedMsg struct{ r Reload }

// deleteComment is the write D asks for, against the card the cursor is on. It
// acts at once: the capital does the whole of the thing, the way R and U do.
func (m *Model) deleteComment(id string) tea.Cmd {
	src, g := m.src, m.gen
	m.busy = true

	return func() tea.Msg {
		r, err := src.DeleteComment(g, id)
		if err != nil {
			return failed(err)
		}
		return deletedMsg{r: r}
	}
}
