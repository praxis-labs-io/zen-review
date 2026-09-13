package app

import (
	tea "charm.land/bubbletea/v2"
)

type notedMsg struct{ text string }

const noteTitle = "Session note"

func (m *Model) composing() tea.Cmd {
	return m.compose.Open(noteTitle, m.summary)
}

func (m *Model) saveNote(text string) tea.Cmd {
	src := m.src
	m.busy = true

	return func() tea.Msg {
		stored, err := src.SetSummary(text)
		if err != nil {
			return failed(err)
		}
		return notedMsg{text: stored}
	}
}

func noted(text string) notice {
	if text == "" {
		return notice{text: "note cleared"}
	}
	return notice{text: "note saved"}
}
