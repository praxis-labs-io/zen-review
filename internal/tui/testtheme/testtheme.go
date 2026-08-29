// Package testtheme is the surface the render tests derive their theme from.
// Named rather than queried: a terminal would answer one way here and another
// in CI.
package testtheme

import (
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Surface is what a dark terminal reports, palette included, so Dark is what an
// ordinary run produces rather than the fallback a silent terminal gets.
var Surface = theme.Surface{
	Background: lipgloss.Color("#1e1e2e"),
	Foreground: lipgloss.Color("#cdd6f4"),
	Red:        lipgloss.Color("#f38ba8"),
	Green:      lipgloss.Color("#a6e3a1"),
}

// Dark is the shipped derivation over Surface.
var Dark = theme.Terminal(Surface)

// Bare is what a terminal that answered nothing gets: no surfaces to fill with,
// so anything lit has to say so by weight.
var Bare = theme.Terminal(theme.Surface{})
