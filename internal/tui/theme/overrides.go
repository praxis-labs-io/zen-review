package theme

import (
	"fmt"
	"image/color"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// A key missing here is refused by name rather than ignored: a dropped color reads as a broken theme.
var setters = map[string]func(*Theme, color.Color){
	"text":               func(t *Theme, c color.Color) { t.Text = c },
	"accent":             func(t *Theme, c color.Color) { t.Accent = c },
	"subtle":             func(t *Theme, c color.Color) { t.Subtle = c },
	"muted":              func(t *Theme, c color.Color) { t.Muted = c },
	"inverted":           func(t *Theme, c color.Color) { t.Inverted = c },
	"success":            func(t *Theme, c color.Color) { t.Success = c },
	"warning":            func(t *Theme, c color.Color) { t.Warning = c },
	"error":              func(t *Theme, c color.Color) { t.Error = c },
	"actor":              func(t *Theme, c color.Color) { t.Actor = c },
	"selectedBackground": func(t *Theme, c color.Color) { t.SelectedBackground = c },
	"addedBackground":    func(t *Theme, c color.Color) { t.AddedBackground = c },
	"removedBackground":  func(t *Theme, c color.Color) { t.RemovedBackground = c },
	"border":             func(t *Theme, c color.Color) { t.Border = c },
	"borderSubtle":       func(t *Theme, c color.Color) { t.BorderSubtle = c },
	"borderMuted":        func(t *Theme, c color.Color) { t.BorderMuted = c },
}

// Keys lists every token override key, sorted.
func Keys() []string {
	keys := make([]string, 0, len(setters))
	for k := range setters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Inputs are the colors the derivation blends from, each "" where config names none.
// They take hex only, since a slot blended through RGBA() is its canonical value, not the terminal's.
type Inputs struct {
	Background string
	Foreground string
	AddedHue   string
	RemovedHue string
}

func (in Inputs) named() []struct{ key, value string } {
	return []struct{ key, value string }{
		{"background", in.Background},
		{"foreground", in.Foreground},
		{"addedHue", in.AddedHue},
		{"removedHue", in.RemovedHue},
	}
}

// Overrides is what config pins over the theme derived from the terminal.
type Overrides struct {
	tokens map[string]string
	inputs Inputs
}

func NewOverrides(tokens map[string]string, inputs Inputs) Overrides {
	return Overrides{tokens: tokens, inputs: inputs}
}

// Validate reports the first unknown token or unusable value, naming its key. A token takes
// hex or an ANSI index from 0 to 255 as a bare number; an input takes hex only.
func (o Overrides) Validate() error {
	for _, in := range o.inputs.named() {
		if in.value == "" {
			continue
		}
		if _, ok := parseHex(in.value); !ok {
			return fmt.Errorf("%s: %q is not a hex color like \"#0a0d14\". "+
				"It is blended, and an ANSI index would blend as its standard color rather than your terminal's", in.key, in.value)
		}
	}
	for _, key := range sorted(o.tokens) {
		if _, ok := setters[key]; !ok {
			return fmt.Errorf("theme: unknown color %q. Known: %s", key, strings.Join(Keys(), ", "))
		}
		if _, ok := parse(o.tokens[key]); !ok {
			return fmt.Errorf("theme: %s: %q is not a color. Want a hex like \"#c4a7e7\" or an ANSI index from 0 to 255",
				key, o.tokens[key])
		}
	}
	return nil
}

// Resolve derives the theme from reported with the named inputs outranking it field by field,
// paints the background when paintBackground is set, then applies the token overrides.
// A value that does not parse is skipped.
func (o Overrides) Resolve(reported Surface, paintBackground bool) Theme {
	pin := func(into *color.Color, value string) {
		if c, ok := parseHex(value); ok {
			*into = c
		}
	}
	pin(&reported.Background, o.inputs.Background)
	pin(&reported.Foreground, o.inputs.Foreground)
	pin(&reported.Green, o.inputs.AddedHue)
	pin(&reported.Red, o.inputs.RemovedHue)

	t := Terminal(reported)
	if paintBackground {
		t.Background = reported.Background
	}

	for _, key := range sorted(o.tokens) {
		set, ok := setters[key]
		if !ok {
			continue
		}
		if c, ok := parse(o.tokens[key]); ok {
			set(&t, c)
		}
	}
	return t
}

// Range-checked because lipgloss.Color packs an integer past 255 as RGB rather than failing.
func parse(s string) (color.Color, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > 255 {
			return nil, false
		}
		return lipgloss.Color(s), true
	}
	return parseHex(s)
}

func parseHex(s string) (color.Color, bool) {
	if !strings.HasPrefix(s, "#") {
		return nil, false
	}
	c := lipgloss.Color(s)
	if _, blank := c.(lipgloss.NoColor); blank {
		return nil, false
	}
	return c, true
}

func sorted(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
