package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

type resolvedMsg struct{ r Reload }

func (m Model) settling() (string, bool) {
	id, on := m.diff.Comment()
	if !on {
		return "", false
	}

	for _, c := range m.unresolved() {
		if c.ID == id {
			return id, true
		}
	}
	return "", false
}

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

func (m Model) answered() string {
	total := len(m.comments)
	return strconv.Itoa(total-len(m.unresolved())) + "/" + strconv.Itoa(total) + " settled"
}
