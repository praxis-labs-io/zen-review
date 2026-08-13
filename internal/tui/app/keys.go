package app

import (
	"charm.land/bubbles/v2/key"
)

// KeyMap is what the root answers to, whichever pane has focus.
type KeyMap struct {
	Left  key.Binding
	Right key.Binding
	Help  key.Binding
	Close key.Binding
	Quit  key.Binding
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		// The digits are the badges the panes carry in their borders. They join
		// the bindings that already move focus rather than being declared beside
		// them, so the help lists one entry per pane.
		Left:  key.NewBinding(key.WithKeys("h", "left", "1"), key.WithHelp("h/1", "tree pane")),
		Right: key.NewBinding(key.WithKeys("l", "right", "2"), key.WithHelp("l/2", "diff pane")),
		Help:  key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Close: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close help")),
		Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp is the line on the status bar. It stays short enough to survive a
// narrow terminal, where the rest is one keypress away.
func (m Model) ShortHelp() []key.Binding {
	return []key.Binding{m.keys.Help, m.keys.Quit}
}

// FullHelp is the overlay, in columns.
//
// The bindings come off the pane that would match them rather than out of a
// second list, so a key cannot be shown under text it no longer does.
func (m Model) FullHelp() [][]key.Binding {
	panes := []key.Binding{m.keys.Left, m.keys.Right}

	movement := m.diff.Keys.Bindings()
	if m.focus == focusTree {
		movement = m.tree.Keys.Movement.Bindings()
		panes = append(panes, m.tree.Keys.Bindings()...)
	}

	return [][]key.Binding{
		movement,
		panes,
		{m.keys.Help, m.keys.Quit},
	}
}
