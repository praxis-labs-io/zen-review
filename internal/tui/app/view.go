package app

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

const (
	treeNum, treeDen = 2, 5
	treeMin, treeMax = 34, 44

	paneChrome = 2

	statusRow = 1

	metaLines  = 5
	metaIndent = 1

	fileGlyph = "\uf15b" // the same Nerd Font family as the tree's folders

	minPane = paneChrome + 1

	minWidth  = treeMin + paneChrome + 20
	minHeight = statusRow + minPane

	dot = "  ·  "
)

func (m *Model) resize(width, height int) {
	m.width, m.height = width, height
	m.help.SetWidth(width)

	m.treePane = m.treePane.Size(m.treeWidth(), m.bodyHeight())
	m.treePane = m.treePane.Note(m.meta())
	m.diffPane = m.diffPane.Size(m.diffWidth(), m.bodyHeight())

	m.tree.SetSize(m.treePane.InnerWidth(), m.treePane.ContentHeight())
	m.diff.SetSize(m.diffPane.InnerWidth(), m.diffPane.InnerHeight())

	m.compose.SetSize(m.width, m.height)
}

func (m Model) bodyHeight() int { return max(m.height-statusRow, 0) }
func (m Model) diffWidth() int  { return max(m.width-m.treeWidth(), 0) }

func (m Model) treeWidth() int {
	return min(max(m.width*treeNum/treeDen, treeMin), treeMax)
}

func (m Model) metaShown() bool {
	return m.treePane.ContentHeight() < m.treePane.InnerHeight()
}

func (m Model) View() tea.View {
	v := tea.NewView(m.content())
	v.AltScreen = true
	v.BackgroundColor = m.theme.Background
	v.Cursor = m.typingAt()
	return v
}

func (m Model) typingAt() *tea.Cursor {
	if m.compose.Active() {
		return m.compose.TypingAt()
	}
	if m.width < minWidth || m.height < minHeight {
		return nil
	}

	c := m.diff.TypingAt()
	if c == nil {
		return nil
	}
	c.X += m.treeWidth() + 1
	c.Y++
	return c
}

func (m Model) content() string {
	frame := m.tooSmall()
	if m.width >= minWidth && m.height >= minHeight {
		frame = strings.Join([]string{m.body(), m.status()}, "\n")
	}

	if m.compose.Active() {
		return comp.Over(frame, m.compose.View(), m.width, m.height)
	}
	if m.picker.active() {
		return comp.Over(frame, m.picker.view(m.width, m.height), m.width, m.height)
	}
	if m.showing {
		return m.overlay(frame)
	}
	return frame
}

func (m Model) body() string {
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)

	tree := m.treePane.
		Index(1).
		Title(titleize(comp.Safe(m.repo))).
		Note(m.meta()).
		Focus(m.focus == focusTree).
		Render(m.tree.View())

	diff := m.diffPane.
		Index(2).
		Title(comp.Safe(m.diff.Path())).
		Footer("", muted.Render(m.diff.Scroll().Footer())).
		Focus(m.focus == focusDiff).
		Render(m.diff.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, tree, diff)
}

func (m Model) meta() string {
	width := max(m.treePane.InnerWidth()-metaIndent*2, 0)
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	rows := make([]string, 0, metaLines)
	for _, f := range m.facts() {
		rows = append(rows, spread(f.label, f.value, width, subtle))
	}

	indent := strings.Repeat(" ", metaIndent)
	for i, r := range rows {
		rows[i] = indent + comp.Clip(r, width, subtle)
	}
	return strings.Join(rows, "\n")
}

type fact struct {
	label string
	value string
}

func (m Model) facts() []fact {
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	return []fact{
		{m.baseLabel(), subtle.Render(short(m.base.SHA))},
		{muted.Render("Generation"), subtle.Render(strconv.Itoa(m.gen.Seq))},
		{muted.Render("Reviewed"), m.burndown()},
		{muted.Render("Comments"), m.settled()},
		{muted.Render("Changes"), m.size()},
	}
}

func (m Model) baseLabel() string {
	label := lipgloss.NewStyle().Foreground(m.theme.Muted).Render(m.base.Name())
	if m.base.Fallback == "" {
		return label
	}

	return label + lipgloss.NewStyle().Foreground(m.theme.Subtle).
		Render(" ("+comp.Safe(m.base.Fallback)+")")
}

func (m Model) settled() string {
	total := len(m.comments)
	done := total - len(m.unresolved())

	c := m.theme.Subtle
	switch {
	case total == 0:
	case done == total:
		c = m.theme.Accent
	default:
		c = m.theme.Warning
	}

	return lipgloss.NewStyle().Foreground(c).Render(fmt.Sprintf("%d/%d", done, total))
}

func (m Model) burndown() string {
	c := m.theme.Subtle
	switch {
	case m.changeset.Items > 0 && m.changeset.Reviewed == m.changeset.Items:
		c = m.theme.Accent
	case m.changeset.Reviewed > 0:
		c = m.theme.Warning
	}

	return lipgloss.NewStyle().Foreground(c).
		Render(fmt.Sprintf("%d/%d", m.changeset.Reviewed, m.changeset.Items))
}

func (m Model) size() string {
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	return subtle.Render(fileGlyph+" "+strconv.Itoa(len(m.changeset.Files))) + "  " +
		comp.Churn(lipgloss.NewStyle(), m.theme, m.changeset.Additions, m.changeset.Deletions)
}

func spread(left, right string, width int, mark lipgloss.Style) string {
	left = comp.Clip(left, max(width-lipgloss.Width(right)-1, 0), mark)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return comp.Clip(right, width, mark)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) overlay(frame string) string {
	full := m.help
	full.ShowAll = true

	return comp.Over(frame, comp.Modal(m.theme, "Keys", full.View(m), m.width, m.height), m.width, m.height)
}

func (m Model) status() string {
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	room := max(m.width-lipgloss.Width(m.help.ShortHelpView(m.wayOut()))-2, 0)

	right := m.said()
	if right == "" && !m.metaShown() {
		right = m.factLine(room)
	}
	if right == "" {
		return m.pad(m.bar(m.width), m.width)
	}

	if !m.note.bad {
		right = comp.Clip(right, room, subtle)
	}
	left := m.bar(max(m.width-lipgloss.Width(right)-2, 0))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return m.pad(right, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) said() string {
	if m.note.text == "" {
		return ""
	}

	c := m.theme.Subtle
	if m.note.bad {
		c = m.theme.Error
	}
	return lipgloss.NewStyle().Foreground(c).Render(comp.Safe(m.note.text))
}

func (m Model) factLine(width int) string {
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	joined := make([]string, 0, len(m.facts()))
	for _, f := range m.facts() {
		joined = append(joined, f.label+" "+f.value)
	}

	for n := len(joined); n > 0; n-- {
		if line := strings.Join(joined[:n], subtle.Render(dot)); lipgloss.Width(line) <= width {
			return line
		}
	}
	return ""
}

func (m Model) bar(width int) string {
	h := m.help
	h.SetWidth(0)

	own, out := m.paneKeys(), m.wayOut()
	if m.diff.Composing() {
		own, out = nil, m.composeKeys()
	}

	for n := len(own); n > 0; n-- {
		line := h.ShortHelpView(append(own[:n:n], out...))
		if lipgloss.Width(line) <= width {
			return line
		}
	}
	return h.ShortHelpView(out)
}

func (m Model) tooSmall() string {
	text := fmt.Sprintf("the terminal is %dx%d, and this needs %dx%d",
		m.width, m.height, minWidth, minHeight)

	lines := []string{m.pad(lipgloss.NewStyle().Foreground(m.theme.Subtle).Render(text), m.width)}
	blank := strings.Repeat(" ", max(m.width, 0))
	for len(lines) < m.height {
		lines = append(lines, blank)
	}
	return strings.Join(lines[:max(m.height, 1)], "\n")
}

func (m Model) pad(text string, width int) string {
	text = comp.Clip(text, width, lipgloss.NewStyle().Foreground(m.theme.Subtle))
	if gap := width - lipgloss.Width(text); gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	return text
}

func titleize(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || unicode.IsSpace(r)
	})
	for i, w := range words {
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func short(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}
