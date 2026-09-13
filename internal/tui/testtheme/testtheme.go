// Package testtheme is the fixed surface render tests derive from, so a terminal and CI agree.
package testtheme

import (
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Surface is what a dark terminal reports, palette included.
var Surface = theme.Surface{
	Background: lipgloss.Color("#1e1e2e"),
	Foreground: lipgloss.Color("#cdd6f4"),
	Red:        lipgloss.Color("#f38ba8"),
	Green:      lipgloss.Color("#a6e3a1"),
}

var Dark = theme.Terminal(Surface)

var Bare = theme.Terminal(theme.Surface{})
