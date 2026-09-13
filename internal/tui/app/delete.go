package app

import (
	tea "charm.land/bubbletea/v2"
)

type deletedMsg struct{ r Reload }

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
