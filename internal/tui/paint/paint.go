// Package paint renders one row of a diff. Every exported function is pure:
// the same line at the same width gives the same string. Folding, scroll,
// side-by-side layout, hunk grouping and review state belong to the caller.
package paint

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// defaultTabWidth is what a tab expands to when a Painter names no width. A raw
// tab is a variable number of cells and puts every column after it out of step.
const defaultTabWidth = 4

// Kind is the side of the change a line belongs to.
type Kind int

const (
	Context Kind = iota
	Added
	Removed
)

// Line is one row ready to paint. Old and New are line numbers; 0 means that
// side has none, and the column stays open so the marker beside it holds still.
type Line struct {
	Kind     Kind
	Old, New int
	Tokens   []syntax.Token

	// Fill beats the kind's tint, nil uses it. A cursor, a selection and a
	// reviewed tint are the caller's state and all have to win.
	Fill color.Color
}

// Painter paints rows from one theme.
type Painter struct {
	Theme    theme.Theme
	TabWidth int // 0 means 4
}

// Line paints one row: two numbers, the marker, and highlighted source over the
// change's tint, cell by cell to the full width and clipped rather than wrapped.
func (p Painter) Line(l Line, gutter, width int) string {
	marker, c := " ", p.Theme.Subtle
	var tint color.Color

	switch l.Kind {
	case Added:
		marker, c, tint = "+", p.Theme.Success, p.Theme.AddedBackground
	case Removed:
		marker, c, tint = "−", p.Theme.Error, p.Theme.RemovedBackground
	}
	if l.Fill != nil {
		tint = l.Fill
	}

	base := background(lipgloss.NewStyle(), tint)
	kind := base.Foreground(c)
	faint := base.Foreground(p.Theme.Subtle)

	oldNum, newNum := faint, faint
	switch l.Kind {
	case Added:
		newNum = kind
	case Removed:
		oldNum = kind
	}

	row := base.Render(" ") +
		oldNum.Render(number(l.Old, gutter)) + base.Render(" ") +
		newNum.Render(number(l.New, gutter)) + base.Render(" ") +
		kind.Render(marker) + base.Render(" ") + p.code(l.Tokens, base)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, faint)
	} else if tint != nil {
		// Only a row with a background has one to run out. A context line with
		// no fill is left short, and the pane's own padding finishes it.
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// Header is the @@ line ready to paint.
type Header struct {
	Text string

	// Marker sits in the column Line puts + and − in, so a heading's mark lines
	// up under it. "" blanks the column; anything past two cells is clipped.
	Marker string

	// Badge is a second glyph, left of the marker, for a state the heading
	// carries whether or not the cursor is on it. It takes blank indent.
	Badge string

	// BadgeColor paints the badge, and nil paints it in Accent. A ladder of
	// states needs more than one weight; a cursor is one thing at one weight.
	BadgeColor color.Color

	// TextColor paints the @@ line and the marker, nil paints both Accent. A
	// column of headings at one weight cannot say which one the reader is in.
	TextColor color.Color

	// Fill is the row's background, and nil paints none. It is the caller's
	// state the same way Line.Fill is.
	Fill color.Color
}

// HunkHeader is the @@ line, indented to the code column so it sits over the
// source it introduces. A fill runs its background out to the full width.
func (p Painter) HunkHeader(h Header, gutter, width int) string {
	base := background(lipgloss.NewStyle(), h.Fill)
	accent := base.Foreground(p.Theme.Accent)

	// The marker takes the text's colour rather than Accent. It is part of what
	// the heading says about itself, and a lit caret on a dimmed line reads odd.
	text := accent
	if h.TextColor != nil {
		text = base.Foreground(h.TextColor)
	}

	badge := accent
	if h.BadgeColor != nil {
		badge = base.Foreground(h.BadgeColor)
	}

	row := base.Render(strings.Repeat(" ", markerColumn(gutter)-markerSlot)) +
		slot(h.Badge, base, badge) + slot(h.Marker, base, text) +
		text.Render(h.Text)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, base.Foreground(p.Theme.Subtle))
	} else if h.Fill != nil {
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// slot renders one glyph in a fixed pair of columns, blank when there is none.
// A wider glyph eats the space after it rather than pushing the text along.
func slot(glyph string, base, on lipgloss.Style) string {
	if glyph == "" {
		return base.Render(strings.Repeat(" ", markerSlot))
	}
	g := lipgloss.NewStyle().MaxWidth(markerSlot).Render(glyph)
	return on.Render(g) + base.Render(strings.Repeat(" ", markerSlot-lipgloss.Width(g)))
}

// code renders one row's tokens over the row's own style. Each takes only a
// foreground, so whatever is behind the row survives all the way across.
func (p Painter) code(tokens []syntax.Token, base lipgloss.Style) string {
	tab := strings.Repeat(" ", p.tabWidth())

	var b strings.Builder
	for _, t := range tokens {
		text := strings.ReplaceAll(t.Text, "\t", tab)
		if t.Color == nil {
			b.WriteString(base.Render(text))
			continue
		}
		b.WriteString(base.Foreground(t.Color).Render(text))
	}
	return b.String()
}

func (p Painter) tabWidth() int {
	if p.TabWidth <= 0 {
		return defaultTabWidth
	}
	return p.TabWidth
}

// background applies a color the theme may not define. A nil one leaves the
// terminal's own showing, which is what keeps a transparent one transparent.
func background(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Background(c)
}
