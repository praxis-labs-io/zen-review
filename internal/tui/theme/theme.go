// Package theme holds the palette the UI styles from. Nothing in the TUI
// hardcodes a color: a color that isn't here means this struct needs a field.
//
// There is one theme and it is derived rather than written down. The hues are
// ANSI slots, so they are whatever the reader's terminal maps them to; the
// shades are blended from the background the terminal reports at launch, so
// they sit just above it whatever it is.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is one palette. Optional fields are nil-able and have accessors that
// fall back, so adding a field doesn't force the derivation to be rewritten.
type Theme struct {
	// Syntax names a Chroma style rather than restating its token colors, there
	// being far more of them than the chrome has fields. Empty is Chroma's default.
	Syntax string

	// Named for weight, not rank: Subtle is still meant to be read, Muted is
	// there to be looked past.
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

	// Surfaces. Background is nil and stays nil: the one this theme was derived
	// from is the terminal's own, so painting it would change nothing a reader
	// can see and would cost a translucent terminal its translucency.
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

// The hues, as ANSI slots. Painted, a slot is whatever the terminal maps it to,
// which is the whole point: a reader's palette reaches the chrome without being
// configured. Only the low eight are taken. A terminal is free to leave 8 to 15
// undeclared or collapsed onto 0 to 7, and nothing here would be able to tell.
const (
	slotBlack   = lipgloss.Black
	slotRed     = lipgloss.Red
	slotGreen   = lipgloss.Green
	slotYellow  = lipgloss.Yellow
	slotBlue    = lipgloss.Blue
	slotMagenta = lipgloss.Magenta
	slotWhite   = lipgloss.White
	slotGrey    = lipgloss.BrightBlack
)

// SyntaxDark and SyntaxLight are the Chroma styles code is highlighted with.
// The chrome follows the terminal and code cannot: Chroma styles are truecolor
// and there is no ANSI one to reach for. Pairing them against the background is
// what stops a light terminal rendering #e6edf3 source on white.
const (
	SyntaxDark  = "github-dark"
	SyntaxLight = "github"
)

// minSeparation is the least a usable pair may be apart, in luma. It is the
// floor under the side test below, for the pair that straddles the midpoint by
// a hair and is no direction to travel in either.
const minSeparation = 48

// tintLift and selectionLift are how far off the background a filled row sits,
// in luma. A distance rather than a ratio, because a ratio toward a hue the
// reader chose is not a fixed step: the same fraction that lifts a row clear of
// one palette's green barely moves it against another's.
const (
	tintLift      = 16
	selectionLift = 12
)

// The ratio a lift toward a hue is held between. The floor is there because a
// pale green reaches the distance in a few percent, and a few percent of a
// colour is not a colour: the row lifts and reads grey.
// The ceiling only ever binds for a hue sitting within a few dozen luma of the
// background, which is the case where a large fraction of it moves the row's
// colour a long way and its weight barely at all. So it is loose: what stops a
// tint burying code is the distance being solved for, not this.
const (
	minHueLift = 0.14
	maxLift    = 0.5
)

// Terminal derives the theme from what the terminal reported. Either field of
// the surface is nil where nothing answered.
func Terminal(s Surface) Theme {
	bg := s.Background
	t := Theme{
		Syntax: SyntaxDark,

		// Text is the terminal's own foreground rather than a color of ours.
		// Nothing matches a reader's palette as exactly as the palette.
		Text: lipgloss.NoColor{},

		// Blue is where a palette puts what is interactive, and it is the slot
		// a scheme's identity most often lives in. Magenta was this app's accent
		// while the theme was Rosé Pine and iris was the color it highlighted
		// with; read as a slot rather than as that palette, it is the decorative
		// one. Nord is the case that shows it: slot 5 is a muted mauve where
		// slot 4 is the frost blue the scheme is known for.
		Accent: slotBlue,

		Success: slotGreen,
		Warning: slotYellow,
		Error:   slotRed,

		// Magenta rather than cyan, which sits beside blue and would muddle
		// wherever a handle is written next to focused chrome.
		Actor: slotMagenta,

		// The terminal's own, always. Every shade below was derived against it
		// rather than over a fill of ours, so there is nothing to paint.
		Background: nil,
	}

	if bg == nil {
		// Slots are the best available guess at a grey and a border, and the
		// caveat above applies: a palette that collapsed 8 onto 0 puts muted
		// chrome on the background and there is no way to see it coming. It is
		// the fallback because there is nothing better, not because it is good.
		t.Subtle, t.Muted = slotWhite, slotGrey
		t.Border, t.BorderSubtle, t.BorderMuted = slotGrey, slotGrey, slotGrey
		t.Inverted = slotBlack
		return t
	}

	// The greys are blends where the hues are slots, and the split is
	// deliberate. A palette's identity lives in its hues. Greys are structural
	// and only have to stay legible, which a slot cannot promise and a blend off
	// the known background is by construction.
	away := shadeToward(bg, s.Foreground)
	t.Subtle = mix(bg, away, 0.65)
	t.Muted = mix(bg, away, 0.45)
	t.Border = mix(bg, away, 0.30)
	t.BorderSubtle = mix(bg, away, 0.20)
	t.BorderMuted = mix(bg, away, 0.12)

	// Text drawn on top of a filled Accent, so it wants to read as the page
	// does: the background the fill was placed over.
	t.Inverted = bg

	if !isDark(bg) {
		t.Syntax = SyntaxLight
	}

	// Neutral rather than along the shade axis. A selection is a lift off the
	// page and not a colour of its own, and travelling toward the foreground
	// gave it the foreground's tint on every palette that has one.
	t.SelectedBackground = lift(bg, nil, selectionLift)

	// The two tints lean on the reader's own red and green where the terminal
	// answered for them. A slot's RGBA() is its canonical value and never what
	// the terminal mapped it to, so blending one washes the row in xterm's dark
	// system red rather than in the red beside it in the marker column.
	t.AddedBackground = lift(bg, hueOr(s.Green, slotGreen), tintLift)
	t.RemovedBackground = lift(bg, hueOr(s.Red, slotRed), tintLift)

	return t
}

// hueOr is a slot as the terminal actually paints it, where it answered for it,
// and the canonical value where it did not. Blending a slot is otherwise
// forbidden and this is the one place it happens: a tint has to lean its own way
// whatever came back, or added and removed are the same wash.
func hueOr(reported, canonical color.Color) color.Color {
	if reported != nil {
		return reported
	}
	return canonical
}

// lift places a color a fixed luma distance off the background, travelling
// toward hue, or neutrally where there is none.
//
// Solving for the distance is what lets one number hold across palettes. The
// ratio it works out to is small against a bright hue and large against a dark
// one, where a fixed ratio leaves the row unlifted on every terminal whose green
// happens to sit near its background.
func lift(bg, hue color.Color, distance float64) color.Color {
	// Which way a shade has to move to be seen: brighter on a dark background,
	// darker on a light one.
	sign := 1.0
	if !isDark(bg) {
		sign = -1
	}

	if hue == nil {
		// Neutral is always the full way to white or black, so it always has
		// the room and never needs the floor.
		toward := contrast(bg)
		span := (luma(toward) - luma(bg)) * sign
		return mix(bg, toward, min(distance/span, maxLift))
	}

	span := (luma(hue) - luma(bg)) * sign
	if span <= 0 {
		// The hue sits at the background's own weight or on the wrong side of
		// it, so no amount of it lifts the row. Keeping the lean beats trading
		// it for a grey the other tint would be indistinguishable from; the
		// marker column carries what the luma cannot.
		return mix(bg, hue, minHueLift)
	}

	return mix(bg, hue, min(max(distance/span, minHueLift), maxLift))
}

// mix blends ratio of b into a, per channel. Both are read at 8 bits, which is
// what a terminal takes and what keeps the arithmetic legible.
func mix(a, b color.Color, ratio float64) color.Color {
	ar, ag, ab := rgb8(a)
	br, bg, bb := rgb8(b)

	blend := func(x, y uint8) uint8 {
		return uint8(float64(x)*(1-ratio) + float64(y)*ratio)
	}
	return lipgloss.RGBColor{R: blend(ar, br), G: blend(ag, bg), B: blend(ab, bb)}
}

// shadeToward is the direction a shade travels from the background. The
// terminal's own foreground is the right answer where it reported one: the greys
// then sit on the axis between the page and the text on it, rather than on the
// one between the page and pure white.
//
// It has to be on the far side of the midpoint from the background, which is a
// different test from being far from it. A terminal is free to report a pair
// that sits on one side of it, and a ladder built along those two climbs from
// almost-black to still-dark. Refusing the pair costs the harmony and keeps the
// shades legible, which is the half that matters.
func shadeToward(bg, fg color.Color) color.Color {
	if fg == nil || isDark(bg) == isDark(fg) || separation(bg, fg) < minSeparation {
		return contrast(bg)
	}
	return fg
}

func separation(a, b color.Color) float64 {
	d := luma(a) - luma(b)
	if d < 0 {
		return -d
	}
	return d
}

// contrast is the direction a shade moves in to stay visible against c: toward
// white on a dark background, toward black on a light one. It is the fallback
// wherever no usable foreground was reported, and it lets one set of ratios
// serve a light terminal and a dark one without a second table.
func contrast(c color.Color) color.Color {
	if isDark(c) {
		return lipgloss.RGBColor{R: 0xff, G: 0xff, B: 0xff}
	}
	return lipgloss.RGBColor{R: 0x00, G: 0x00, B: 0x00}
}

// isDark reports whether c is dark, by perceived luminance. lipgloss has this
// and does not export it.
func isDark(c color.Color) bool { return luma(c) < 128 }

func luma(c color.Color) float64 {
	r, g, b := rgb8(c)
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}

func rgb8(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}
