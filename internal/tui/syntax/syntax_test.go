package syntax_test

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
)

func colorizer(t *testing.T) syntax.Syntax {
	t.Helper()
	s, ok := syntax.New(testtheme.Dark.Syntax)
	if !ok {
		t.Fatalf("Chroma does not know %q", testtheme.Dark.Syntax)
	}
	return s
}

func TestTheDefaultThemeNamesAStyleChromaShips(t *testing.T) {
	colorizer(t)
}

func TestAnUnknownStyleStillColorizes(t *testing.T) {
	s, ok := syntax.New("not-a-chroma-style")
	if ok {
		t.Error("an unknown style reported itself as known")
	}
	if got := s.Lines("a.go", "package a"); len(got) != 1 {
		t.Errorf("lines = %d, want the code back anyway", len(got))
	}
}

func TestCodeIsSplitIntoLinesOfColoredTokens(t *testing.T) {
	s := colorizer(t)
	lines := s.Lines("a.go", "package main\n\nconst n = 4")

	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if text(lines[0]) != "package main" {
		t.Errorf("first line = %q", text(lines[0]))
	}
	if text(lines[1]) != "" {
		t.Errorf("blank line = %q, want empty", text(lines[1]))
	}
	if text(lines[2]) != "const n = 4" {
		t.Errorf("third line = %q", text(lines[2]))
	}
}

func TestAnEmptyBodyIsOneEmptyLine(t *testing.T) {
	s := colorizer(t)

	for _, path := range []string{"a.go", "a.txt", "no-extension"} {
		lines := s.Lines(path, "")
		if len(lines) != 1 {
			t.Errorf("%s: lines = %d, want 1", path, len(lines))
			continue
		}
		if got := text(lines[0]); got != "" {
			t.Errorf("%s: first line = %q, want empty", path, got)
		}
	}
}

func TestTokensCarryDifferentColorsWithinALine(t *testing.T) {
	s := colorizer(t)
	seen := make(map[string]bool)
	for _, tok := range s.Lines("a.go", "const n = 4")[0] {
		if tok.Color != nil {
			seen[hex(tok.Color)] = true
		}
	}
	if len(seen) < 2 {
		t.Errorf("the line came back in %d colors, want the keyword and the number apart", len(seen))
	}
}

func TestTheLexerFollowsTheFileName(t *testing.T) {
	s := colorizer(t)
	goLine := colors(s.Lines("a.go", "package main")[0])
	txtLine := colors(s.Lines("a.txt", "package main")[0])

	if goLine == txtLine {
		t.Errorf("a.go and a.txt colored identically: %s", goLine)
	}
}

func TestTokensCarryNoEscapeSequencesOfTheirOwn(t *testing.T) {
	s := colorizer(t)
	for _, tok := range s.Lines("a.go", "const n = 4")[0] {
		if strings.Contains(tok.Text, "\x1b") {
			t.Errorf("token %q carries its own escape sequence", tok.Text)
		}
	}
}

func TestABackgroundStaysWithTheCaller(t *testing.T) {
	base := lipgloss.NewStyle().Background(testtheme.Dark.SelectedBackground)

	s := colorizer(t)
	var b strings.Builder
	for _, tok := range s.Lines("a.go", "const n = 4")[0] {
		style := base
		if tok.Color != nil {
			style = base.Foreground(tok.Color)
		}
		b.WriteString(style.Render(tok.Text))
	}

	r, g, bl, _ := testtheme.Dark.SelectedBackground.RGBA()
	want := fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, bl>>8)
	if got := strings.Count(b.String(), want); got < 3 {
		t.Errorf("the caller's background survives %d runs, want it on every token", got)
	}
}

func TestTheSameFileIsOnlyTokenisedOnce(t *testing.T) {
	s := colorizer(t)
	first := s.Lines("a.go", "const n = 4")
	second := s.Lines("a.go", "const n = 4")

	if &first[0] != &second[0] {
		t.Error("the second call re-tokenised instead of answering from the cache")
	}
}

func text(tokens []syntax.Token) string {
	var b strings.Builder
	for _, tok := range tokens {
		b.WriteString(tok.Text)
	}
	return b.String()
}

func colors(tokens []syntax.Token) string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		out = append(out, hex(tok.Color))
	}
	return strings.Join(out, ",")
}

func hex(c color.Color) string {
	if c == nil {
		return "none"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
