package comp

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/theme"
)

// Pane is a bordered region. It carries its name and an index in the top
// border, a counter in the bottom, colours the border by focus, and reports the
// size left over for content.
//
// The border lines are built rather than drawn by lipgloss and then edited,
// because setting styled text into a rendered border means splicing ANSI in
// place. This is zen-octo's pane, so the two tools frame a pane the same way.
type Pane struct {
	theme       theme.Theme
	title       string
	index       int
	footerLeft  string
	footerRight string
	focused     bool
	width       int
	height      int
}

// NewPane returns an unsized pane. Callers set size, content and focus as the
// model changes and render last.
func NewPane(t theme.Theme) Pane {
	return Pane{theme: t}
}

// Title sets the name in the top border.
func (p Pane) Title(s string) Pane {
	p.title = s
	return p
}

// Index sets the bracketed number leading the top border, which is the digit
// that jumps focus here. Zero leaves it off.
func (p Pane) Index(n int) Pane {
	p.index = n
	return p
}

// Footer sets the text in the bottom border: left against the left corner, the
// way the title sits in the top, and right against the right. Either may be
// empty.
//
// Both are taken as-is. A footer coloured piece by piece would be cut short by
// the first reset inside it if the pane restyled it, and the two sides do not
// read at one weight: a churn count says which way it went.
func (p Pane) Footer(left, right string) Pane {
	p.footerLeft, p.footerRight = left, right
	return p
}

// Focus lights the heading and the border, which is the only thing on the pane
// that says where the keys go.
func (p Pane) Focus(v bool) Pane {
	p.focused = v
	return p
}

// Size sets the pane's outer dimensions, borders included.
func (p Pane) Size(width, height int) Pane {
	p.width, p.height = width, height
	return p
}

// InnerWidth is the width left for content.
func (p Pane) InnerWidth() int { return max(p.width-2, 0) }

// InnerHeight is the height left for content.
func (p Pane) InnerHeight() int { return max(p.height-2, 0) }

// Render frames content. Content shorter than the pane is padded and longer is
// clipped: the pane is the authority on its own size.
//
// Padding is plain spaces, so content wanting a background out to the edge has
// to emit rows at the full inner width itself.
func (p Pane) Render(content string) string {
	if p.width < 2 || p.height < 2 {
		return ""
	}

	lines := make([]string, 0, p.height)
	lines = append(lines, p.topBorder())
	lines = append(lines, p.rows(content)...)
	return strings.Join(append(lines, p.bottomBorder()), "\n")
}

// rows is the interior, always exactly InnerHeight lines of exactly InnerWidth
// columns between the side borders.
func (p Pane) rows(content string) []string {
	lines := strings.Split(content, "\n")
	side := p.borderStyle().Render("│")

	out := make([]string, 0, p.InnerHeight())
	for i := range p.InnerHeight() {
		line := ""
		if i < len(lines) {
			line = Clip(lines[i], p.InnerWidth(), p.subtle())
		}
		gap := max(p.InnerWidth()-lipgloss.Width(line), 0)
		out = append(out, side+line+strings.Repeat(" ", gap)+side)
	}
	return out
}

// topBorder lays the index and the title flush against the left corner,
// separated by border runes rather than padded with spaces. That placement is
// lazygit's and it reads tighter than a floated label.
func (p Pane) topBorder() string {
	style := p.borderStyle()
	mid := p.InnerWidth()

	var label strings.Builder
	label.WriteString(style.Render("─"))
	if p.index > 0 {
		label.WriteString(p.indexStyle().Render("[" + strconv.Itoa(p.index) + "]"))
		label.WriteString(style.Render("─"))
	}
	if p.title != "" {
		label.WriteString(p.titleStyle().Render(p.title))
	}

	// The badge and the title are clipped together rather than one after the
	// other. A pane too narrow for the badge alone would otherwise push the
	// corner off the frame, and half a badge names no pane.
	text := Clip(label.String(), mid, p.subtle())
	fill := max(mid-lipgloss.Width(text), 0)

	return style.Render("╭") + text + style.Render(strings.Repeat("─", fill)) + style.Render("╮")
}

// bottomBorder carries the footer, a rune in from each corner. It stays as it
// is whichever pane has focus: which one that is, the heading already says
// three ways.
//
// The right side is measured first and the left is clipped into what is left
// over. The right is a total and a clipped total misstates it; the left is a
// label, and a clipped label still reads.
func (p Pane) bottomBorder() string {
	style := p.borderStyle()
	mid := p.InnerWidth()

	if p.footerLeft == "" && p.footerRight == "" {
		return style.Render("╰" + strings.Repeat("─", mid) + "╯")
	}

	right := Clip(p.footerRight, max(mid-1, 0), p.muted())

	var left string
	if p.footerLeft != "" {
		left = Clip(p.footerLeft, max(mid-lipgloss.Width(right)-2, 0), p.muted())
	}

	fill := max(mid-lipgloss.Width(left)-lipgloss.Width(right)-1, 0)
	if left != "" {
		fill = max(fill-1, 0)
		left = style.Render("─") + left
	}

	return style.Render("╰") + left + style.Render(strings.Repeat("─", fill)) +
		right + style.Render("─╯")
}

// The whole heading answers to focus, not the border alone: the border is a
// thin rule around the edge of the screen, and a reader glancing back after
// typing finds the name before they find the line under it.
//
// The three weights move together. A lit border under a dim name reads as two
// panes half-focused rather than one focused pane.

func (p Pane) borderStyle() lipgloss.Style {
	c := p.theme.BorderSubtleOrBorder()
	if p.focused {
		c = p.theme.Accent
	}
	return lipgloss.NewStyle().Foreground(c)
}

func (p Pane) titleStyle() lipgloss.Style {
	if p.focused {
		return lipgloss.NewStyle().Foreground(p.theme.Accent).Bold(true)
	}
	return p.subtle()
}

func (p Pane) indexStyle() lipgloss.Style {
	if p.focused {
		return lipgloss.NewStyle().Foreground(p.theme.Accent)
	}
	return p.muted()
}

func (p Pane) muted() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.theme.Muted)
}

func (p Pane) subtle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.theme.Subtle)
}

// Scroll is where a pane's window sits in its content, and renders the counter
// its bottom border carries.
type Scroll struct {
	Offset int
	Height int
	Total  int
}

// Footer reports position only when there is somewhere to scroll to. A counter
// on content that already fits is noise.
func (s Scroll) Footer() string {
	if s.Total <= s.Height {
		return ""
	}
	return strconv.Itoa(min(s.Offset+s.Height, s.Total)) + "/" + strconv.Itoa(s.Total)
}
