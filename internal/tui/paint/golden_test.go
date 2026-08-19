package paint_test

import (
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/golden"
	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// These goldens keep their escapes, where the frame ones are stripped: what a
// painted row gets wrong is the colour and the fill. `cat` one to read it.
func compare(t *testing.T, name, got string) {
	t.Helper()
	golden.Compare(t, name, []byte(got))
}

// Tokens are hand-built rather than taken from Chroma, so a golden file records
// what the painter did and not what a lexer version thought of a line.
func tokens() []syntax.Token {
	return []syntax.Token{
		{Text: "const ", Color: theme.RosePineMoon.Accent},
		{Text: "n", Color: theme.RosePineMoon.Text},
		{Text: " = "},
		{Text: "4", Color: theme.RosePineMoon.Warning},
	}
}

func painter() paint.Painter {
	return paint.Painter{Theme: theme.RosePineMoon}
}

func TestGoldenLines(t *testing.T) {
	tests := []struct {
		name  string
		line  paint.Line
		width int
	}{
		{"line_added", paint.Line{Kind: paint.Added, New: 12, Tokens: tokens()}, 40},
		{"line_removed", paint.Line{Kind: paint.Removed, Old: 11, Tokens: tokens()}, 40},
		{"line_context", paint.Line{Kind: paint.Context, Old: 11, New: 12, Tokens: tokens()}, 40},
		{
			"tabs",
			paint.Line{Kind: paint.Context, Old: 11, New: 12, Tokens: []syntax.Token{
				{Text: "\t"},
				{Text: "return", Color: theme.RosePineMoon.Accent},
				{Text: "\tnil"},
			}},
			40,
		},
		{
			"clipped",
			paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{
				{Text: "if err != nil { return fmt.Errorf(\"painting: %w\", err) }", Color: theme.RosePineMoon.Text},
			}},
			24,
		},
		// An odd remainder against two-cell runes is where the cut used to come
		// back a column short of the pane.
		{
			"clipped_wide",
			paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{
				{Text: "// 日本語のコメント", Color: theme.RosePineMoon.Subtle},
			}},
			21,
		},
		{
			"fill_override",
			paint.Line{Kind: paint.Added, New: 12, Tokens: tokens(), Fill: theme.RosePineMoon.SelectedBackground},
			40,
		},
		{"wide_gutter", paint.Line{Kind: paint.Context, Old: 1234, New: 1235, Tokens: tokens()}, 40},
	}

	p := painter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gutter := paint.Gutter(max(tt.line.Old, tt.line.New))
			compare(t, tt.name, p.Line(tt.line, gutter, tt.width))
		})
	}
}

// A held-open column has to be exactly as wide as a filled one, or the marker
// moves between a line that has both numbers and a line that has one.
func TestGoldenOneSided(t *testing.T) {
	p := painter()
	gutter := paint.Gutter(120)

	rows := []string{
		p.Line(paint.Line{Kind: paint.Added, New: 120, Tokens: tokens()}, gutter, 40),
		p.Line(paint.Line{Kind: paint.Removed, Old: 119, Tokens: tokens()}, gutter, 40),
		p.Line(paint.Line{Kind: paint.Context, Old: 119, New: 120, Tokens: tokens()}, gutter, 40),
	}
	compare(t, "one_sided", strings.Join(rows, "\n"))
}

func TestGoldenHunkHeader(t *testing.T) {
	compare(t, "hunk_header", painter().HunkHeader(paint.Header{Text: "@@ -11,4 +12,6 @@ func Paint()"}, paint.Gutter(1235), 40))
}

// The heading a cursor is on: filled to the edge, with the mark in the column
// the change marks under it use.
func TestGoldenHunkHeaderMarked(t *testing.T) {
	compare(t, "hunk_header_marked", painter().HunkHeader(paint.Header{
		Text:   "@@ -11,4 +12,6 @@ func Paint()",
		Marker: "▸",
		Fill:   theme.RosePineMoon.SelectedBackground,
	}, paint.Gutter(1235), 40))
}

// Both glyphs at once, which is a cursor on a heading that carries a state.
func TestGoldenHunkHeaderBadged(t *testing.T) {
	compare(t, "hunk_header_badged", painter().HunkHeader(paint.Header{
		Text:   "@@ -11,4 +12,6 @@ func Paint()",
		Marker: "▸",
		Badge:  "●",
		Fill:   theme.RosePineMoon.SelectedBackground,
	}, paint.Gutter(1235), 40))
}
