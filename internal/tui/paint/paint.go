// Package paint renders one diff row. Every exported function is pure.
package paint

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

const defaultTabWidth = 4

type Kind int

const (
	Context Kind = iota
	Added
	Removed
)

// Line is one row to paint. A zero Old or New leaves that side's number column blank.
type Line struct {
	Kind     Kind
	Old, New int
	Tokens   []syntax.Token

	// Fill overrides the kind's tint; nil keeps it.
	Fill color.Color

	// Bar paints the leading cell; nil leaves it blank.
	Bar color.Color

	// Weight bolds the row, for a caller with no Fill to mark it.
	Weight bool
}

type Painter struct {
	Theme    theme.Theme
	TabWidth int // 0 means 4
}

// Line paints both number columns, the marker and the source over the tint, clipped to width.
// Only a row with a background is padded to width.
func (p Painter) Line(l Line, gutter, width int) string {
	marker, c, tint := p.weight(l.Kind)
	if l.Fill != nil {
		tint = l.Fill
	}

	base := background(lipgloss.NewStyle(), tint).Bold(l.Weight)
	kind := base.Foreground(c)
	faint := base.Foreground(p.Theme.Subtle)

	oldNum, newNum := faint, faint
	switch l.Kind {
	case Added:
		newNum = kind
	case Removed:
		oldNum = kind
	}

	row := lead(l.Bar, base) +
		oldNum.Render(number(l.Old, gutter)) + base.Render(" ") +
		newNum.Render(number(l.New, gutter)) + base.Render(" ") +
		kind.Render(marker) + base.Render(" ") + p.code(l.Tokens, base)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, faint)
	} else if tint != nil {
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// Body paints the marker and source over the kind's tint, without number columns.
// It ignores Fill, Bar and Weight.
func (p Painter) Body(l Line, width int) string {
	marker, c, tint := p.weight(l.Kind)
	base := background(lipgloss.NewStyle(), tint)

	row := base.Render(" ") + base.Foreground(c).Render(marker) + base.Render(" ") +
		p.code(l.Tokens, base)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, base.Foreground(p.Theme.Subtle))
	} else if tint != nil {
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// Half paints one side of a side-by-side row, numbered by whichever of Old and New is set
// and always padded to width.
// A zero Line paints a blank column.
func (p Painter) Half(l Line, gutter, width int) string {
	marker, c, tint := p.weight(l.Kind)
	if l.Fill != nil {
		tint = l.Fill
	}

	base := background(lipgloss.NewStyle(), tint).Bold(l.Weight)
	kind := base.Foreground(c)

	num := base.Foreground(p.Theme.Subtle)
	if l.Kind != Context {
		num = kind
	}

	row := lead(l.Bar, base) + num.Render(number(max(l.Old, l.New), gutter)) +
		base.Render(" ") + kind.Render(marker) + base.Render(" ") +
		p.code(l.Tokens, base)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, base.Foreground(p.Theme.Subtle))
	} else if w < width {
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

func (p Painter) weight(k Kind) (string, color.Color, color.Color) {
	switch k {
	case Added:
		return "+", p.Theme.Success, p.Theme.AddedBackground
	case Removed:
		return "−", p.Theme.Error, p.Theme.RemovedBackground
	}
	return " ", p.Theme.Subtle, nil
}

type Header struct {
	Text string

	// Marker sits in Line's +/− column. "" blanks it; past two cells is clipped.
	Marker string

	// Badge is a glyph left of Marker, for a state the heading carries with or without the cursor.
	Badge string

	// BadgeColor paints Badge; nil paints Accent.
	BadgeColor color.Color

	// TextColor paints the text and Marker; nil paints Accent.
	TextColor color.Color

	// Fill is the row background; nil paints none.
	Fill color.Color

	// Bar paints the leading cell; nil leaves it blank.
	Bar color.Color
}

// HunkHeader paints the @@ line indented to code: CodeColumn for Line rows, HalfColumn for Half.
func (p Painter) HunkHeader(h Header, code, width int) string {
	base := background(lipgloss.NewStyle(), h.Fill)
	accent := base.Foreground(p.Theme.Accent)

	text := accent
	if h.TextColor != nil {
		text = base.Foreground(h.TextColor)
	}

	badge := accent
	if h.BadgeColor != nil {
		badge = base.Foreground(h.BadgeColor)
	}

	row := lead(h.Bar, base) + base.Render(strings.Repeat(" ", max(0, code-2*markerSlot-1))) +
		slot(h.Badge, base, badge) + slot(h.Marker, base, text) +
		text.Render(h.Text)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, base.Foreground(p.Theme.Subtle))
	} else if h.Fill != nil {
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

const barGlyph = "▌"

func lead(bar color.Color, base lipgloss.Style) string {
	if bar == nil {
		return base.Render(" ")
	}
	return base.Foreground(bar).Render(barGlyph)
}

func slot(glyph string, base, on lipgloss.Style) string {
	if glyph == "" {
		return base.Render(strings.Repeat(" ", markerSlot))
	}
	g := lipgloss.NewStyle().MaxWidth(markerSlot).Render(glyph)
	return on.Render(g) + base.Render(strings.Repeat(" ", markerSlot-lipgloss.Width(g)))
}

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

func background(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Background(c)
}
