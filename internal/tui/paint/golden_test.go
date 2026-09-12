package paint_test

import (
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/golden"
	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
)

func compare(t *testing.T, name, got string) {
	t.Helper()
	golden.Compare(t, name, []byte(got))
}

// tokens are hand-built so a Chroma upgrade cannot move a golden.
func tokens() []syntax.Token {
	return []syntax.Token{
		{Text: "const ", Color: testtheme.Dark.Accent},
		{Text: "n", Color: testtheme.Dark.Text},
		{Text: " = "},
		{Text: "4", Color: testtheme.Dark.Warning},
	}
}

func painter() paint.Painter {
	return paint.Painter{Theme: testtheme.Dark}
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
				{Text: "return", Color: testtheme.Dark.Accent},
				{Text: "\tnil"},
			}},
			40,
		},
		{
			"clipped",
			paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{
				{Text: "if err != nil { return fmt.Errorf(\"painting: %w\", err) }", Color: testtheme.Dark.Text},
			}},
			24,
		},
		{
			"clipped_wide",
			paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{
				{Text: "// 日本語のコメント", Color: testtheme.Dark.Subtle},
			}},
			21,
		},
		{
			"fill_override",
			paint.Line{Kind: paint.Added, New: 12, Tokens: tokens(), Fill: testtheme.Dark.SelectedBackground},
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
	compare(t, "hunk_header", painter().HunkHeader(paint.Header{Text: "@@ -11,4 +12,6 @@ func Paint()"}, paint.CodeColumn(paint.Gutter(1235)), 40))
}

func TestGoldenHunkHeaderMarked(t *testing.T) {
	compare(t, "hunk_header_marked", painter().HunkHeader(paint.Header{
		Text:   "@@ -11,4 +12,6 @@ func Paint()",
		Marker: "▸",
		Fill:   testtheme.Dark.SelectedBackground,
	}, paint.CodeColumn(paint.Gutter(1235)), 40))
}

func TestGoldenHunkHeaderBadged(t *testing.T) {
	compare(t, "hunk_header_badged", painter().HunkHeader(paint.Header{
		Text:   "@@ -11,4 +12,6 @@ func Paint()",
		Marker: "▸",
		Badge:  "●",
		Fill:   testtheme.Dark.SelectedBackground,
	}, paint.CodeColumn(paint.Gutter(1235)), 40))
}

func TestGoldenBody(t *testing.T) {
	compare(t, "body_removed", painter().Body(paint.Line{
		Kind:   paint.Removed,
		Tokens: tokens(),
	}, 30))
}

func TestGoldenBodyClipped(t *testing.T) {
	compare(t, "body_clipped", painter().Body(paint.Line{
		Kind:   paint.Removed,
		Tokens: tokens(),
	}, 10))
}

func TestGoldenHalves(t *testing.T) {
	tests := []struct {
		name   string
		line   paint.Line
		gutter int
		width  int
	}{
		{"half_added", paint.Line{Kind: paint.Added, New: 120, Tokens: tokens()}, 3, 26},
		{"half_removed", paint.Line{Kind: paint.Removed, Old: 119, Tokens: tokens()}, 3, 26},
		{"half_context", paint.Line{Kind: paint.Context, New: 120, Tokens: tokens()}, 3, 26},

		{"half_blank", paint.Line{}, 3, 26},

		{"half_clipped", paint.Line{Kind: paint.Added, New: 120, Tokens: tokens()}, 3, 14},
		{"half_wide_gutter", paint.Line{Kind: paint.Added, New: 42100, Tokens: tokens()}, 5, 26},
		{"half_filled", paint.Line{
			Kind: paint.Context, New: 120, Tokens: tokens(),
			Fill: testtheme.Dark.SelectedBackground,
		}, 3, 26},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compare(t, tt.name, painter().Half(tt.line, tt.gutter, tt.width))
		})
	}
}

func TestGoldenBarred(t *testing.T) {
	compare(t, "line_barred", painter().Line(paint.Line{
		Kind: paint.Added, New: 120, Tokens: tokens(),
		Fill: testtheme.Dark.SelectedBackground,
		Bar:  testtheme.Dark.Accent,
	}, 3, 40))
}

func TestGoldenHalfBarred(t *testing.T) {
	compare(t, "half_barred", painter().Half(paint.Line{
		Kind: paint.Added, New: 120, Tokens: tokens(),
		Fill: testtheme.Dark.SelectedBackground,
		Bar:  testtheme.Dark.Accent,
	}, 3, 26))
}

func TestGoldenHeaderBarred(t *testing.T) {
	compare(t, "hunk_header_barred", painter().HunkHeader(paint.Header{
		Text: "@@ -11,4 +12,6 @@ func Paint()", Badge: "○",
		Fill: testtheme.Dark.SelectedBackground,
		Bar:  testtheme.Dark.Accent,
	}, paint.CodeColumn(3), 40))
}
