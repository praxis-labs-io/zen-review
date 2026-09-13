package comp

import (
	"slices"

	"charm.land/bubbles/v2/key"
)

// Movement is the row-at-a-time keys every pane shares. The half-page keys belong to the diff pane.
type Movement struct {
	Up     key.Binding
	Down   key.Binding
	Top    key.Binding
	Bottom key.Binding
}

func NewMovement() Movement {
	return Movement{
		Up:     key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
		Top:    key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom: key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
	}
}

// Pair joins two opposite bindings into one status bar entry shown as label.
func Pair(a, b key.Binding, label, desc string) key.Binding {
	return key.NewBinding(
		key.WithKeys(slices.Concat(a.Keys(), b.Keys())...),
		key.WithHelp(label, desc),
	)
}

// Bindings returns the movement keys in the order help lists them.
func (m Movement) Bindings() []key.Binding {
	return []key.Binding{m.Down, m.Up, m.Top, m.Bottom}
}
