package comp

import "charm.land/bubbles/v2/key"

// Movement is the keys every pane moves a row at a time with.
//
// It is declared here rather than in each pane because a key described in two
// places drifts: the two would answer to the same press and disagree in the
// help a release later.
//
// The half-page keys are not here. They page the diff whichever pane has the
// keys, so only the diff pane declares them.
type Movement struct {
	Up     key.Binding
	Down   key.Binding
	Top    key.Binding
	Bottom key.Binding
}

// NewMovement is the bindings and the help text they carry.
func NewMovement() Movement {
	return Movement{
		Up:     key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
		Top:    key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom: key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
	}
}

// Bindings is the movement keys in the order the help lists them.
func (m Movement) Bindings() []key.Binding {
	return []key.Binding{m.Down, m.Up, m.Top, m.Bottom}
}
