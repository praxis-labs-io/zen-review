package comp

import "charm.land/bubbles/v2/key"

// Movement is the keys every pane scrolls with.
//
// It is declared here rather than in each pane because a key described in two
// places drifts: the two would answer to the same press and disagree in the
// help a release later.
type Movement struct {
	Up       key.Binding
	Down     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	HalfUp   key.Binding
	HalfDown key.Binding
}

// NewMovement is the bindings and the help text they carry.
func NewMovement() Movement {
	return Movement{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
	}
}

// Bindings is the movement keys in the order the help lists them.
func (m Movement) Bindings() []key.Binding {
	return []key.Binding{m.Down, m.Up, m.HalfDown, m.HalfUp, m.Top, m.Bottom}
}
