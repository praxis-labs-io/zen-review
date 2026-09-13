package paint

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

const gutterMin = 2

// Gutter is the number column width for a file whose highest line number is widest.
// Pass the same value to Line and HunkHeader.
func Gutter(widest int) int {
	return max(gutterMin, len(strconv.Itoa(widest)))
}

// Clip truncates content to width and always marks the cut, even when content already fits.
func Clip(content string, width int, mark lipgloss.Style) string {
	switch {
	case width <= 0:
		return ""
	case width == 1:
		return mark.Render("…")
	}
	cut := lipgloss.NewStyle().MaxWidth(width - 1).Render(content)

	if lipgloss.Width(content) > width-1 {
		if gap := width - 1 - lipgloss.Width(cut); gap > 0 {
			cut += mark.Render(strings.Repeat(" ", gap))
		}
	}
	return cut + mark.Render("…")
}

// CodeColumn is the cell where source starts in a Line row.
func CodeColumn(gutter int) int {
	return gutter*2 + 5
}

// HalfColumn is the cell where source starts in a Half row.
func HalfColumn(gutter int) int {
	return gutter + 4
}

const markerSlot = 2

func number(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	s := strconv.Itoa(n)
	return strings.Repeat(" ", max(0, width-len(s))) + s
}
