package app

import (
	tea "charm.land/bubbletea/v2"
)

// notedMsg is the session note as review stored it.
type notedMsg struct{ text string }

// noteTitle names the box C opens, which is the only thing on screen saying
// what the words being typed are for.
const noteTitle = "Session note"

// composing is C: the note as it stands, in a box that takes the keys. The
// write is gated at the save key, where there is something to lose.
func (m *Model) composing() tea.Cmd {
	return m.compose.Open(noteTitle, m.summary)
}

// saveNote writes what was typed. The source is lifted out first, the way every
// other write lifts it: Update goes on writing the model.
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

// noted is what the bar says a saved note did. Empty is the only way to take one
// back, so it is a thing that happened rather than a write that did nothing.
func noted(text string) notice {
	if text == "" {
		return notice{text: "note cleared"}
	}
	return notice{text: "note saved"}
}
