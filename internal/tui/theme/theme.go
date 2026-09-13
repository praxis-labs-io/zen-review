// Package theme derives the palette from the terminal: ANSI slots for hues, blends for shades.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is the palette. A nil color leaves the terminal's own showing.
type Theme struct {
	// Syntax names a Chroma style; empty is Chroma's default.
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

	// Background stays nil, since painting it costs a translucent terminal its translucency.
	Background         color.Color
	SelectedBackground color.Color

	AddedBackground   color.Color
	RemovedBackground color.Color

	Border       color.Color
	BorderSubtle color.Color
	BorderMuted  color.Color
}

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

const (
	SyntaxDark  = "github-dark"
	SyntaxLight = "github"
)

const minSeparation = 48

const (
	tintLift      = 16
	selectionLift = 12
)

const (
	minHueLift = 0.14
	maxLift    = 0.5
)

func Terminal(s Surface) Theme {
	bg := s.Background
	t := Theme{
		Syntax: SyntaxDark,

		Text: lipgloss.NoColor{},

		Accent:  slotBlue,
		Success: slotGreen,
		Warning: slotYellow,
		Error:   slotRed,

		Actor: slotMagenta,

		Background: nil,
	}

	if bg == nil {
		t.Subtle, t.Muted = lipgloss.NoColor{}, lipgloss.NoColor{}
		t.Border, t.BorderSubtle, t.BorderMuted = slotGrey, slotGrey, slotGrey
		t.Inverted = slotBlack
		return t
	}

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

	t.SelectedBackground = lift(bg, nil, selectionLift)

	t.AddedBackground = lift(bg, hueOr(s.Green, slotGreen), tintLift)
	t.RemovedBackground = lift(bg, hueOr(s.Red, slotRed), tintLift)

	return t
}

// hueOr is the one place a slot is blended, since a tint has to lean toward its own hue.
func hueOr(reported, canonical color.Color) color.Color {
	if reported != nil {
		return reported
	}
	return canonical
}

func lift(bg, hue color.Color, distance float64) color.Color {
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
		return mix(bg, hue, minHueLift)
	}

	return mix(bg, hue, min(max(distance/span, minHueLift), maxLift))
}

func mix(a, b color.Color, ratio float64) color.Color {
	ar, ag, ab := rgb8(a)
	br, bg, bb := rgb8(b)

	blend := func(x, y uint8) uint8 {
		return uint8(float64(x)*(1-ratio) + float64(y)*ratio)
	}
	return lipgloss.RGBColor{R: blend(ar, br), G: blend(ag, bg), B: blend(ab, bb)}
}

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
