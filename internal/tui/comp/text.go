package comp

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// BodyWidth is the widest prose is drawn.
const BodyWidth = 80

// Wrap folds each line of body to width, keeping typed breaks and repeating an indent on each fold.
// A word wider than width overhangs rather than breaking.
func Wrap(body string, width int) []string {
	var out []string
	for _, block := range strings.Split(body, "\n") {
		if strings.TrimSpace(block) == "" {
			out = append(out, "")
			continue
		}

		lead := block[:len(block)-len(strings.TrimLeft(block, " \t"))]
		for _, line := range fold(block[len(lead):], max(width-lipgloss.Width(lead), 1)) {
			out = append(out, lead+line)
		}
	}
	return out
}

func fold(line string, width int) []string {
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
