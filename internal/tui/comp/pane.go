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
	theme   theme.Theme
	title   string
	index   int
	footer  string
	focused bool
	width   int
	height  int
}

// NewPane returns an unsized pane. Callers set size, content and focus as the
// model changes and render last.
func NewPane(t theme.Theme) Pane {
	return Pane{theme: t}
}

// Title sets the text in the top border.
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

// Footer sets the text in the bottom border, right-aligned.
func (p Pane) Footer(s string) Pane {
	p.footer = s
	return p
}

// Focus colours the border, and is the only thing on the pane that says where
// the keys go.
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
			line = Clip(lines[i], p.InnerWidth(), p.faint())
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
		label.WriteString(lipgloss.NewStyle().Foreground(p.theme.Secondary).
			Render("[" + strconv.Itoa(p.index) + "]"))
		label.WriteString(style.Render("─"))
	}
	if p.title != "" {
		label.WriteString(lipgloss.NewStyle().Foreground(p.theme.Primary).Bold(true).Render(p.title))
	}

	// The badge and the title are clipped together rather than one after the
	// other. A pane too narrow for the badge alone would otherwise push the
	// corner off the frame, and half a badge names no pane.
	text := Clip(label.String(), mid, p.faint())
	fill := max(mid-lipgloss.Width(text), 0)

	return style.Render("╭") + text + style.Render(strings.Repeat("─", fill)) + style.Render("╮")
}

// bottomBorder carries the counter, right-aligned one rune in from the corner.
//
// The counter is chrome and reads at the border's weight rather than the
// content's, and it stays there whichever pane has focus. Which pane that is
// the border already says.
func (p Pane) bottomBorder() string {
	style := p.borderStyle()
	mid := p.InnerWidth()

	if p.footer == "" {
		return style.Render("╰" + strings.Repeat("─", mid) + "╯")
	}

	footer := Clip(p.faint().Render(p.footer), max(mid-1, 0), p.faint())
	fill := max(mid-lipgloss.Width(footer)-1, 0)

	return style.Render("╰"+strings.Repeat("─", fill)) + footer + style.Render("─╯")
}

func (p Pane) borderStyle() lipgloss.Style {
	c := p.theme.BorderSecondaryOrBorder()
	if p.focused {
		c = p.theme.Secondary
	}
	return lipgloss.NewStyle().Foreground(c)
}

func (p Pane) faint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.theme.Faint)
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
