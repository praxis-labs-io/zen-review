// Package comp holds the widgets panes share, since a pane never imports another.
package comp

import (
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Safe strips escapes and control characters so repository text prints on one row.
// Display only: a file is still looked up by its raw path.
func Safe(text string) string { return sanitize(text, ' ') }

// Code is Safe that keeps tabs, for source the painter expands.
func Code(text string) string { return sanitize(text, '\t') }

// Prose is Safe that keeps newlines, for text drawn over several rows.
func Prose(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = Safe(line)
	}
	return strings.Join(lines, "\n")
}

func sanitize(text string, tab rune) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return tab
		}
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, ansi.Strip(text))
}

// Clip truncates row to width with a mark, and leaves a row that already fits untouched.
func Clip(row string, width int, mark lipgloss.Style) string {
	if lipgloss.Width(row) <= width {
		return row
	}
	return paint.Clip(row, width, mark)
}

// Churn renders "+added -removed" in the success and error colours over base's background.
func Churn(base lipgloss.Style, t theme.Theme, added, removed int) string {
	return base.Foreground(t.Success).Render("+"+strconv.Itoa(added)) +
		base.Render(" ") +
		base.Foreground(t.Error).Render("-"+strconv.Itoa(removed))
}

// Placeholder renders text on a blank width by height block, a third of the way down.
func Placeholder(t theme.Theme, text string, width, height int) string {
	subtle := lipgloss.NewStyle().Foreground(t.Subtle)

	lines := make([]string, max(height, 0))
	blank := strings.Repeat(" ", max(width, 0))
	for i := range lines {
		lines[i] = blank
	}
	if len(lines) == 0 || width <= 0 {
		return strings.Join(lines, "\n")
	}

	row := Clip(subtle.Render(Safe(text)), width, subtle)
	pad := (width - lipgloss.Width(row)) / 2
	lines[len(lines)/3] = strings.Repeat(" ", pad) + row +
		strings.Repeat(" ", width-pad-lipgloss.Width(row))
	return strings.Join(lines, "\n")
}

func Help(t theme.Theme) help.Model {
	m := help.New()

	m.ShortSeparator = " · "

	key := lipgloss.NewStyle().Foreground(t.Accent)
	desc := lipgloss.NewStyle().Foreground(t.Subtle)
	sep := lipgloss.NewStyle().Foreground(t.BorderMutedOrSubtle())

	m.Styles = help.Styles{
		Ellipsis:       desc,
		ShortKey:       key,
		ShortDesc:      desc,
		ShortSeparator: sep,
		FullKey:        key,
		FullDesc:       desc,
		FullSeparator:  sep,
	}
	return m
}
