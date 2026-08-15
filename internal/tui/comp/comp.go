// Package comp holds what both panes draw with. A pane never imports another
// pane, so anything two of them need lives here.
package comp

import (
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zen-kit/zen-kit/paint"
	"github.com/zen-kit/zen-kit/theme"
)

// Safe makes repository text fit to print on one row.
//
// A path and a hunk's section heading both come out of the repository, and git
// allows a newline and an escape sequence in either. A newline splits a row in
// two and puts every row of the other pane out of step with it; an escape
// sequence is run by the terminal. Neither is the pane's to trust.
//
// This is for display. The raw path stays the key a file is looked up under,
// so a name that had a control character in it still opens.
func Safe(text string) string { return sanitize(text, ' ') }

// Code is Safe for a line of source, which keeps its tabs. The painter expands
// one to a fixed number of cells, and a tab flattened to a single space here
// would collapse the file's indentation on the way past.
func Code(text string) string { return sanitize(text, '\t') }

// Prose is Safe for a block that draws as more than one row, so its newlines
// survive. Safe reads a newline as the control character it is and eats it.
func Prose(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = Safe(line)
	}
	return strings.Join(lines, "\n")
}

// sanitize strips what a terminal would run, and puts tab in place of a tab.
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

// Clip fits a rendered row into width, marking the cut, and leaves a row that
// already fits alone.
//
// Every row goes through this before a pane sees it. A pane clips overflow
// silently: a row wider than it loses its trailing columns mid-cell with no
// ellipsis, and a width test on the unclipped row still passes.
func Clip(row string, width int, mark lipgloss.Style) string {
	if lipgloss.Width(row) <= width {
		return row
	}
	return paint.Clip(row, width, mark)
}

// Churn is an added and removed count, the additions in the success colour and
// the removals in the error one. One grey for both says how much a file changed
// and not which way it went.
//
// base carries whatever background the row is painted on. Every styled run ends
// in a reset that clears the background with it, so the background has to be on
// each piece rather than wrapped round the result.
func Churn(base lipgloss.Style, t theme.Theme, added, removed int) string {
	return base.Foreground(t.Success).Render("+"+strconv.Itoa(added)) +
		base.Render(" ") +
		base.Foreground(t.Error).Render("-"+strconv.Itoa(removed))
}

// Help renders a keymap, styled from the theme rather than from bubbles'
// defaults, which pick their own colours.
func Help(t theme.Theme) help.Model {
	m := help.New()

	// The same dot the facts are separated by, rather than bubbles' bullet. One
	// screen reads at one weight.
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
