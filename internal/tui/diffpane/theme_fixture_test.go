package diffpane_test

import (
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// testSurface is what a dark terminal would report. It is named rather than
// queried: a test that asked the terminal would answer one way here and another
// in CI, and every color below is derived from this pair.
var testSurface = theme.Surface{
	Background: lipgloss.Color("#232136"),
	Foreground: lipgloss.Color("#e0def4"),
}

// testTheme is the shipped derivation over that surface, so a test asserts the
// colors a reader is actually given rather than a palette nothing runs.
var testTheme = theme.Terminal(testSurface)
