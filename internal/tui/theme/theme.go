// Package theme is the one palette the UI styles from, derived from the
// terminal: ANSI slots for the hues, blends off the reported background for the
// shades.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is one palette. A nil color means the terminal's own shows through.
type Theme struct {
	// Syntax names a Chroma style. Empty is Chroma's default.
	Syntax string

	// Named for weight, not rank: Subtle is still read, Muted is looked past.
	Text     color.Color
	Accent   color.Color
	Subtle   color.Color
	Muted    color.Color
	Inverted color.Color

	Success color.Color
	Warning color.Color
	Error   color.Color
	Actor   color.Color

	// Background stays nil: it is the terminal's own, and painting it would
	// cost a translucent terminal its translucency.
	Background         color.Color
	SelectedBackground color.Color

	AddedBackground   color.Color
	RemovedBackground color.Color

	Border       color.Color
	BorderSubtle color.Color
	BorderMuted  color.Color
}

// InvertedOrText is the text color for use on top of a filled surface.
func (t Theme) InvertedOrText() color.Color {
	if t.Inverted != nil {
		return t.Inverted
	}
	return t.Text
}

func (t Theme) BorderSubtleOrBorder() color.Color {
	if t.BorderSubtle != nil {
		return t.BorderSubtle
	}
	return t.Border
}

func (t Theme) BorderMutedOrSubtle() color.Color {
	if t.BorderMuted != nil {
		return t.BorderMuted
	}
	return t.BorderSubtleOrBorder()
}

// A terminal may leave 8 to 15 undeclared or collapsed onto 0 to 7, so only the
// low eight are taken.
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

// Chroma ships no ANSI style, so code cannot follow the palette. These are
// paired against the background instead.
const (
	SyntaxDark  = "github-dark"
	SyntaxLight = "github"
)

// The least a usable background/foreground pair may be apart, in luma.
const minSeparation = 48

// How far off the background a filled row sits, in luma. A distance rather than
// a ratio: a ratio toward a hue the reader chose is not a fixed step.
const (
	tintLift      = 16
	selectionLift = 12
)

// The floor keeps a pale hue from reaching the distance in so few percent that
// the row reads grey. The ceiling binds where a hue sits at the background's own
// weight, canonical red over a dark page being the case that matters.
const (
	minHueLift = 0.14
	maxLift    = 0.5
)

// Terminal derives the theme from what the terminal reported.
func Terminal(s Surface) Theme {
	bg := s.Background
	t := Theme{
		Syntax: SyntaxDark,

		Text: lipgloss.NoColor{},

		// Blue over magenta: it is where a palette puts what is interactive and
		// where a scheme's identity most often lives.
		Accent:  slotBlue,
		Success: slotGreen,
		Warning: slotYellow,
		Error:   slotRed,

		// Magenta rather than cyan, which sits beside blue and would muddle a
		// handle written next to focused chrome.
		Actor: slotMagenta,

		Background: nil,
	}

	if bg == nil {
		// Nothing to blend against, and no slot is safe on both a light
		// terminal and a dark one: 7 disappears into a light background and 8
		// may be undeclared or collapsed onto 0. Text a reader has to read
		// takes the terminal's own foreground, which is legible by
		// construction. Borders are drawn runes and take the grey: a frame that
		// went missing would be visible, where unreadable labels are not.
		t.Subtle, t.Muted = lipgloss.NoColor{}, lipgloss.NoColor{}
		t.Border, t.BorderSubtle, t.BorderMuted = slotGrey, slotGrey, slotGrey
		t.Inverted = slotBlack
		return t
	}

	// Greys are structural and only have to stay legible, which a slot cannot
	// promise and a blend off the known background is by construction.
	away := shadeToward(bg, s.Foreground)
	t.Subtle = mix(bg, away, 0.65)
	t.Muted = mix(bg, away, 0.45)
	t.Border = mix(bg, away, 0.30)
	t.BorderSubtle = mix(bg, away, 0.20)
	t.BorderMuted = mix(bg, away, 0.12)

	t.Inverted = bg

	if !isDark(bg) {
		t.Syntax = SyntaxLight
	}

	// Neutral: along the shade axis it took the foreground's tint.
	t.SelectedBackground = lift(bg, nil, selectionLift)

	// A slot's RGBA() is its canonical value, so blending one washes the row in
	// xterm's system red rather than the red in the marker column beside it.
	t.AddedBackground = lift(bg, hueOr(s.Green, slotGreen), tintLift)
	t.RemovedBackground = lift(bg, hueOr(s.Red, slotRed), tintLift)

	return t
}

// The reported slot, or its canonical value. The one place a slot is blended:
// a tint has to lean its own way, or added and removed are the same wash.
func hueOr(reported, canonical color.Color) color.Color {
	if reported != nil {
		return reported
	}
	return canonical
}

// lift places a color a fixed luma distance off the background, toward hue or
// neutrally where there is none.
func lift(bg, hue color.Color, distance float64) color.Color {
	// Brighter on a dark background, darker on a light one.
	sign := 1.0
	if !isDark(bg) {
		sign = -1
	}

	if hue == nil {
		toward := contrast(bg)
		span := (luma(toward) - luma(bg)) * sign
		return mix(bg, toward, min(distance/span, maxLift))
	}

	span := (luma(hue) - luma(bg)) * sign
	if span <= 0 {
		// No amount of this hue lifts the row, so keep the lean rather than
		// trade it for a grey the other tint is indistinguishable from.
		return mix(bg, hue, minHueLift)
	}

	return mix(bg, hue, min(max(distance/span, minHueLift), maxLift))
}

// mix blends ratio of b into a, per channel, at 8 bits.
func mix(a, b color.Color, ratio float64) color.Color {
	ar, ag, ab := rgb8(a)
	br, bg, bb := rgb8(b)

	blend := func(x, y uint8) uint8 {
		return uint8(float64(x)*(1-ratio) + float64(y)*ratio)
	}
	return lipgloss.RGBColor{R: blend(ar, br), G: blend(ag, bg), B: blend(ab, bb)}
}

// shadeToward is the direction a shade travels from the background. Far from it
// and on the far side of the midpoint are two tests, and a pair failing either
// is no direction to travel in.
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

// contrast is the fallback direction where no usable foreground was reported.
func contrast(c color.Color) color.Color {
	if isDark(c) {
		return lipgloss.RGBColor{R: 0xff, G: 0xff, B: 0xff}
	}
	return lipgloss.RGBColor{R: 0x00, G: 0x00, B: 0x00}
}

// lipgloss has this and does not export it.
func isDark(c color.Color) bool { return luma(c) < 128 }

func luma(c color.Color) float64 {
	r, g, b := rgb8(c)
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}

func rgb8(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}
