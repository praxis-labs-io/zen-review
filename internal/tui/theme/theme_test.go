package theme_test

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

var (
	darkBG  = lipgloss.Color("#232136")
	lightBG = lipgloss.Color("#faf4ed")

	// The pair a terminal reports. The shades travel toward the foreground, so
	// most of these assertions need both halves.
	dark  = theme.Surface{Background: darkBG, Foreground: lipgloss.Color("#e0def4")}
	light = theme.Surface{Background: lightBG, Foreground: lipgloss.Color("#575279")}
)

// rgb reads a color the way a terminal will, so a test compares what is painted
// rather than how it was spelled.
func rgb(c color.Color) (int, int, int) {
	r, g, b, _ := c.RGBA()
	return int(r >> 8), int(g >> 8), int(b >> 8)
}

func luma(c color.Color) float64 {
	r, g, b := rgb(c)
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}

func TestHuesStayASlot(t *testing.T) {
	// A slot has to reach the terminal as a slot. Flattened to RGB it stops
	// following the reader's palette, which is the whole of the feature.
	th := theme.Terminal(dark)
	for _, tc := range []struct {
		name string
		got  color.Color
		want xansi.BasicColor
	}{
		{"Accent", th.Accent, lipgloss.Blue},
		{"Success", th.Success, lipgloss.Green},
		{"Warning", th.Warning, lipgloss.Yellow},
		{"Error", th.Error, lipgloss.Red},
		{"Actor", th.Actor, lipgloss.Magenta},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.got.(xansi.BasicColor)
			if !ok {
				t.Fatalf("%s = %T, want a xansi.BasicColor", tc.name, tc.got)
			}
			if got != tc.want {
				t.Errorf("%s = slot %d, want slot %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestHuesDoNotFollowTheBackground(t *testing.T) {
	// The hues are the reader's, so a light terminal and a dark one get the
	// same slots. Only the shades move.
	dark, light := theme.Terminal(dark), theme.Terminal(light)
	if dark.Accent != light.Accent {
		t.Errorf("Accent = %v on dark and %v on light, want the same slot", dark.Accent, light.Accent)
	}
	if dark.Error != light.Error {
		t.Errorf("Error = %v on dark and %v on light, want the same slot", dark.Error, light.Error)
	}
}

func TestTextIsTheTerminalsOwn(t *testing.T) {
	if got := theme.Terminal(dark).Text; !isNoColor(got) {
		t.Errorf("Text = %v, want NoColor so the terminal's own foreground is used", got)
	}
}

func TestAReportedForegroundIsNotPaintedAsText(t *testing.T) {
	got := theme.Terminal(theme.Surface{Background: darkBG, Foreground: lipgloss.Color("#e0c0a0")})
	if !isNoColor(got.Text) {
		t.Errorf("Text = %v, want NoColor even when a foreground was reported", got.Text)
	}
}

func isNoColor(c color.Color) bool {
	_, ok := c.(lipgloss.NoColor)
	return ok
}

// The background a shade was derived against is the terminal's own, so there is
// nothing of ours to paint over it. Carrying one would cost a translucent
// terminal its translucency for a fill nobody could see.
func TestNoThemePaintsABackground(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    theme.Surface
	}{
		{"dark", dark},
		{"light", light},
		{"undetected", theme.Surface{}},
	} {
		if got := theme.Terminal(tc.s).Background; got != nil {
			t.Errorf("%s: Background = %v, want nil", tc.name, got)
		}
	}
}

func TestShadesLightenADarkBackground(t *testing.T) {
	th := theme.Terminal(dark)
	base := luma(darkBG)

	// The ladder, dimmest first. Each has to clear the background it sits on
	// and each has to clear the one below it, or the weights collapse.
	for _, step := range []struct {
		name string
		c    color.Color
	}{
		{"BorderMuted", th.BorderMuted},
		{"BorderSubtle", th.BorderSubtle},
		{"Border", th.Border},
		{"Muted", th.Muted},
		{"Subtle", th.Subtle},
	} {
		if got := luma(step.c); got <= base {
			t.Errorf("%s luma = %.1f, want brighter than the background's %.1f", step.name, got, base)
		}
		base = luma(step.c)
	}
}

func TestShadesDarkenALightBackground(t *testing.T) {
	th := theme.Terminal(light)
	base := luma(lightBG)

	for _, step := range []struct {
		name string
		c    color.Color
	}{
		{"BorderMuted", th.BorderMuted},
		{"BorderSubtle", th.BorderSubtle},
		{"Border", th.Border},
		{"Muted", th.Muted},
		{"Subtle", th.Subtle},
	} {
		if got := luma(step.c); got >= base {
			t.Errorf("%s luma = %.1f, want darker than the background's %.1f", step.name, got, base)
		}
		base = luma(step.c)
	}
}

func TestShadesTravelTowardTheReportedForeground(t *testing.T) {
	warm := lipgloss.Color("#e0c0a0") // a foreground well off neutral
	got := theme.Terminal(theme.Surface{Background: darkBG, Foreground: warm})
	flat := theme.Terminal(theme.Surface{Background: darkBG})

	if got.Subtle == flat.Subtle {
		t.Error("Subtle ignored the reported foreground, want it derived toward it")
	}

	// Toward a warm foreground the grey has to come out warm: red above blue,
	// where the pure-white fallback keeps the background's own balance.
	r, _, b := rgb(got.Subtle)
	if r <= b {
		t.Errorf("Subtle = %d,_,%d, want the warm foreground to lead red over blue", r, b)
	}
}

// Far from the background and on the far side of the midpoint from it are two
// different tests, and a pair failing either is no direction to travel in.
func TestAForegroundTooCloseToTheBackgroundIsRefused(t *testing.T) {
	murky := theme.Surface{Background: darkBG, Foreground: lipgloss.Color("#2b2940")}
	got := theme.Terminal(murky)
	flat := theme.Terminal(theme.Surface{Background: darkBG})

	if got.Subtle != flat.Subtle {
		t.Error("a foreground with no separation was used, want the contrast fallback")
	}
}

func TestDiffTintsLeanTheirOwnWay(t *testing.T) {
	th := theme.Terminal(dark)
	br, bg, bb := rgb(darkBG)

	ar, ag, ab := rgb(th.AddedBackground)
	if ag <= bg || ag <= ar || ag <= ab {
		t.Errorf("AddedBackground = %d,%d,%d over a %d,%d,%d base, want green to lead", ar, ag, ab, br, bg, bb)
	}

	rr, rg, rb := rgb(th.RemovedBackground)
	if rr <= br || rr <= rg || rr <= rb {
		t.Errorf("RemovedBackground = %d,%d,%d over a %d,%d,%d base, want red to lead", rr, rg, rb, br, bg, bb)
	}
}

func TestTintsStayUnderTheCode(t *testing.T) {
	// A tint groups a run of changed lines. At full strength it buries the
	// source sitting on it, so it has to stay nearer the background than the
	// color it leans toward.
	th := theme.Terminal(dark)
	base, added := luma(darkBG), luma(th.AddedBackground)
	if added-base > luma(lipgloss.Green)-base {
		t.Errorf("AddedBackground luma = %.1f, want far nearer the background's %.1f than green's %.1f",
			added, base, luma(lipgloss.Green))
	}
}

func TestNoBackgroundPaintsNoSurface(t *testing.T) {
	// A guessed surface is worse than none. Slot 0 is the background on a great
	// many dark palettes, so a selection painted in it is invisible exactly
	// where it was needed; the bar glyph and the markers carry it instead.
	th := theme.Terminal(theme.Surface{})
	for _, tc := range []struct {
		name string
		c    color.Color
	}{
		{"SelectedBackground", th.SelectedBackground},
		{"AddedBackground", th.AddedBackground},
		{"RemovedBackground", th.RemovedBackground},
	} {
		if tc.c != nil {
			t.Errorf("%s = %v with no background detected, want nil", tc.name, tc.c)
		}
	}

	// Borders are drawn runes rather than fills, so they still have a color to
	// be. A slot is the only thing left to reach for.
	if _, ok := th.Border.(xansi.BasicColor); !ok {
		t.Errorf("Border = %T, want a slot when nothing was detected", th.Border)
	}
}

// Nothing answered, so there is no background to have blended against. A shade
// pinned to RGB here is one the terminal has no say in.
func TestNoBackgroundPinsNothing(t *testing.T) {
	th := theme.Terminal(theme.Surface{})
	for name, c := range map[string]color.Color{
		"Accent":       th.Accent,
		"Success":      th.Success,
		"Warning":      th.Warning,
		"Error":        th.Error,
		"Actor":        th.Actor,
		"Subtle":       th.Subtle,
		"Muted":        th.Muted,
		"Inverted":     th.Inverted,
		"Border":       th.Border,
		"BorderSubtle": th.BorderSubtle,
		"BorderMuted":  th.BorderMuted,
	} {
		if _, ok := c.(xansi.BasicColor); !ok {
			t.Errorf("%s = %T, want a slot the terminal maps", name, c)
		}
	}
	if !isNoColor(th.Text) {
		t.Errorf("Text = %T, want NoColor", th.Text)
	}
}

func TestSyntaxIsPairedAgainstTheBackground(t *testing.T) {
	// Chroma has no ANSI style, so code is the one thing that cannot follow the
	// palette. Pairing is what stops a light terminal drawing dark-theme source.
	for _, tc := range []struct {
		name string
		s    theme.Surface
		want string
	}{
		{"dark", dark, theme.SyntaxDark},
		{"light", light, theme.SyntaxLight},
		{"undetected", theme.Surface{}, theme.SyntaxDark},
	} {
		if got := theme.Terminal(tc.s).Syntax; got != tc.want {
			t.Errorf("%s: Syntax = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOptionalFieldsFallBack(t *testing.T) {
	bare := theme.Theme{
		Text:   lipgloss.Color("#ffffff"),
		Border: lipgloss.Color("#333333"),
	}

	if got := bare.InvertedOrText(); got != bare.Text {
		t.Errorf("InvertedOrText() = %v, want Text when Inverted is unset", got)
	}
	if got := bare.BorderSubtleOrBorder(); got != bare.Border {
		t.Errorf("BorderSubtleOrBorder() = %v, want Border when unset", got)
	}
	if got := bare.BorderMutedOrSubtle(); got != bare.Border {
		t.Errorf("BorderMutedOrSubtle() = %v, want it to fall through to Border", got)
	}
}

func TestSetOptionalFieldsWin(t *testing.T) {
	full := theme.Terminal(dark)

	if got := full.InvertedOrText(); got != full.Inverted {
		t.Errorf("InvertedOrText() = %v, want Inverted when it is set", got)
	}
	if got := full.BorderMutedOrSubtle(); got != full.BorderMuted {
		t.Errorf("BorderMutedOrSubtle() = %v, want BorderMuted when it is set", got)
	}
}
