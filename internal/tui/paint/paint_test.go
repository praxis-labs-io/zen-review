package paint_test

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

func TestGutterHoldsTwoColumnsUntilThereAreMoreDigits(t *testing.T) {
	tests := []struct {
		widest int
		want   int
	}{
		{0, 2},
		{1, 2},
		{9, 2},
		{10, 2},
		{99, 2},
		{100, 3},
		{999, 3},
		{1000, 4},
		{12345, 5},
	}

	for _, tt := range tests {
		if got := paint.Gutter(tt.widest); got != tt.want {
			t.Errorf("Gutter(%d) = %d, want %d", tt.widest, got, tt.want)
		}
	}
}

// The gutter and the numbers going into it are computed apart, and disagreeing
// misaligns a long file. The floor of two hides that until the fourth digit.
func TestEveryRowIsTheSameWidthUpToTheGutterWhateverTheNumber(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}

	for _, widest := range []int{4, 42, 421, 4210, 42100} {
		gutter := paint.Gutter(widest)
		want := markerColumn(t, p, paint.Line{Kind: paint.Added, New: widest}, gutter)

		for _, n := range []int{1, 9, 10, widest} {
			got := markerColumn(t, p, paint.Line{Kind: paint.Added, New: n}, gutter)
			if got != want {
				t.Errorf("widest %d, line %d: marker at column %d, want %d", widest, n, got, want)
			}
		}
	}
}

// A caller indents its own block to CodeColumn, so the number it is handed has
// to be where Line actually puts the source rather than a second guess at it.
func TestCodeColumnIsWhereTheSourceStarts(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	const code = "n = 4"

	for _, widest := range []int{9, 120, 4210, 42100} {
		gutter := paint.Gutter(widest)
		row := xansi.Strip(p.Line(
			paint.Line{Kind: paint.Context, Old: widest, New: widest, Tokens: []syntax.Token{{Text: code}}},
			gutter, 60))

		i := strings.Index(row, code)
		if i < 0 {
			t.Fatalf("gutter %d: no source in %q", gutter, row)
		}
		if at := lipgloss.Width(row[:i]); at != paint.CodeColumn(gutter) {
			t.Errorf("gutter %d: the source starts at column %d, CodeColumn says %d",
				gutter, at, paint.CodeColumn(gutter))
		}
	}
}

// markerColumn is where the +/− lands once the escapes are stripped.
func markerColumn(t *testing.T, p paint.Painter, l paint.Line, gutter int) int {
	t.Helper()
	plain := xansi.Strip(p.Line(l, gutter, 40))
	i := strings.IndexAny(plain, "+−")
	if i < 0 {
		t.Fatalf("no marker in %q", plain)
	}
	return lipgloss.Width(plain[:i])
}

// Every styled run ends in a reset that clears the background, so a tinted row
// has to reach the last cell or the block reads ragged down its right edge.
func TestARowWithATintIsPaintedToTheFullWidth(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}

	tests := []struct {
		name string
		line paint.Line
	}{
		{"added", paint.Line{Kind: paint.Added, New: 12}},
		{"removed", paint.Line{Kind: paint.Removed, Old: 11}},
		{"context under a fill", paint.Line{Kind: paint.Context, Old: 11, New: 12, Fill: theme.RosePineMoon.SelectedBackground}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.line.Tokens = []syntax.Token{{Text: "n = 4"}}
			if got := lipgloss.Width(p.Line(tt.line, 2, 40)); got != 40 {
				t.Errorf("row width = %d, want the full 40", got)
			}
		})
	}
}

// A context line has no background to run out, and padding it would hand the
// caller trailing cells it has to reason about.
func TestAContextRowIsLeftShort(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	row := p.Line(paint.Line{Kind: paint.Context, Old: 11, New: 12, Tokens: []syntax.Token{{Text: "n = 4"}}}, 2, 40)

	if got := lipgloss.Width(row); got >= 40 {
		t.Errorf("row width = %d, want it to stop at the code", got)
	}
}

func TestFillBeatsTheKindTint(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	line := paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{{Text: "n = 4"}}, Fill: theme.RosePineMoon.SelectedBackground}
	row := p.Line(line, 2, 40)

	if !strings.Contains(row, bgSeq(theme.RosePineMoon.SelectedBackground)) {
		t.Error("the fill is not on the row")
	}
	if strings.Contains(row, bgSeq(theme.RosePineMoon.AddedBackground)) {
		t.Error("the added tint painted over the fill")
	}
}

// A theme leaving a surface nil means "leave the terminal's own showing", and
// handing that to Lipgloss is what breaks a transparent background.
func TestARowTakesNoBackgroundFromAThemeThatDefinesNone(t *testing.T) {
	bare := theme.Theme{Name: "bare", Text: theme.RosePineMoon.Text, Subtle: theme.RosePineMoon.Subtle}
	p := paint.Painter{Theme: bare}
	row := p.Line(paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{{Text: "n = 4"}}}, 2, 40)

	if strings.Contains(row, "48;2;") {
		t.Errorf("row set a background the theme does not define: %q", row)
	}
}

func TestTabsExpandToTheTabWidth(t *testing.T) {
	row := func(p paint.Painter, code string) string {
		return xansi.Strip(p.Line(paint.Line{
			Kind: paint.Context, Old: 11, New: 12, Tokens: []syntax.Token{{Text: code}},
		}, 2, 60))
	}

	tests := []struct {
		name  string
		width int
		want  int
	}{
		{"unset means four", 0, 4},
		{"two", 2, 2},
		{"eight", 8, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := paint.Painter{Theme: theme.RosePineMoon, TabWidth: tt.width}
			tabbed := row(p, "\tn")

			if strings.Contains(tabbed, "\t") {
				t.Fatalf("a raw tab survived: %q", tabbed)
			}
			if got := lipgloss.Width(tabbed) - lipgloss.Width(row(p, "n")); got != tt.want {
				t.Errorf("tab took %d cells, want %d (%q)", got, tt.want, tabbed)
			}
		})
	}
}

// A wrapped row of code puts its tail under the gutter and every row below it
// out of step, so overflow is cut instead.
func TestARowWiderThanThePaneIsClippedNotWrapped(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	long := strings.Repeat("n = 4; ", 20)
	row := p.Line(paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{{Text: long}}}, 2, 24)

	if strings.Contains(row, "\n") {
		t.Error("the row wrapped")
	}
	if got := lipgloss.Width(row); got != 24 {
		t.Errorf("row width = %d, want 24", got)
	}
	if !strings.Contains(xansi.Strip(row), "…") {
		t.Error("the cut is not marked")
	}
}

// A clipped row still has to reach the pane edge, and a cut landing on a
// two-cell rune comes back short. Each width is its own case.
func TestAClippedRowWithWideRunesStillFillsTheWidth(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}

	for _, code := range []string{"日本語のコメントです", "🌱 seedling 🌱 seedling"} {
		for width := 18; width <= 25; width++ {
			row := p.Line(paint.Line{Kind: paint.Added, New: 12, Tokens: []syntax.Token{{Text: code}}}, 2, width)
			if got := lipgloss.Width(row); got != width {
				t.Errorf("%q at width %d painted %d cells", code, width, got)
			}
		}
	}
}

// Clip is the primitive both tools truncate with, and it marks either way. A
// caller that wants short content left alone checks the width itself.
func TestClipAlwaysMarksTheCut(t *testing.T) {
	plain := lipgloss.NewStyle()

	tests := []struct {
		name    string
		content string
		width   int
		want    string
	}{
		{"no room at all", "hello", 0, ""},
		{"room for the mark alone", "hello", 1, "…"},
		{"cut", "hello", 3, "he…"},
		{"content that already fits", "hi", 5, "hi…"},
		// A two-cell rune cannot half-fill the last column, so unpadded the row
		// comes back short and its tint stops before the pane edge.
		{"cut landing on a wide rune", "日本語", 4, "日 …"},
		{"wide runes cut on the boundary", "日本語", 5, "日本…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xansi.Strip(paint.Clip(tt.content, tt.width, plain)); got != tt.want {
				t.Errorf("Clip(%q, %d) = %q, want %q", tt.content, tt.width, got, tt.want)
			}
		})
	}
}

// The header sits over the source it introduces, and asserting only "past the
// marker" passes with it parked in the marker's own gap.
func TestTheHunkHeaderStartsAtTheCodeColumn(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}

	// An emoji is two cells. Each slot takes the space after its glyph rather
	// than a column of its own, or every row under the heading reads shifted.
	for _, marker := range []string{"", "▸", "🔵"} {
		for _, badge := range []string{"", "●", "🔵"} {
			for _, widest := range []int{9, 120, 4210} {
				gutter := paint.Gutter(widest)
				header := xansi.Strip(p.HunkHeader(
					paint.Header{Text: "@@ -1,2 +1,3 @@", Marker: marker, Badge: badge}, gutter, 60))
				indent := lipgloss.Width(header[:strings.Index(header, "@@")])

				if want := codeColumn(t, p, gutter); indent != want {
					t.Errorf("gutter %d, marker %q, badge %q: header starts at column %d, want the code column %d",
						gutter, marker, badge, indent, want)
				}
			}
		}
	}
}

// A badge takes a colour of its own, so a caller can run a ladder of states
// where the quiet end is quiet. nil keeps the marker's accent.
func TestABadgeTakesItsOwnColour(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	gutter := paint.Gutter(9)

	// The text is already accent, so subtle appearing at all is the badge and
	// nothing else. That is what makes either direction here worth asserting.
	plain := p.HunkHeader(paint.Header{Text: "@@ -1,2 +1,3 @@", Badge: "○"}, gutter, 60)
	if strings.Contains(plain, fgSeq(theme.RosePineMoon.Subtle)) {
		t.Errorf("a badge with no colour took one anyway: %q", plain)
	}

	own := p.HunkHeader(paint.Header{
		Text: "@@ -1,2 +1,3 @@", Badge: "○", BadgeColor: theme.RosePineMoon.Subtle,
	}, gutter, 60)
	if !strings.Contains(own, fgSeq(theme.RosePineMoon.Subtle)) {
		t.Errorf("the badge does not carry its own colour: %q", own)
	}
	if !strings.Contains(xansi.Strip(own), "○") {
		t.Errorf("the badge is missing: %q", own)
	}
}

// A heading the reader is not in is dimmed, so the column says which one they
// are in rather than only which hunk is which.
func TestAHeaderTakesItsOwnTextColour(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	gutter := paint.Gutter(9)

	plain := p.HunkHeader(paint.Header{Text: "@@ -1,2 +1,3 @@", Marker: "\u25b8"}, gutter, 60)
	if !strings.Contains(plain, fgSeq(theme.RosePineMoon.Accent)) {
		t.Errorf("a header with no colour of its own is not accent: %q", plain)
	}

	// The marker goes with the text. A lit caret on a dimmed line reads as two
	// things disagreeing about whether the reader is here.
	own := p.HunkHeader(paint.Header{
		Text: "@@ -1,2 +1,3 @@", Marker: "\u25b8", TextColor: theme.RosePineMoon.Muted,
	}, gutter, 60)
	if strings.Contains(own, fgSeq(theme.RosePineMoon.Accent)) {
		t.Errorf("a dimmed header still paints accent somewhere: %q", own)
	}
	if !strings.Contains(own, fgSeq(theme.RosePineMoon.Muted)) {
		t.Errorf("the header does not carry its own colour: %q", own)
	}
	if !strings.Contains(xansi.Strip(own), "@@ -1,2 +1,3 @@") {
		t.Errorf("the text is missing: %q", own)
	}
}

// The badge sits in the two blank columns before the marker, so a heading says
// what the cursor is on and what has been read without moving its text.
func TestABadgeSitsLeftOfTheMarker(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}

	for _, widest := range []int{9, 120, 4210} {
		gutter := paint.Gutter(widest)
		header := xansi.Strip(p.HunkHeader(
			paint.Header{Text: "@@ -1,2 +1,3 @@", Marker: "▸", Badge: "●"}, gutter, 60))

		badge, mark := strings.Index(header, "●"), strings.Index(header, "▸")
		if badge < 0 || mark < 0 {
			t.Fatalf("gutter %d: heading is missing a glyph: %q", gutter, header)
		}
		if badge > mark {
			t.Errorf("gutter %d: want the badge before the marker, got %q", gutter, header)
		}

		at := lipgloss.Width(header[:badge])
		if want := markerColumn(t, p, paint.Line{Kind: paint.Added, New: 1}, gutter) - 2; at != want {
			t.Errorf("gutter %d: the badge sits at column %d, want %d", gutter, at, want)
		}
	}
}

// A heading's marker goes in the column Line puts + and − in, so a mark on a
// hunk lines up with the change marks under it.
func TestAHeadersMarkerSitsInTheMarkerColumn(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}

	for _, widest := range []int{9, 120, 4210} {
		gutter := paint.Gutter(widest)
		header := xansi.Strip(p.HunkHeader(paint.Header{Text: "@@ -1,2 +1,3 @@", Marker: "▸"}, gutter, 60))

		at := lipgloss.Width(header[:strings.Index(header, "▸")])
		if want := markerColumn(t, p, paint.Line{Kind: paint.Added, New: 1}, gutter); at != want {
			t.Errorf("gutter %d: the marker sits at column %d, want the marker column %d", gutter, at, want)
		}
	}
}

// A filled heading is a block the same as a tinted row, and every styled run
// ends in a reset that clears the background with it.
func TestAFilledHeaderIsPaintedToTheFullWidth(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	header := p.HunkHeader(paint.Header{
		Text: "@@ -11,4 +12,6 @@", Fill: theme.RosePineMoon.SelectedBackground,
	}, 2, 40)

	if got := lipgloss.Width(header); got != 40 {
		t.Errorf("header width = %d, want the full 40", got)
	}
	if !strings.Contains(header, bgSeq(theme.RosePineMoon.SelectedBackground)) {
		t.Error("the fill is not on the heading")
	}
}

// A heading with no fill has no background to run out, the same as a context
// row, and padding it would hand the caller trailing cells to reason about.
func TestAHeaderWithNoFillIsLeftShort(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}

	if got := lipgloss.Width(p.HunkHeader(paint.Header{Text: "@@ -11,4 +12,6 @@"}, 2, 40)); got >= 40 {
		t.Errorf("header width = %d, want it to stop at the text", got)
	}
}

// codeColumn is where the source starts in a painted row, found by painting a
// token nothing else in the row can contain.
func codeColumn(t *testing.T, p paint.Painter, gutter int) int {
	t.Helper()

	plain := xansi.Strip(p.Line(paint.Line{
		Kind: paint.Added, New: 1, Tokens: []syntax.Token{{Text: "X"}},
	}, gutter, 60))

	i := strings.Index(plain, "X")
	if i < 0 {
		t.Fatalf("no code in %q", plain)
	}
	return lipgloss.Width(plain[:i])
}

func TestAHunkHeaderWiderThanThePaneIsClipped(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	header := p.HunkHeader(paint.Header{Text: "@@ -1,200 +1,240 @@ func AVeryLongEnclosingSymbolName()"}, 2, 24)

	if got := lipgloss.Width(header); got != 24 {
		t.Errorf("header width = %d, want 24", got)
	}
	if !strings.Contains(xansi.Strip(header), "…") {
		t.Error("the cut is not marked")
	}
}

// The cut takes the fill with it, or the block stops one cell short of the pane
// edge and the row reads ragged where it was clipped.
func TestAFilledHeaderWiderThanThePaneKeepsItsFillToTheEdge(t *testing.T) {
	p := paint.Painter{Theme: theme.RosePineMoon}
	header := p.HunkHeader(paint.Header{
		Text:   "@@ -1,200 +1,240 @@ func AVeryLongEnclosingSymbolName()",
		Marker: "▸",
		Fill:   theme.RosePineMoon.SelectedBackground,
	}, 2, 24)

	if got := lipgloss.Width(header); got != 24 {
		t.Errorf("header width = %d, want 24", got)
	}

	// The mark is its own run, so the escape opening it carries the fill.
	// Slicing back from the last background lands in the text's run instead.
	cut := strings.LastIndex(header, "…")
	if cut < 0 {
		t.Fatalf("the cut is not marked: %q", header)
	}
	open := strings.LastIndex(header[:cut], "\x1b[")
	if open < 0 {
		t.Fatalf("the cut mark carries no style: %q", header)
	}
	if !strings.Contains(header[open:cut], bgSeq(theme.RosePineMoon.SelectedBackground)) {
		t.Errorf("the cut mark lost the fill: %q", header)
	}
}

// paint and syntax compose or neither is worth having: Chroma's colors have to
// arrive as foregrounds over the row's own background.
func TestRealChromaTokensPaintOverTheRowsBackground(t *testing.T) {
	s, ok := syntax.New(theme.RosePineMoon.Syntax)
	if !ok {
		t.Fatalf("Chroma does not know %q", theme.RosePineMoon.Syntax)
	}

	p := paint.Painter{Theme: theme.RosePineMoon}
	lines := s.Lines("a.go", "const n = 4")
	row := p.Line(paint.Line{Kind: paint.Added, New: 12, Tokens: lines[0]}, 2, 40)

	if got := strings.Count(row, bgSeq(theme.RosePineMoon.AddedBackground)); got < 3 {
		t.Errorf("the tint survives %d runs, want it under every token", got)
	}
	if got := xansi.Strip(row); !strings.Contains(got, "const n = 4") {
		t.Errorf("the code did not come through: %q", got)
	}
}

func bgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func fgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}
