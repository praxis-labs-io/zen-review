package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/config"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

func configured(t *testing.T, cfg config.Config, c review.Changeset, comments []store.Comment, width, height int) *screen {
	t.Helper()

	r := app.Reload{
		Base:       review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"},
		Generation: review.Generation{ID: 2, Seq: 2},
		Changeset:  c,
		Comments:   comments,
	}

	src := &source{at: r}
	s := &screen{t: t, m: app.New(testtheme.Dark, src, "zen-review", r, app.Launch{Config: cfg}), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

func TestASplitDefaultOpensSideBySide(t *testing.T) {
	got := configured(t, config.Config{Split: true}, testchangeset.Nested(t), nil, 120, 16).press("n", "n").frame()
	want := open(t, 120, 16).press("n", "n", "|").frame()

	if got != want {
		t.Errorf("split in config did not open side by side:\n%s\nwant:\n%s", got, want)
	}
}

func TestASplitDefaultWaitsForTheWidth(t *testing.T) {
	s := configured(t, config.Config{Split: true}, testchangeset.Nested(t), nil, 100, 16).press("n", "n")

	if got, want := s.frame(), open(t, 100, 16).press("n", "n").frame(); got != want {
		t.Errorf("a pane too narrow to split did not open unified:\n%s\nwant:\n%s", got, want)
	}
	if bar := s.bar(); strings.Contains(bar, "short") {
		t.Errorf("bar = %q, want the deferred split to say nothing", bar)
	}

	s.send(tea.WindowSizeMsg{Width: 120, Height: 16})
	if got, want := s.frame(), open(t, 120, 16).press("n", "n", "|").frame(); got != want {
		t.Errorf("widening the pane did not split it:\n%s\nwant:\n%s", got, want)
	}
}

func TestBarOnADeferredSplitNamesTheShortfall(t *testing.T) {
	s := configured(t, config.Config{Split: true}, testchangeset.Nested(t), nil, 100, 16).press("n", "n", "|")

	if bar := s.bar(); !strings.Contains(bar, "more columns") {
		t.Errorf("bar = %q, want | on a deferred split to name the shortfall", bar)
	}
	s.send(tea.WindowSizeMsg{Width: 120, Height: 16})
	s.press("esc")
	if got, want := s.frame(), open(t, 120, 16).press("n", "n", "|", "esc").frame(); got != want {
		t.Errorf("the refused press dropped the split request:\n%s\nwant:\n%s", got, want)
	}
}

func TestFoldSettledOffOpensAResolvedCard(t *testing.T) {
	settled := answered(testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "an answered question\nand the rest of it"))
	open := false

	for _, tc := range []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{"by default", config.Config{}, false},
		{"with foldSettled off", config.Config{FoldSettled: &open}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := configured(t, tc.cfg, testchangeset.Nested(t), []store.Comment{settled}, 100, 24).press("shift+tab").frame()
			if strings.Contains(got, "and the rest of it") != tc.want {
				t.Errorf("the resolved card's body showing = %v, want %v:\n%s", !tc.want, tc.want, got)
			}
		})
	}
}

const tabbedPatch = `diff --git a/a.go b/a.go
index 1111111111111111111111111111111111111111..2222222222222222222222222222222222222222 100644
--- a/a.go
+++ b/a.go
@@ -1,1 +1,1 @@
-	old
+	new
`

func TestTabWidthSetsTheIndent(t *testing.T) {
	c := testchangeset.Derive(t, tabbedPatch)

	for _, tc := range []struct {
		name  string
		width int
		want  string
	}{
		{"the default", 0, "+     new"},
		{"eight", 8, "+         new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := configured(t, config.Config{TabWidth: tc.width}, c, nil, 100, 12).frame()
			if !strings.Contains(got, tc.want) {
				t.Errorf("want %q in the frame:\n%s", tc.want, got)
			}
		})
	}
}

func TestTheLaunchThemeTakesTheConfig(t *testing.T) {
	derived := theme.Terminal(testtheme.Surface)

	tests := []struct {
		name    string
		cfg     config.Config
		surface *theme.Surface
		warning string
		check   func(t *testing.T, got theme.Theme)
	}{
		{
			name: "nothing set",
			check: func(t *testing.T, got theme.Theme) {
				if got != derived {
					t.Errorf("theme = %+v, want the derived one", got)
				}
			},
		},
		{
			name: "a known syntax style",
			cfg:  config.Config{SyntaxTheme: "dracula"},
			check: func(t *testing.T, got theme.Theme) {
				if got.Syntax != "dracula" {
					t.Errorf("Syntax = %q, want dracula", got.Syntax)
				}
			},
		},
		{
			name:    "an unknown syntax style",
			cfg:     config.Config{SyntaxTheme: "nope"},
			warning: `no style "nope"`,
			check: func(t *testing.T, got theme.Theme) {
				if got.Syntax != derived.Syntax {
					t.Errorf("Syntax = %q, want the derived %q", got.Syntax, derived.Syntax)
				}
			},
		},
		{
			name:    "a bad color drops the set",
			cfg:     config.Config{Theme: map[string]string{"accent": "#ff0000", "chartreuse": "1"}},
			warning: "chartreuse",
			check: func(t *testing.T, got theme.Theme) {
				if got != derived {
					t.Errorf("theme = %+v, want the derived one with the whole set dropped", got)
				}
			},
		},
		{
			name:    "an ANSI index as the background",
			cfg:     config.Config{Background: "0", PaintBackground: true},
			warning: "background",
			check: func(t *testing.T, got theme.Theme) {
				if got.Subtle != derived.Subtle {
					t.Error("the refused background still drove the derivation")
				}
			},
		},
		{
			name: "paintBackground",
			cfg:  config.Config{PaintBackground: true},
			check: func(t *testing.T, got theme.Theme) {
				if got.Background != testtheme.Surface.Background {
					t.Errorf("Background = %v, want the reported one painted", got.Background)
				}
			},
		},
		{
			name:    "a foreground with no background anywhere",
			cfg:     config.Config{Foreground: "#dce3ee"},
			surface: &theme.Surface{},
			warning: "until the config names a background",
		},
		{
			name:    "a foreground over a named background",
			cfg:     config.Config{Foreground: "#dce3ee", Background: "#0a0d14"},
			surface: &theme.Surface{},
		},
		{
			name:    "both problems at once",
			cfg:     config.Config{SyntaxTheme: "nope", Theme: map[string]string{"accent": "nonsense"}},
			warning: `, so no color from the config is applied; syntaxTheme`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testtheme.Surface
			if tt.surface != nil {
				s = *tt.surface
			}
			got, err := app.LaunchTheme(tt.cfg, s)
			switch {
			case tt.warning == "" && err != nil:
				t.Errorf("warning = %v, want none", err)
			case tt.warning != "" && (err == nil || !strings.Contains(err.Error(), tt.warning)):
				t.Errorf("warning = %v, want it to say %q", err, tt.warning)
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}
