// Package comp holds what both panes draw with. A pane never imports another
// pane, so anything two of them need lives here.
package comp

import (
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
func Safe(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
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

// Help renders a keymap, styled from the theme rather than from bubbles'
// defaults, which pick their own colours.
func Help(t theme.Theme) help.Model {
	m := help.New()

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
