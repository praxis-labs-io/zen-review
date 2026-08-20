// Command paintdemo paints a canned diff to stdout and exits. Golden files hold
// the painter still; this is where a rendering change is judged.
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

// width is a pane narrow enough that one row overflows it. A truncating change
// only shows at a width where something has to be cut.
const width = 76

// row is one line of the canned diff. Old and New are 0 on the side the line is
// not on; Cursor is the selected row, which is what Fill is for.
type row struct {
	Kind     paint.Kind
	Old, New int
	Text     string
	Cursor   bool
}

type hunk struct {
	Header string

	// Cursor marks the hunk a caller has landed on, which is what the header's
	// own Marker and Fill are for. Badged is the state beside it.
	Cursor bool
	Badged bool
	Rows   []row
}

// Two hunks, four-digit numbers in the second. One gutter serves a whole file,
// so a demo that never leaves two digits proves nothing about the alignment.
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
	},
}

func main() {
	t := theme.RosePineMoon

	s, ok := syntax.New(t.Syntax)
	if !ok {
		fmt.Fprintf(os.Stderr, "paintdemo: Chroma does not know %q, using its default style\n", t.Syntax)
	}

	p := paint.Painter{Theme: t}
	gutter := paint.Gutter(widest())
	half := (width - 1) / 2

	// Each side whole and the two apart: a lexer carries state across lines, and
	// run together it reads a file holding both halves of every change.
	oldSide := s.Lines("paint.go", source(paint.Removed))
	newSide := s.Lines("paint.go", source(paint.Added))

	out := []string{lipgloss.NewStyle().Foreground(t.Subtle).
		Render(fmt.Sprintf("theme %s, pane %d columns, gutter %d", t.Name, width, gutter))}

	lines := painted(t, oldSide, newSide)

	at := 0
	for _, h := range hunks {
		out = append(out, p.HunkHeader(header(t, h), paint.CodeColumn(gutter), width))
		for range h.Rows {
			out = append(out, p.Line(lines[at], gutter, width))
			at++
		}
	}

	// The same rows side by side, where the painter has half the columns and two
	// chances to lose a cell. A blank half faces a change with no pair.
	out = append(out, "", lipgloss.NewStyle().Foreground(t.Subtle).
		Render(fmt.Sprintf("side by side, %d columns each", half)))

	at = 0
	for _, h := range hunks {
		out = append(out, p.HunkHeader(header(t, h), paint.HalfColumn(gutter), width))

		rule := lipgloss.NewStyle().Foreground(t.Muted)
		for _, pr := range pairs(h.Rows) {
			l, r := blank(lines, at, pr.left), blank(lines, at, pr.right)
			l.New, r.Old = 0, 0
			out = append(out, p.Half(l, gutter, half)+rule.Render("│")+p.Half(r, gutter, width-half-1))
		}
		at += len(h.Rows)
	}

	fmt.Println(strings.Join(out, "\n"))
}

// header is one hunk's @@ line, the cursor and the state glyph on it.
func header(t theme.Theme, h hunk) paint.Header {
	head := paint.Header{Text: h.Header}
	if h.Cursor {
		head.Marker, head.Fill = "▸", t.SelectedBackground
	}

	head.Badge, head.BadgeColor = "○", t.Subtle
	if h.Badged {
		head.Badge, head.BadgeColor = "●", t.Accent
	}
	return head
}

// painted is every canned row as a paint.Line, in file order. A context line
// takes its colour from the new side and advances both.
func painted(t theme.Theme, oldSide, newSide [][]syntax.Token) []paint.Line {
	var out []paint.Line
	oldAt, newAt := 0, 0

	for _, h := range hunks {
		for _, r := range h.Rows {
			l := paint.Line{Kind: r.Kind, Old: r.Old, New: r.New}
			if r.Cursor {
				l.Fill = t.SelectedBackground
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

// pair is the rows one side-by-side row draws, by index into a hunk's own, and
// -1 for a column with no line.
type pair struct{ left, right int }

// pairs puts a run of removals against the additions after it. The pane has its
// own copy of this; here it only has to arrange the canned rows.
func pairs(rows []row) []pair {
	var out []pair
	var rem, add []int

	flush := func() {
		for i := range max(len(rem), len(add)) {
			p := pair{left: -1, right: -1}
			if i < len(rem) {
				p.left = rem[i]
			}
			if i < len(add) {
				p.right = add[i]
			}
			out = append(out, p)
		}
		rem, add = nil, nil
	}

	for i, r := range rows {
		switch r.Kind {
		case paint.Removed:
			rem = append(rem, i)
		case paint.Added:
			add = append(add, i)
		default:
			flush()
			out = append(out, pair{left: i, right: i})
		}
	}
	flush()

	return out
}

// blank is the line at an index, and a zero one for a column with none.
func blank(lines []paint.Line, base, i int) paint.Line {
	if i < 0 {
		return paint.Line{}
	}
	return lines[base+i]
}

// source is one side of the diff as a file, context lines included. A side
// missing its unchanged lines does not read as source to a lexer.
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
