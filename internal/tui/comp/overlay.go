package comp

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/theme"
)

// Over composites over on top of base, centred in a frame of the given size.
//
// The compositor works on a cell buffer rather than joining strings, which is
// what keeps the layer beneath from showing through the gaps in the one on top.
// Canvas.Compose looks like the same thing and is not: it ignores a layer's
// position and draws every layer at the origin.
//
// This is zen-octo's, so a dialog lands the same way in both tools.
func Over(base, over string, width, height int) string {
	if width <= 0 || height <= 0 {
		return base
	}

	// An overlay larger than the frame would otherwise grow the composite past
	// the terminal, which is the one thing the frame must never do.
	over = lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(over)

	x := max(0, (width-lipgloss.Width(over))/2)
	y := max(0, (height-lipgloss.Height(over))/2)

	out := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(over).X(x).Y(y).Z(1),
	).Render()

	// The compositor trims each line's trailing spaces, so a base line ending in
	// padding rather than in a border rune comes back short and the frame no
	// longer fills the width it was given. Every pane line ends in a border; the
	// status bar under them does not.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	}
	return strings.Join(lines, "\n")
}

// Modal frames content as a dialog for Over to place. It is a focused pane, so
// a dialog wears the chrome the panes already wear.
//
// It is sized into the frame rather than left to overflow it. Over clips what
// does not fit, and a clipped box loses the border off two of its sides; a pane
// told its own size clips the content instead and stays a box.
//
// This is where it parts from zen-octo's, which takes no frame and overflows.
func Modal(t theme.Theme, title, content string, width, height int) string {
	padded := lipgloss.NewStyle().Padding(0, 1).Render(content)
	w, h := lipgloss.Size(padded)

	return NewPane(t).Title(title).Focus(true).
		Size(min(w+2, width), min(h+2, height)).
		Render(padded)
}
