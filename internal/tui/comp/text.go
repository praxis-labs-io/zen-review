package comp

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// BodyWidth is as wide as prose is allowed to get. A body running the full width
// of a modern terminal is a line the eye loses its place in on the way back.
const BodyWidth = 80

// Wrap is a body as the rows it draws as: every line folded to width, its own
// indent put back on the runs it folded into, and a newline kept as a break.
func Wrap(body string, width int) []string {
	var out []string
	for _, block := range strings.Split(body, "\n") {
		if strings.TrimSpace(block) == "" {
			out = append(out, "")
			continue
		}

		// Taken off once, a long indented line comes back reading as the prose
		// around it, which is the layout blocks kept it separate to preserve.
		lead := block[:len(block)-len(strings.TrimLeft(block, " \t"))]
		for _, line := range fold(block[len(lead):], max(width-lipgloss.Width(lead), 1)) {
			out = append(out, lead+line)
		}
	}
	return out
}

// fold breaks a line into runs no wider than width, on the spaces between words.
// A word too wide overhangs rather than breaking: half a path is worth nothing.
func fold(line string, width int) []string {
	// Cells and not runes. A rune can take two, and counting runes calls a line
	// that renders past the pane a line that fits.
	if lipgloss.Width(line) <= width {
		return []string{line}
	}

	var out []string
	var run string

	for _, word := range strings.Fields(line) {
		switch {
		case run == "":
			run = word
		case lipgloss.Width(run)+1+lipgloss.Width(word) <= width:
			run += " " + word
		default:
			out, run = append(out, run), word
		}
	}
	if run != "" {
		out = append(out, run)
	}
	return out
}
