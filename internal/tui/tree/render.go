package tree

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

func (m Model) View() string {
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

// render is one row: the indent, the glyph, the name, and the churn against the
// right edge.
//
// The row under the cursor is a filled background and nothing else. On an
// unfocused pane the fill stays, so the reader can still see where the tree is,
// and the text goes quiet so it does not read as the thing the keys are pointed
// at.
func (m Model) render(r row, cursor bool) string {
	fill := m.theme.Background

	// A directory is the shape of the changeset rather than a thing to read, so
	// it takes the accent and the files under it stay in the reading colour.
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

	subtle := base.Foreground(m.theme.Subtle)
	glyph, glyphColor := m.glyph(r.n)

	// The indent gives way before the name does. A branching tree eleven deep
	// would otherwise spend the whole pane saying how deep it is.
	depth := min(r.depth*indent, max(m.width-gutter*2-nameMin-2, 0))

	room := m.width - gutter*2 - depth - lipgloss.Width(glyph) - 1

	// The trailing cell takes what is left over the name's share, not the other
	// way round. "renamed, contents unchanged" is 27 columns and would leave a
	// 32-column pane naming no file at all.
	trailing := comp.Clip(m.trailing(r.n, base), max(room-nameMin-1, 0), subtle)
	if trailing != "" {
		room -= lipgloss.Width(trailing) + 1
	}

	// A clipped path still names the file, so the name is what gives up the last
	// columns to the churn beside it.
	name := comp.Clip(comp.Safe(r.n.name), max(room, 0), subtle)

	row := base.Render(strings.Repeat(" ", gutter+depth)) +
		base.Foreground(glyphColor).Render(glyph) +
		base.Render(" ") +
		base.Foreground(text).Render(name)

	gap := m.width - lipgloss.Width(row) - lipgloss.Width(trailing) - gutter
	if gap > 0 {
		row += base.Render(strings.Repeat(" ", gap))
	}
	if trailing != "" {
		row += trailing
	}
	return comp.Clip(row+base.Render(strings.Repeat(" ", gutter)), m.width, subtle)
}

// glyph is the row's mark: which way a directory is folded, or how much of a
// file has been read.
//
// A file's three states are zen-linear's status icons, glyph for glyph: an
// empty ring is untouched, a ring with a centre is started, a filled one is
// done. They come from one circle family so every state is one cell at the
// same weight, and a burn-down here reads the way a ticket board does.
//
// The folders are Nerd Font, which is the one thing on screen that asks
// anything of the terminal's font. Both render one cell wide, so a font
// without them draws a replacement box rather than putting every row after it
// out of step.
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

// trailing is the cell against the right edge: what a file changed, or why
// there is nothing to count. A directory has neither.
//
// It comes back styled, so the churn can say which way the file went. base
// carries the row's background into every piece of it.
func (m Model) trailing(n *node, base lipgloss.Style) string {
	if n.dir() {
		return ""
	}
	if n.file.Diff.Omitted != "" {
		return base.Foreground(m.theme.Subtle).Render(n.file.Diff.Omitted)
	}
	return comp.Churn(base, m.theme, n.file.Diff.Additions, n.file.Diff.Deletions)
}
