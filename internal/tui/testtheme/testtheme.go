// Package testtheme is the surface the render tests derive from. Named rather
// than queried: a terminal answers one way here and another in CI.
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

// Dark is the shipped derivation over Surface.
var Dark = theme.Terminal(Surface)

// Bare is what a terminal that answered nothing gets: no surfaces at all.
var Bare = theme.Terminal(theme.Surface{})
