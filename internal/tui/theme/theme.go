// Package theme holds the color palettes the UI styles from. Nothing in the
// TUI hardcodes a color: a color that isn't here means this struct needs a
// field.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is one palette. Optional fields are nil-able and have accessors that
// fall back, so adding a field doesn't force every theme to be rewritten.
type Theme struct {
	Name string

	// Syntax names a Chroma style rather than restating its token colors, there
	// being far more of them than the chrome has fields. Empty is Chroma's default.
	Syntax string

	// Named for weight, not rank: Subtle is still meant to be read, Muted is
	// there to be looked past. Accent is this UI's name for Rosé Pine's iris.
	Text     color.Color
	Accent   color.Color
	Subtle   color.Color
	Muted    color.Color
	Inverted color.Color

	// Semantic
	Success color.Color
	Warning color.Color
	Error   color.Color
	Actor   color.Color

	// Surfaces. A nil Background keeps transparency working and leaves every tint
	// below reading against an unknown. Both are true; the reader picks.
	Background         color.Color
	SelectedBackground color.Color

	// Diff surfaces. They group a run of changed lines and nothing more, because
	// the marker and the line number already say which way a line went.
	AddedBackground   color.Color
	RemovedBackground color.Color

	// Borders
	Border       color.Color
	BorderSubtle color.Color
	BorderMuted  color.Color
}

// InvertedOrText is the text color to use on top of a filled surface.
func (t Theme) InvertedOrText() color.Color {
	if t.Inverted != nil {
		return t.Inverted
	}
	return t.Text
}

// BorderSubtleOrBorder falls back for themes that define one border color.
func (t Theme) BorderSubtleOrBorder() color.Color {
	if t.BorderSubtle != nil {
		return t.BorderSubtle
	}
	return t.Border
}

// BorderMutedOrSubtle falls back through the border ladder.
func (t Theme) BorderMutedOrSubtle() color.Color {
	if t.BorderMuted != nil {
		return t.BorderMuted
	}
	return t.BorderSubtleOrBorder()
}

// rpBase is Rosé Pine Moon's own background. Inverted names it as the colour to
// write on top of a filled surface; Background stays nil, so nothing paints it.
var rpBase = lipgloss.Color("#232136")

// RosePineMoon is the only theme. Its text, semantic and border colors are the
// palette's own; the two diff tints are not, it having no role for a changed row.
var RosePineMoon = Theme{
	Name:               "rose-pine-moon",
	Syntax:             "rose-pine-moon",
	Text:               lipgloss.Color("#e0def4"),
	Accent:             lipgloss.Color("#c4a7e7"),
	Subtle:             lipgloss.Color("#908caa"),
	Muted:              lipgloss.Color("#6e6a86"),
	Inverted:           rpBase,
	Success:            lipgloss.Color("#9ccfd8"),
	Warning:            lipgloss.Color("#f6c177"),
	Error:              lipgloss.Color("#eb6f92"),
	Actor:              lipgloss.Color("#ea9a97"),
	Background:         nil,
	SelectedBackground: lipgloss.Color("#2a283e"),

	AddedBackground:   lipgloss.Color("#26383c"),
	RemovedBackground: lipgloss.Color("#3c2635"),

	Border:       lipgloss.Color("#56526e"),
	BorderSubtle: lipgloss.Color("#44415a"),
	BorderMuted:  lipgloss.Color("#393552"),
}
