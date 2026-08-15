package comp

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// BodyWidth is as wide as prose is allowed to get. A body running the full width
// of a modern terminal is a line the eye loses its place in on the way back.
const BodyWidth = 80

// Wrap is a body as the rows it draws as: paragraphs folded to width, blank
// lines kept, and every line's own indent put back on the runs it folded into.
func Wrap(body string, width int) []string {
	var out []string
	for _, block := range blocks(body) {
		if block == "" {
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

// blocks splits a body into the runs the layout treats as one thing. Consecutive
// lines join, or a body hard-wrapped at another width sheds a word per line.
func blocks(body string) []string {
	var out []string
	var para []string

	flush := func() {
		if len(para) > 0 {
			out, para = append(out, strings.Join(para, " ")), nil
		}
	}

	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			flush()
			out = append(out, "")
		case opens(line):
			flush()
			out = append(out, line)
		default:
			para = append(para, line)
		}
	}

	flush()
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

// opens reports a line that begins something: an indent, a bullet, a number, a
// quote, a heading, a fence. Folding one into the paragraph above eats a list.
func opens(line string) bool {
	if line != strings.TrimLeft(line, " \t") {
		return true
	}
	for _, mark := range []string{"- ", "* ", "+ ", "> ", "#", "```"} {
		if strings.HasPrefix(line, mark) {
			return true
		}
	}
	return counted(line)
}

// counted reports an ordered list marker: digits, then a dot or a bracket, then
// a space.
func counted(line string) bool {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return false
	}
	return (line[i] == '.' || line[i] == ')') && line[i+1] == ' '
}
