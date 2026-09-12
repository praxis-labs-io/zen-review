package comp

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Pane is a bordered region with a title in the top border, a footer in the bottom, lit by focus.
type Pane struct {
	theme       theme.Theme
	title       string
	label       string
	note        string
	index       int
	footerLeft  string
	footerRight string
	focused     bool
	width       int
	height      int
}

func NewPane(t theme.Theme) Pane {
	return Pane{theme: t}
}

func (p Pane) Title(s string) Pane {
	p.title = s
	return p
}

// Label replaces the title with s, drawn as given and not styled by focus.
func (p Pane) Label(s string) Pane {
	p.label = s
	return p
}

// Index sets the bracketed digit that jumps focus here, leading the top border. Zero omits it.
func (p Pane) Index(n int) Pane {
	p.index = n
	return p
}

// Footer sets the bottom border's left and right text, drawn as given. Either may be empty.
func (p Pane) Footer(left, right string) Pane {
	p.footerLeft, p.footerRight = left, right
	return p
}

// Note pins s under the content, ruled off and drawn as given. It is omitted when empty or
// when the pane has no room for it and a row of content.
func (p Pane) Note(s string) Pane {
	p.note = s
	return p
}

// ContentHeight is the height left for content under the note. Size by this, not InnerHeight.
func (p Pane) ContentHeight() int { return max(p.InnerHeight()-p.noteHeight(), 0) }

func (p Pane) noteHeight() int {
	if p.note == "" {
		return 0
	}
	lines := strings.Count(p.note, "\n") + 1
	if p.InnerHeight() < lines+2 {
		return 0
	}
	return lines + 1
}

// Focus lights the heading and the border.
func (p Pane) Focus(v bool) Pane {
	p.focused = v
	return p
}

// Size sets the outer dimensions, borders included.
func (p Pane) Size(width, height int) Pane {
	p.width, p.height = width, height
	return p
}

func (p Pane) InnerWidth() int { return max(p.width-2, 0) }

func (p Pane) InnerHeight() int { return max(p.height-2, 0) }

// Render frames content, padding or clipping it to size. Padding is unstyled, so a filled row
// must arrive at full width.
func (p Pane) Render(content string) string {
	if p.width < 2 || p.height < 2 {
		return ""
	}

	lines := make([]string, 0, p.height)
	lines = append(lines, p.topBorder())
	lines = append(lines, p.rows(content, p.ContentHeight())...)

	if n := p.noteHeight(); n > 0 {
		lines = append(lines, p.rule())
		lines = append(lines, p.rows(p.note, n-1)...)
	}
	return strings.Join(append(lines, p.bottomBorder()), "\n")
}

func (p Pane) rule() string {
	style := p.borderStyle()
	return style.Render("├" + strings.Repeat("─", p.InnerWidth()) + "┤")
}

func (p Pane) rows(content string, n int) []string {
	lines := strings.Split(content, "\n")
	side := p.borderStyle().Render("│")

	out := make([]string, 0, n)
	for i := range n {
		line := ""
		if i < len(lines) {
			line = Clip(lines[i], p.InnerWidth(), p.subtle())
		}
		gap := max(p.InnerWidth()-lipgloss.Width(line), 0)
		out = append(out, side+line+strings.Repeat(" ", gap)+side)
	}
	return out
}

func (p Pane) topBorder() string {
	style := p.borderStyle()
	mid := p.InnerWidth()

	var label strings.Builder
	label.WriteString(style.Render("─"))
	if p.index > 0 {
		label.WriteString(p.indexStyle().Render("[" + strconv.Itoa(p.index) + "]"))
		label.WriteString(style.Render("─"))
	}
	switch {
	case p.label != "":
		label.WriteString(p.label)
	case p.title != "":
		label.WriteString(p.titleStyle().Render(p.title))
	}

	text := Clip(label.String(), mid, p.subtle())
	fill := max(mid-lipgloss.Width(text), 0)

	return style.Render("╭") + text + style.Render(strings.Repeat("─", fill)) + style.Render("╮")
}

// bottomBorder clips the left label before the right total, since a clipped total misstates it.
func (p Pane) bottomBorder() string {
	style := p.borderStyle()
	mid := p.InnerWidth()

	if mid == 0 || (p.footerLeft == "" && p.footerRight == "") {
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

type Scroll struct {
	Offset int
	Height int
	Total  int
}

// Footer returns "end/total", or "" when the content fits.
func (s Scroll) Footer() string {
	if s.Total <= s.Height {
		return ""
	}
	return strconv.Itoa(min(s.Offset+s.Height, s.Total)) + "/" + strconv.Itoa(s.Total)
}
