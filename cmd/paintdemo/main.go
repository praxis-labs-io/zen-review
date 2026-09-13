// Command paintdemo paints a canned diff to stdout, for judging a rendering change.
package main

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// width is narrow enough that a row overflows, since truncation only shows where something is cut.
const width = 76

type row struct {
	Kind     paint.Kind
	Old, New int
	Text     string
	Cursor   bool
}

type hunk struct {
	Header string

	Cursor bool
	Badged bool
	Rows   []row

	Pairs []pair
}

type pair struct{ left, right int }

// hunks reach four-digit line numbers, since two digits prove nothing about gutter alignment.
var hunks = []hunk{
	{
		Header: "@@ -41,8 +41,9 @@ func (p Painter) Line",
		Cursor: true,
		Rows: []row{
			{Kind: paint.Context, Old: 41, New: 41, Text: "// Line is one row: numbers, marker, source."},
			{Kind: paint.Removed, Old: 42, Text: "func (p Painter) Line(l Line, width int) string {"},
			{Kind: paint.Added, New: 42, Text: "func (p Painter) Line(l Line, gutter, width int) string {"},
			{Kind: paint.Removed, Old: 43, Text: "\tmarker := \" \""},
			{Kind: paint.Added, New: 43, Text: "\tmarker, tint := \" \", color.Color(nil)"},
			{Kind: paint.Context, Old: 44, New: 44, Text: "\tif l.Kind == Added {"},
			{Kind: paint.Removed, Old: 45, Text: "\t\tmarker = \"+\""},
			{Kind: paint.Added, New: 45, Text: "\t\tmarker, tint = \"+\", p.Theme.AddedBackground", Cursor: true},
			{Kind: paint.Context, Old: 46, New: 46, Text: "\t}"},
			{Kind: paint.Removed, Old: 47, Text: "\treturn marker + code(l.Tokens)"},
			{Kind: paint.Added, New: 47, Text: "\trow := background(lipgloss.NewStyle(), tint).Render(marker) + p.code(l.Tokens, base)"},
			{Kind: paint.Added, New: 48, Text: "\treturn clipTo(row, width, p.faint())"},
			{Kind: paint.Context, Old: 48, New: 49, Text: "}"},
		},
		Pairs: []pair{
			{0, 0}, {1, 2}, {3, 4}, {5, 5}, {6, 7}, {8, 8}, {9, 10}, {-1, 11}, {12, 12},
		},
	},
	{
		Header: "@@ -1229,3 +1230,3 @@ func Gutter(widest int) int",
		Badged: true,
		Rows: []row{
			{Kind: paint.Context, Old: 1229, New: 1230, Text: "func Gutter(widest int) int {"},
			{Kind: paint.Removed, Old: 1230, Text: "\treturn len(strconv.Itoa(widest))"},
			{Kind: paint.Added, New: 1231, Text: "\treturn max(gutterMin, len(strconv.Itoa(widest)))"},
			{Kind: paint.Context, Old: 1231, New: 1232, Text: "}"},
		},
		Pairs: []pair{{0, 0}, {1, 2}, {3, 3}},
	},
}

func main() {
	t := theme.Terminal(theme.Query(os.Stdin, os.Stdout))

	s, ok := syntax.New(t.Syntax)
	if !ok {
		fmt.Fprintf(os.Stderr, "paintdemo: Chroma does not know %q, using its default style\n", t.Syntax)
	}

	p := paint.Painter{Theme: t}
	gutter := paint.Gutter(widest())
	half := (width - 1) / 2

	oldSide := s.Lines("paint.go", source(paint.Removed))
	newSide := s.Lines("paint.go", source(paint.Added))

	out := []string{lipgloss.NewStyle().Foreground(t.Subtle).
		Render(fmt.Sprintf("syntax %s, pane %d columns, gutter %d", t.Syntax, width, gutter))}

	lines := painted(t, oldSide, newSide)

	at := 0
	for _, h := range hunks {
		out = append(out, p.HunkHeader(header(t, h), paint.CodeColumn(gutter), width))
		for range h.Rows {
			out = append(out, p.Line(lines[at], gutter, width))
			at++
		}
	}

	out = append(out, "", lipgloss.NewStyle().Foreground(t.Subtle).
		Render(fmt.Sprintf("side by side, %d columns each", half)))

	at = 0
	for _, h := range hunks {
		out = append(out, p.HunkHeader(header(t, h), paint.HalfColumn(gutter), width))

		rule := lipgloss.NewStyle().Foreground(t.Muted)
		for _, pr := range h.Pairs {
			l, r := blank(lines, at, pr.left), blank(lines, at, pr.right)
			l.New, r.Old = 0, 0
			out = append(out, p.Half(l, gutter, half)+rule.Render("│")+p.Half(r, gutter, width-half-1))
		}
		at += len(h.Rows)
	}

	fmt.Println(strings.Join(out, "\n"))
}

func header(t theme.Theme, h hunk) paint.Header {
	head := paint.Header{Text: h.Header}
	if h.Cursor {
		head.Marker, head.Fill, head.Bar = "▸", t.SelectedBackground, t.Accent
	}

	head.Badge, head.BadgeColor = "○", t.Subtle
	if h.Badged {
		head.Badge, head.BadgeColor = "●", t.Accent
	}
	return head
}

func painted(t theme.Theme, oldSide, newSide [][]syntax.Token) []paint.Line {
	var out []paint.Line
	oldAt, newAt := 0, 0

	for _, h := range hunks {
		for _, r := range h.Rows {
			l := paint.Line{Kind: r.Kind, Old: r.Old, New: r.New}
			if r.Cursor {
				l.Fill, l.Bar = t.SelectedBackground, t.Accent
			}

			switch r.Kind {
			case paint.Removed:
				l.Tokens = nth(oldSide, oldAt)
				oldAt++
			case paint.Added:
				l.Tokens = nth(newSide, newAt)
				newAt++
			case paint.Context:
				l.Tokens = nth(newSide, newAt)
				oldAt++
				newAt++
			}
			out = append(out, l)
		}
	}
	return out
}

func blank(lines []paint.Line, base, i int) paint.Line {
	if i < 0 {
		return paint.Line{}
	}
	return lines[base+i]
}

func source(kind paint.Kind) string {
	var lines []string
	for _, h := range hunks {
		for _, r := range h.Rows {
			if r.Kind == kind || r.Kind == paint.Context {
				lines = append(lines, r.Text)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func nth(lines [][]syntax.Token, i int) []syntax.Token {
	if i >= len(lines) {
		return nil
	}
	return lines[i]
}

func widest() int {
	n := 0
	for _, h := range hunks {
		for _, r := range h.Rows {
			n = max(n, r.Old, r.New)
		}
	}
	return n
}
