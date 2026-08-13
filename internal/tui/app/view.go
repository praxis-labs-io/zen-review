package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-review/zen-review/internal/tui/comp"
)

const (
	// treeWidth is the tree pane, fixed. A path is not worth a column of the
	// diff, and a pane that resizes under the reader is worse than a narrow one.
	treeWidth = 32

	// rule is the gap, the vertical line and the gap after it.
	rule = 3

	// titleRow and statusRow are the lines the body does not get.
	titleRow  = 1
	statusRow = 1

	// minWidth leaves the diff pane something to draw in. minHeight leaves the
	// body one line between the title and the status bar.
	minWidth  = treeWidth + rule + 20
	minHeight = titleRow + statusRow + 1

	// dot separates the facts on the status bar.
	dot = "  ·  "
)

func (m *Model) resize(width, height int) {
	m.width, m.height = width, height
	m.help.SetWidth(width)

	body := m.bodyHeight()
	m.tree.SetSize(treeWidth, body)
	m.diff.SetSize(m.diffWidth(), body)
}

func (m Model) bodyHeight() int { return max(m.height-titleRow-statusRow, 0) }
func (m Model) diffWidth() int  { return max(m.width-treeWidth-rule, 0) }

func (m Model) View() tea.View {
	v := tea.NewView(m.content())
	v.AltScreen = true
	v.BackgroundColor = m.theme.Background
	return v
}

func (m Model) content() string {
	if m.width < minWidth || m.height < minHeight {
		return m.tooSmall()
	}
	if m.showing {
		return strings.Join([]string{m.titles(), m.overlay(), m.status()}, "\n")
	}
	return strings.Join([]string{m.titles(), m.body(), m.status()}, "\n")
}

// titles name the two panes: the left one always, the right one with the file
// it is showing.
//
// The title is styled and then padded, not the other way round, so the name
// carries the colour and the empty columns beside it do not.
func (m Model) titles() string {
	return m.pad(m.column("CHANGES", m.focus == focusTree), treeWidth) +
		m.separator(false) +
		m.pad(m.column(comp.Safe(m.diff.Path()), m.focus == focusDiff), m.diffWidth())
}

func (m Model) body() string {
	left := strings.Split(m.tree.View(), "\n")
	right := strings.Split(m.diff.View(), "\n")

	rows := make([]string, 0, len(left))
	for i := range left {
		rows = append(rows, left[i]+m.separator(true)+right[i])
	}
	return strings.Join(rows, "\n")
}

// overlay is the full keymap, drawn over the body it replaces.
func (m Model) overlay() string {
	full := m.help
	full.ShowAll = true

	lines := strings.Split(full.View(m), "\n")
	for i, line := range lines {
		lines[i] = m.pad(line, m.width)
	}

	blank := strings.Repeat(" ", m.width)
	for len(lines) < m.bodyHeight() {
		lines = append(lines, blank)
	}
	return strings.Join(lines[:m.bodyHeight()], "\n")
}

// status is the base, the generation and the burn-down, with the way out on
// the right.
//
// The hint is measured first and the facts are clipped into what is left, not
// the other way round. Dropping it is dropping the only thing on screen saying
// that ? exists, and the reader who needed it is the one on the narrow
// terminal.
func (m Model) status() string {
	facts := []string{
		fmt.Sprintf("%s (%s)", m.base.Ref, short(m.base.SHA)),
		fmt.Sprintf("generation %d", m.gen.Seq),
		fmt.Sprintf("%d / %d reviewed", m.changeset.Reviewed, m.changeset.Items),
	}

	faint := lipgloss.NewStyle().Foreground(m.theme.Faint)
	right := m.help.ShortHelpView(m.ShortHelp())

	left := comp.Clip(faint.Render(strings.Join(facts, dot)),
		max(m.width-lipgloss.Width(right)-1, 0), faint)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return m.pad(right, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// separator is the line between the panes. It runs the height of the body and
// stops at the status bar, which spans both.
func (m Model) separator(body bool) string {
	bar := " "
	if body {
		bar = "│"
	}
	return " " + lipgloss.NewStyle().Foreground(m.theme.BorderFaintOrSecondary()).Render(bar) + " "
}

// column titles a pane, quiet when the keys are pointed elsewhere.
func (m Model) column(text string, focused bool) string {
	style := lipgloss.NewStyle().Foreground(m.theme.Faint)
	if focused {
		style = lipgloss.NewStyle().Foreground(m.theme.Secondary).Bold(true)
	}
	return style.Render(text)
}

// tooSmall fills the screen the same way every other path does, so the frame
// is width by height whatever is drawn on it.
func (m Model) tooSmall() string {
	text := fmt.Sprintf("the terminal is %dx%d, and this needs %dx%d",
		m.width, m.height, minWidth, minHeight)

	lines := []string{m.pad(lipgloss.NewStyle().Foreground(m.theme.Faint).Render(text), m.width)}
	blank := strings.Repeat(" ", max(m.width, 0))
	for len(lines) < m.height {
		lines = append(lines, blank)
	}
	return strings.Join(lines[:max(m.height, 1)], "\n")
}

// pad fits text to exactly width, clipping what does not fit so a pane never
// pushes its neighbour sideways.
func (m Model) pad(text string, width int) string {
	text = comp.Clip(text, width, lipgloss.NewStyle().Foreground(m.theme.Faint))
	if gap := width - lipgloss.Width(text); gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	return text
}

func short(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}
