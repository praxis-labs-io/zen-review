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
	darkFG  = lipgloss.Color("#e0def4")
	lightBG = lipgloss.Color("#faf4ed")

	// The shades travel toward the foreground, so most cases need both halves.
	dark  = theme.Surface{Background: darkBG, Foreground: darkFG}
	light = theme.Surface{Background: lightBG, Foreground: lipgloss.Color("#575279")}

	// Real palettes: a background bluer than its own green, olive hues, light,
	// and almost no room between the background and the hues.
	mocha = theme.Surface{Background: lipgloss.Color("#1e1e2e"), Foreground: lipgloss.Color("#cdd6f4"),
		Red: lipgloss.Color("#f38ba8"), Green: lipgloss.Color("#a6e3a1")}
	gruvbox = theme.Surface{Background: lipgloss.Color("#282828"), Foreground: lipgloss.Color("#ebdbb2"),
		Red: lipgloss.Color("#cc241d"), Green: lipgloss.Color("#98971a")}
	solarizedLight = theme.Surface{Background: lipgloss.Color("#fdf6e3"), Foreground: lipgloss.Color("#657b83"),
		Red: lipgloss.Color("#dc322f"), Green: lipgloss.Color("#859900")}
	lowContrast = theme.Surface{Background: lipgloss.Color("#2b2b2b"), Foreground: lipgloss.Color("#8a8a8a"),
		Red: lipgloss.Color("#5c3030"), Green: lipgloss.Color("#305c30")}
)

// rgb reads a color the way a terminal will, not the way it was spelled.
func rgb(c color.Color) (int, int, int) {
	r, g, b, _ := c.RGBA()
	return int(r >> 8), int(g >> 8), int(b >> 8)
}

func luma(c color.Color) float64 {
	r, g, b := rgb(c)
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}

func TestHuesStayASlot(t *testing.T) {
	// Flattened to RGB a slot stops following the reader's palette.
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

// Painting the terminal's own background would cost a translucent terminal its
// translucency for a fill nobody could see.
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

	// Dimmest first. Each has to clear the one below it or the weights collapse.
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

	// Toward a warm foreground the grey comes out warm: red above blue.
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

// Which channel comes out largest is the palette's business: over a background
// as blue as Catppuccin's, a wash of a real green is still bluer than green.
func TestATintLiesBetweenTheBackgroundAndItsHue(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    theme.Surface
	}{
		{"dark", withPalette(dark, "#eb6f92", "#3e8fb0")},
		{"blue-heavy background", mocha},
		{"olive hues", gruvbox},
		{"light", solarizedLight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := theme.Terminal(tc.s)
			between(t, "AddedBackground", tc.s.Background, th.AddedBackground, tc.s.Green)
			between(t, "RemovedBackground", tc.s.Background, th.RemovedBackground, tc.s.Red)
		})
	}
}

// A tint never overshoots its hue and stays the nearer end of the run.
func between(t *testing.T, name string, bg, tint, hue color.Color) {
	t.Helper()

	br, bgr, bb := rgb(bg)
	tr, tg, tb := rgb(tint)
	hr, hg, hb := rgb(hue)

	for _, c := range []struct {
		channel        string
		base, got, end int
	}{
		{"red", br, tr, hr},
		{"green", bgr, tg, hg},
		{"blue", bb, tb, hb},
	} {
		if c.got < min(c.base, c.end) || c.got > max(c.base, c.end) {
			t.Errorf("%s %s = %d, want it between the background's %d and the hue's %d",
				name, c.channel, c.got, c.base, c.end)
		}
		if abs(c.got-c.base) > abs(c.got-c.end) {
			t.Errorf("%s %s = %d, nearer the hue's %d than the background's %d: the code on it goes under",
				name, c.channel, c.got, c.end, c.base)
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func withPalette(s theme.Surface, red, green string) theme.Surface {
	s.Red, s.Green = lipgloss.Color(red), lipgloss.Color(green)
	return s
}

// Blending a slot takes its canonical value, which nobody is looking at.
func TestATintTakesTheReportedHueOverTheSlot(t *testing.T) {
	reported := theme.Terminal(theme.Surface{Background: darkBG, Foreground: darkFG,
		Red: lipgloss.Color("#f38ba8"), Green: lipgloss.Color("#a6e3a1")})
	canonical := theme.Terminal(theme.Surface{Background: darkBG, Foreground: darkFG})

	if reported.AddedBackground == canonical.AddedBackground {
		t.Error("AddedBackground ignored the reported green, want it derived from the palette")
	}
	if reported.RemovedBackground == canonical.RemovedBackground {
		t.Error("RemovedBackground ignored the reported red, want it derived from the palette")
	}
}

// The same fraction that clears one palette's green leaves the row flat against
// another's, which is why the lift solves for a distance.
func TestATintClearsTheBackgroundOnEverySurface(t *testing.T) {
	const least = 8

	for _, tc := range []struct {
		name string
		s    theme.Surface
	}{
		{"blue-heavy background", mocha},
		{"olive hues", gruvbox},
		{"light palette", solarizedLight},
		{"low contrast", lowContrast},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := theme.Terminal(tc.s)
			base := luma(tc.s.Background)

			for _, tint := range []struct {
				name string
				c    color.Color
			}{
				{"AddedBackground", th.AddedBackground},
				{"RemovedBackground", th.RemovedBackground},
				{"SelectedBackground", th.SelectedBackground},
			} {
				if got := luma(tint.c) - base; got < least && got > -least {
					t.Errorf("%s luma = %.1f against a background of %.1f, want it clear by %d",
						tint.name, luma(tint.c), base, least)
				}
			}
		})
	}
}

// The fallback wash cannot lift the row at all, but it moves a long way in
// colour, so the floor every surface meets is a channel distance not a luma one.
func TestATintIsPerceptibleOnEverySurface(t *testing.T) {
	const least = 10

	for _, tc := range []struct {
		name string
		s    theme.Surface
	}{
		{"dark", dark},
		{"light", light},
		{"blue-heavy background", mocha},
		{"olive hues", gruvbox},
		{"light palette", solarizedLight},
		{"low contrast", lowContrast},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := theme.Terminal(tc.s)
			br, bg, bb := rgb(tc.s.Background)

			for _, tint := range []struct {
				name string
				c    color.Color
			}{
				{"AddedBackground", th.AddedBackground},
				{"RemovedBackground", th.RemovedBackground},
				{"SelectedBackground", th.SelectedBackground},
			} {
				r, g, b := rgb(tint.c)
				if got := max(abs(r-br), abs(g-bg), abs(b-bb)); got < least {
					t.Errorf("%s is %d,%d,%d over a %d,%d,%d background, no channel moving more than %d",
						tint.name, r, g, b, br, bg, bb, got)
				}
			}
		})
	}
}

// A reader scanning a hunk reads the block before the marker in it.
func TestTheTwoTintsNeverCollapseTogether(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    theme.Surface
	}{
		{"dark", dark},
		{"light", light},
		{"blue-heavy background", mocha},
		{"olive hues", gruvbox},
		{"low contrast", lowContrast},
		{"no palette reported", theme.Surface{Background: darkBG, Foreground: darkFG}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := theme.Terminal(tc.s)
			ar, ag, ab := rgb(th.AddedBackground)
			rr, rg, rb := rgb(th.RemovedBackground)

			if ar == rr && ag == rg && ab == rb {
				t.Errorf("both tints are %d,%d,%d, so a changed block cannot say which way it went", ar, ag, ab)
			}
		})
	}
}

// Along the shade axis a selection took the foreground's tint, which is a colour
// the reader never chose.
func TestTheSelectionIsANeutralLift(t *testing.T) {
	warm := theme.Surface{Background: darkBG, Foreground: lipgloss.Color("#e0c0a0")}
	th := theme.Terminal(warm)

	br, bg, bb := rgb(darkBG)
	sr, sg, sb := rgb(th.SelectedBackground)

	dr, dg, db := sr-br, sg-bg, sb-bb
	if spread := max(dr, dg, db) - min(dr, dg, db); spread > 2 {
		t.Errorf("SelectedBackground moves %d,%d,%d off the background, want an even lift", dr, dg, db)
	}
}

func TestTintsStayUnderTheCode(t *testing.T) {
	// At full strength a tint buries the source sitting on it.
	th := theme.Terminal(dark)
	base, added := luma(darkBG), luma(th.AddedBackground)
	if added-base > luma(lipgloss.Green)-base {
		t.Errorf("AddedBackground luma = %.1f, want far nearer the background's %.1f than green's %.1f",
			added, base, luma(lipgloss.Green))
	}
}

func TestNoBackgroundPaintsNoSurface(t *testing.T) {
	// Slot 0 is the background on many dark palettes, so a selection painted in
	// it is invisible exactly where it was needed.
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

	// Borders are drawn runes rather than fills, so they still take a slot.
	if _, ok := th.Border.(xansi.BasicColor); !ok {
		t.Errorf("Border = %T, want a slot when nothing was detected", th.Border)
	}
}

// With no background to blend against, a shade pinned to RGB is one the terminal
// has no say in.
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
	// Pairing is what stops a light terminal drawing dark-theme source.
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
