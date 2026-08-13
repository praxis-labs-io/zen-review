package app

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

const (
	// treeWidth is the tree pane, fixed: 32 columns of rows plus the two its
	// border costs. A path is not worth a column of the diff, and a pane that
	// resizes under the reader is worse than a narrow one.
	treeWidth = 34

	// paneChrome is the two lines and two columns a pane spends on its border.
	paneChrome = 2

	// statusRow is the line the panes do not get.
	statusRow = 1

	// minWidth leaves the diff pane twenty columns to draw in. minHeight leaves
	// each pane its two borders and a row of content between them.
	minWidth  = treeWidth + paneChrome + 20
	minHeight = statusRow + paneChrome + 1

	// dot separates the facts on the status bar.
	dot = "  ·  "
)

func (m *Model) resize(width, height int) {
	m.width, m.height = width, height
	m.help.SetWidth(width)

	m.treePane = m.treePane.Size(treeWidth, m.bodyHeight())
	m.diffPane = m.diffPane.Size(m.diffWidth(), m.bodyHeight())

	m.tree.SetSize(m.treePane.InnerWidth(), m.treePane.InnerHeight())
	m.diff.SetSize(m.diffPane.InnerWidth(), m.diffPane.InnerHeight())
}

func (m Model) bodyHeight() int { return max(m.height-statusRow, 0) }
func (m Model) diffWidth() int  { return max(m.width-treeWidth, 0) }

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
		return strings.Join([]string{m.overlay(), m.status()}, "\n")
	}
	return strings.Join([]string{m.body(), m.status()}, "\n")
}

// body is the two framed panes, joined flush. Their borders meet, so the seam
// between them is one line rather than a rule with a gap either side.
func (m Model) body() string {
	tree := m.treePane.
		Index(1).
		Title(titleize(comp.Safe(m.repo))).
		Footer(files(len(m.changeset.Files)), churn(m.changeset)).
		Focus(m.focus == focusTree).
		Render(m.tree.View())

	diff := m.diffPane.
		Index(2).
		Title(comp.Safe(m.diff.Path())).
		Footer("", m.diff.Scroll().Footer()).
		Focus(m.focus == focusDiff).
		Render(m.diff.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, tree, diff)
}

// overlay is the full keymap, drawn over the panes it replaces.
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

	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	right := m.help.ShortHelpView(m.ShortHelp())

	left := comp.Clip(subtle.Render(strings.Join(facts, dot)),
		max(m.width-lipgloss.Width(right)-1, 0), subtle)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return m.pad(right, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// tooSmall fills the screen the same way every other path does, so the frame
// is width by height whatever is drawn on it.
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

// pad fits text to exactly width, clipping what does not fit so a pane never
// pushes its neighbour sideways.
func (m Model) pad(text string, width int) string {
	text = comp.Clip(text, width, lipgloss.NewStyle().Foreground(m.theme.Subtle))
	if gap := width - lipgloss.Width(text); gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	return text
}

// files and churn are the two facts in the tree's bottom border: how much there
// is to read, and how much of it there is.
func files(n int) string {
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
}

func churn(c review.Changeset) string {
	return fmt.Sprintf("+%d -%d", c.Additions, c.Deletions)
}

// titleize reads a directory name as a name: zen-review becomes Zen Review.
//
// Only the first letter of each word is touched. Lowercasing the rest would
// turn a repository called CLAUDE or zenOcto into something nobody named.
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
