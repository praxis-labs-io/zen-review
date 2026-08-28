package tree_test

import (
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// testSurface is what a dark terminal reports, palette included, so the derived
// colors below are the ones an ordinary run produces rather than the weaker
// fallback a terminal that answered nothing gets. It is named rather than
// queried: a test that asked the terminal would answer one way here and another
// in CI.
var testSurface = theme.Surface{
	Background: lipgloss.Color("#1e1e2e"),
	Foreground: lipgloss.Color("#cdd6f4"),
	Red:        lipgloss.Color("#f38ba8"),
	Green:      lipgloss.Color("#a6e3a1"),
}

// testTheme is the shipped derivation over that surface, so a test asserts the
// colors a reader is actually given rather than a palette nothing runs.
var testTheme = theme.Terminal(testSurface)
