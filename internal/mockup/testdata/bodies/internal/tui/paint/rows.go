// Package paint turns one diff line into one row, and holds no model, state or keys.
package paint

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Kind is what a row is painted as, which is not always what the diff called it.
type Kind int

const (
	Context Kind = iota
	Added
	Removed
	Filler
)

const tabWidth = 4

// Row is a painted line, padded to width so the tint reaches the edge of the pane.
type Row struct {
	Kind   Kind
	Number string
	Text   string
	Width  int
}

// Render paints r against t. It is pure: the same row and theme give the same string.
func Render(t theme.Theme, r Row) string {
	style := lipgloss.NewStyle()
	if bg := fill(t, r.Kind); bg != nil {
		style = style.Background(bg)
	}

	text := clip(expand(r.Text), r.Width)
	return style.Render(text + strings.Repeat(" ", max(0, r.Width-lipgloss.Width(text))))
}

func fill(t theme.Theme, k Kind) lipgloss.Color {
	switch k {
	case Added:
		return t.AddedFill
	case Removed:
		return t.RemovedFill
	}
	return nil
}

// expand takes the tabs out, since a raw tab is a variable number of cells and one anywhere in a
// line puts every column after it out of step with the line above.
func expand(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}

	var b strings.Builder
	for _, r := range s {
		if r != '\t' {
			b.WriteRune(r)
			continue
		}
		b.WriteString(strings.Repeat(" ", tabWidth-b.Len()%tabWidth))
	}
	return b.String()
}

// clip cuts before lipgloss can wrap, since Style.Width folds a long line onto a second row.
func clip(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
