package tree

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

func (m Model) View() string {
	lines := make([]string, 0, m.height)
	for i := m.offset; i < len(m.rows) && len(lines) < m.height; i++ {
		lines = append(lines, m.render(m.rows[i], i == m.cursor))
	}

	blank := strings.Repeat(" ", max(0, m.width))
	for len(lines) < m.height {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

// render is one row: the cursor bar, the glyph, the name, and the churn against
// the right edge.
func (m Model) render(r row, cursor bool) string {
	fill := m.theme.Background
	text := m.theme.Primary
	if cursor {
		fill = m.theme.SelectedBackground
		if !m.focused {
			// The bar stays on an unfocused pane so the reader can still see where
			// the tree is, and the text goes quiet so it does not read as the thing
			// the keys are pointed at.
			text = m.theme.Faint
		}
	}

	base := lipgloss.NewStyle()
	if fill != nil {
		base = base.Background(fill)
	}

	glyph, glyphColor := m.glyph(r.n)
	trailing := m.trailing(r.n)

	room := m.width - barWidth - r.depth*indent - lipgloss.Width(glyph) - 1
	if trailing != "" {
		room -= lipgloss.Width(trailing) + 1
	}

	// The name gives up the columns rather than the churn: a clipped "+12 -3"
	// misstates the file, and a clipped path still names it.
	name := comp.Clip(r.n.name, max(room, 0), base.Foreground(m.theme.Faint))

	row := m.bar(cursor, base) +
		base.Render(strings.Repeat(" ", r.depth*indent)) +
		base.Foreground(glyphColor).Render(glyph) +
		base.Render(" ") +
		base.Foreground(text).Render(name)

	gap := m.width - lipgloss.Width(row) - lipgloss.Width(trailing)
	if gap > 0 {
		row += base.Render(strings.Repeat(" ", gap))
	}
	if trailing != "" {
		row += base.Foreground(m.theme.Faint).Render(trailing)
	}
	return comp.Clip(row, m.width, base.Foreground(m.theme.Faint))
}

// bar is the mark on the row the keys are pointed at.
//
// A selected row carries a background too, but a background alone is a row the
// reader cannot find on a terminal that drops it, and a mark the render tests
// cannot see once the escapes are stripped off the frame.
func (m Model) bar(cursor bool, base lipgloss.Style) string {
	if !cursor {
		return base.Render("  ")
	}
	c := m.theme.Secondary
	if !m.focused {
		c = m.theme.Faint
	}
	return base.Foreground(c).Render("▎") + base.Render(" ")
}

// glyph is the row's mark: which way a directory is folded, or how much of a
// file has been read.
func (m Model) glyph(n *node) (string, color.Color) {
	if n.dir() {
		if n.open {
			return "▾", m.theme.Faint
		}
		return "▸", m.theme.Faint
	}

	switch n.file.State {
	case review.Reviewed:
		return "✓", m.theme.Success
	case review.Partial:
		return "~", m.theme.Warning
	default:
		return "·", m.theme.Faint
	}
}

// trailing is the cell against the right edge: what a file changed, or why
// there is nothing to count. A directory has neither.
func (m Model) trailing(n *node) string {
	if n.dir() {
		return ""
	}
	if n.file.Diff.Omitted != "" {
		return n.file.Diff.Omitted
	}
	return fmt.Sprintf("+%d -%d", n.file.Diff.Additions, n.file.Diff.Deletions)
}
