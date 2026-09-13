package tree

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

func (m Model) View() string {
	if len(m.rows) == 0 {
		return comp.Placeholder(m.theme, "no files changed", m.width, m.height)
	}

	blank := strings.Repeat(" ", max(0, m.width))

	lines := make([]string, 0, m.height)
	for i := m.offset; i < m.total() && len(lines) < m.height; i++ {
		row := i - topPad
		if row < 0 || row >= len(m.rows) {
			lines = append(lines, blank)
			continue
		}
		lines = append(lines, m.render(m.rows[row], row == m.cursor))
	}

	for len(lines) < m.height {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

func (m Model) render(r row, cursor bool) string {
	fill := m.theme.Background

	text := m.theme.Text
	if r.n.dir() {
		text = m.theme.Accent
	}
	if cursor {
		fill = m.theme.SelectedBackground
		if !m.focused {
			text = m.theme.Subtle
		}
	}

	base := lipgloss.NewStyle()
	if fill != nil {
		base = base.Background(fill)
	}

	nameStyle := base.Foreground(text).Bold(cursor)

	subtle := base.Foreground(m.theme.Subtle)
	glyph, glyphColor := m.glyph(r.n)

	depth := min(r.depth*indent, max(m.width-gutter*2-nameMin-2, 0))

	room := m.width - gutter*2 - depth - lipgloss.Width(glyph) - 1

	trailing := comp.Clip(m.trailing(r.n, base), max(room-nameMin-1, 0), subtle)
	if trailing != "" {
		room -= lipgloss.Width(trailing) + 1
	}

	name := comp.Clip(comp.Safe(r.n.name), max(room, 0), subtle)

	row := base.Render(strings.Repeat(" ", gutter+depth)) +
		base.Foreground(glyphColor).Render(glyph) +
		base.Render(" ") +
		nameStyle.Render(name)

	gap := m.width - lipgloss.Width(row) - lipgloss.Width(trailing) - gutter
	if gap > 0 {
		row += base.Render(strings.Repeat(" ", gap))
	}
	if trailing != "" {
		row += trailing
	}
	return comp.Clip(row+base.Render(strings.Repeat(" ", gutter)), m.width, subtle)
}

// glyph uses zen-linear's status icons for files, so a burn-down reads the way a ticket board does.
func (m Model) glyph(n *node) (string, color.Color) {
	if n.dir() {
		if n.open {
			return "\uf07c", m.theme.Accent
		}
		return "\uf07b", m.theme.Accent
	}

	switch n.file.State {
	case review.Reviewed:
		return "●", m.theme.Accent
	case review.Partial:
		return "⊙", m.theme.Warning
	default:
		return "○", m.theme.Subtle
	}
}

func (m Model) trailing(n *node, base lipgloss.Style) string {
	if n.dir() {
		return ""
	}
	if n.file.Diff.Omitted != "" {
		return base.Foreground(m.theme.Subtle).Render(n.file.Diff.Omitted)
	}
	return comp.Churn(base, m.theme, n.file.Diff.Additions, n.file.Diff.Deletions)
}
