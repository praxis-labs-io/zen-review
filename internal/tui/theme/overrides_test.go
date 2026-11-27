package theme_test

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

func tokens(pairs ...string) map[string]string {
	m := make(map[string]string, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return m
}

func sameRGB(a, b color.Color) bool {
	ar, ag, ab := rgb(a)
	br, bg, bb := rgb(b)
	return ar == br && ag == bg && ab == bb
}

func TestATokenOverrideWinsAndLeavesTheRestDerived(t *testing.T) {
	got := theme.NewOverrides(tokens("accent", "#ff0000"), theme.Inputs{}).Resolve(dark, false)
	derived := theme.Terminal(dark)

	if !sameRGB(got.Accent, lipgloss.Color("#ff0000")) {
		t.Errorf("Accent = %v, want the override", got.Accent)
	}
	if got.Success != derived.Success || got.Subtle != derived.Subtle {
		t.Error("an override for one token changed another, want the rest left derived")
	}
}

func TestATokenTakesASlotIndex(t *testing.T) {
	got := theme.NewOverrides(tokens("accent", "4"), theme.Inputs{}).Resolve(dark, false)
	if got.Accent != lipgloss.Blue {
		t.Errorf("Accent = %v, want slot 4", got.Accent)
	}
}

func TestEveryColorFieldHasAKey(t *testing.T) {
	want := []string{"accent", "actor", "addedBackground", "border", "borderMuted", "borderSubtle",
		"error", "inverted", "muted", "removedBackground", "selectedBackground", "subtle",
		"success", "text", "warning"}
	if got := strings.Join(theme.Keys(), " "); got != strings.Join(want, " ") {
		t.Errorf("Keys() = %s, want %s", got, strings.Join(want, " "))
	}
}

func TestValidateNamesWhatItRefuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tokens map[string]string
		inputs theme.Inputs
		want   string
	}{
		{"unknown token", tokens("chartreuse", "#ff0000"), theme.Inputs{}, "chartreuse"},
		{"unparseable token", tokens("accent", "nonsense"), theme.Inputs{}, "accent"},
		{"index past the range", tokens("accent", "256"), theme.Inputs{}, "accent"},
		{"negative index", tokens("accent", "-1"), theme.Inputs{}, "accent"},
		{"half a hex", tokens("accent", "#ff00"), theme.Inputs{}, "accent"},
		{"an index as the background", nil, theme.Inputs{Background: "0"}, "background"},
		{"an index as the foreground", nil, theme.Inputs{Foreground: "15"}, "foreground"},
		{"an index as the added hue", nil, theme.Inputs{AddedHue: "2"}, "addedHue"},
		{"an index as the removed hue", nil, theme.Inputs{RemovedHue: "1"}, "removedHue"},
		{"an unparseable background", nil, theme.Inputs{Background: "navy"}, "background"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := theme.NewOverrides(tc.tokens, tc.inputs).Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v, want an error naming %s", err, tc.want)
			}
		})
	}
}

func TestValidOverridesPass(t *testing.T) {
	o := theme.NewOverrides(tokens("accent", "#c4a7e7", "selectedBackground", "#2a283e", "error", "1", "muted", "255"),
		theme.Inputs{Background: "#0a0d14", Foreground: "#dce3ee", AddedHue: "#6ee6b8", RemovedHue: "#f2707a"})
	if err := o.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestANamedBackgroundOutranksTheReportedOne(t *testing.T) {
	got := theme.NewOverrides(nil, theme.Inputs{Background: "#faf4ed"}).Resolve(dark, false)
	want := theme.Terminal(theme.Surface{Background: lightBG, Foreground: darkFG})

	if got.Subtle != want.Subtle || got.Syntax != theme.SyntaxLight {
		t.Error("the named background did not drive the derivation")
	}
	if got.Background != nil {
		t.Errorf("Background = %v, want nil with paintBackground off", got.Background)
	}
}

func TestPaintBackgroundPaintsTheBackgroundTheDerivationUsed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inputs theme.Inputs
		s      theme.Surface
		want   color.Color
	}{
		{"reported", theme.Inputs{}, dark, darkBG},
		{"named", theme.Inputs{Background: "#faf4ed"}, dark, lightBG},
		{"nothing answered", theme.Inputs{}, theme.Surface{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := theme.NewOverrides(nil, tc.inputs).Resolve(tc.s, true).Background
			if tc.want == nil {
				if got != nil {
					t.Errorf("Background = %v, want nil", got)
				}
				return
			}
			if got == nil || !sameRGB(got, tc.want) {
				t.Errorf("Background = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNamedHuesLeanTheTints(t *testing.T) {
	blue, orange := "#0000ff", "#ff8800"
	got := theme.NewOverrides(nil, theme.Inputs{AddedHue: blue, RemovedHue: orange}).Resolve(dark, false)
	want := theme.Terminal(theme.Surface{Background: darkBG, Foreground: darkFG,
		Green: lipgloss.Color(blue), Red: lipgloss.Color(orange)})

	if got.AddedBackground != want.AddedBackground || got.RemovedBackground != want.RemovedBackground {
		t.Error("the named hues did not reach the tints")
	}
	if got.Success != lipgloss.Green {
		t.Errorf("Success = %v, want it left on its slot", got.Success)
	}
}

func TestATokenOverridesAnInputsDerivation(t *testing.T) {
	got := theme.NewOverrides(tokens("addedBackground", "#123456"), theme.Inputs{AddedHue: "#0000ff"}).Resolve(dark, false)
	if !sameRGB(got.AddedBackground, lipgloss.Color("#123456")) {
		t.Errorf("AddedBackground = %v, want the token over the derived tint", got.AddedBackground)
	}
}

func TestResolveWithNothingNamedIsTheTerminal(t *testing.T) {
	var o theme.Overrides
	if got, want := o.Resolve(dark, false), theme.Terminal(dark); got != want {
		t.Errorf("Resolve = %+v, want Terminal's %+v", got, want)
	}
}
