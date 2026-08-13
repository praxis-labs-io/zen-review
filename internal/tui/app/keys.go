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

// ShortHelp is the line on the status bar: what the pane holding the keys can
// do, then the two that answer from anywhere.
//
// It changes with focus, because the point of the line is what the next press
// would do. The rest is one keypress away.
func (m Model) ShortHelp() []key.Binding {
	return append(m.paneKeys(), m.wayOut()...)
}

// paneKeys is what the pane holding the keys can do. The bar drops from the
// tail, so the last of these is the first to go.
//
// The paging keys are last. They are the ones a reader would not guess, which
// argues for keeping them, but the bar only runs short on a terminal narrow
// enough that the diff pane is a sliver, and paging a sliver is the least of
// what the reader wants there.
func (m Model) paneKeys() []key.Binding {
	own := m.diff.Keys.Hints()
	if m.focus == focusTree {
		own = m.tree.Keys.Hints()
	}
	return append(own, m.diff.Keys.Paging())
}

// wayOut is the two the bar never drops. They are the only thing on screen
// saying the overlay exists, and the reader who needs that is the one on the
// terminal too narrow for the rest.
func (m Model) wayOut() []key.Binding {
	return []key.Binding{m.keys.Help, m.keys.Quit}
}

// FullHelp is the overlay, in columns.
//
// The bindings come off the pane that would match them rather than out of a
// second list, so a key cannot be shown under text it no longer does.
func (m Model) FullHelp() [][]key.Binding {
	panes := []key.Binding{m.keys.Left, m.keys.Right}

	// The diff pane adds nothing to the pane column: everything it answers to is
	// movement. The tree has keys of its own.
	movement := m.diff.Keys.Bindings()
	if m.focus == focusTree {
		movement = m.tree.Keys.Movement.Bindings()
		panes = append(panes, m.tree.Keys.Bindings()...)
	}

	// The half-page keys are listed under whichever pane has the keys, because
	// they answer from both.
	movement = append(movement, m.diff.Keys.Scrolling()...)

	return [][]key.Binding{
		movement,
		panes,
		{m.keys.Help, m.keys.Quit},
	}
}
