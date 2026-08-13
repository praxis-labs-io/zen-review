// Package comp holds what both panes draw with. A pane never imports another
// pane, so anything two of them need lives here.
package comp

import (
	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/paint"
	"github.com/zen-kit/zen-kit/theme"
)

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

	key := lipgloss.NewStyle().Foreground(t.Secondary)
	desc := lipgloss.NewStyle().Foreground(t.Faint)
	sep := lipgloss.NewStyle().Foreground(t.BorderFaintOrSecondary())

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
