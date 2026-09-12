package comp

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Over centres over on base in a width by height frame, clipping what does not fit.
// Canvas.Compose is not a substitute: it draws every layer at the origin.
func Over(base, over string, width, height int) string {
	if width <= 0 || height <= 0 {
		return base
	}

	over = lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(over)

	x := max(0, (width-lipgloss.Width(over))/2)
	y := max(0, (height-lipgloss.Height(over))/2)

	out := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(over).X(x).Y(y).Z(1),
	).Render()

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	}
	return strings.Join(lines, "\n")
}

// Modal frames content as a focused pane sized to fit width by height, for Over to place.
func Modal(t theme.Theme, title, content string, width, height int) string {
	padded := lipgloss.NewStyle().Padding(0, 1).Render(content)
	w, h := lipgloss.Size(padded)

	return NewPane(t).Title(title).Focus(true).
		Size(min(w+2, width), min(h+2, height)).
		Render(padded)
}
