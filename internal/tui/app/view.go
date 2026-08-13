package app

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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

	// metaLines is the facts under the tree: the changeset's size, what it is
	// measured against, which generation is on screen, and the burn-down.
	//
	// They sit inside the tree's pane, ruled off at its foot, so the rows
	// scrolling above them never move them. metaIndent is the tree's own gutter,
	// so a fact lines up under a filename.
	metaLines  = 4
	metaIndent = 1

	// minPane is a pane showing anything at all: two borders and a row.
	minPane = paneChrome + 1

	// minWidth leaves the diff pane twenty columns to draw in. minHeight leaves
	// each pane its two borders and a row of content between them.
	minWidth  = treeWidth + paneChrome + 20
	minHeight = statusRow + minPane

	// dot separates the facts when the status bar has to carry them.
	dot = "  ·  "
)

func (m *Model) resize(width, height int) {
	m.width, m.height = width, height
	m.help.SetWidth(width)

	// Sized first, because the facts are laid out to the width the pane reports
	// and the pane decides for itself whether it has room for them.
	m.treePane = m.treePane.Size(treeWidth, m.bodyHeight())
	m.treePane = m.treePane.Note(m.meta())
	m.diffPane = m.diffPane.Size(m.diffWidth(), m.bodyHeight())

	m.tree.SetSize(m.treePane.InnerWidth(), m.treePane.ContentHeight())
	m.diff.SetSize(m.diffPane.InnerWidth(), m.diffPane.InnerHeight())
}

func (m Model) bodyHeight() int { return max(m.height-statusRow, 0) }
func (m Model) diffWidth() int  { return max(m.width-treeWidth, 0) }

// metaShown is whether the pane found room for the facts. It did not on a
// frame that could not spare them, and the status bar carries them instead.
func (m Model) metaShown() bool {
	return m.treePane.ContentHeight() < m.treePane.InnerHeight()
}

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
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)

	tree := m.treePane.
		Index(1).
		Title(titleize(comp.Safe(m.repo))).
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

// meta is the facts at the foot of the tree's pane. Nothing here takes a key,
// and a box of its own would read as a third pane to move into.
func (m Model) meta() string {
	width := max(m.treePane.InnerWidth()-metaIndent*2, 0)
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	var rows []string
	for _, fact := range m.facts() {
		rows = append(rows, subtle.Render(fact))
	}

	// The size goes last, under the burn-down it is the denominator of.
	rows = append(rows, spread(m.files(), comp.Churn(lipgloss.NewStyle(), m.theme,
		m.changeset.Additions, m.changeset.Deletions), width, subtle))

	// The pane pads each row out to its own width, which is the gutter on the
	// right; this is the one on the left.
	indent := strings.Repeat(" ", metaIndent)
	for i, r := range rows {
		rows[i] = indent + comp.Clip(r, width, subtle)
	}
	return strings.Join(rows, "\n")
}

// facts are what the changeset is measured against, which generation is on
// screen, and how far down the burn-down has come.
func (m Model) facts() []string {
	return []string{
		fmt.Sprintf("%s (%s)", m.base.Ref, short(m.base.SHA)),
		fmt.Sprintf("generation %d", m.gen.Seq),
		fmt.Sprintf("%d / %d reviewed", m.changeset.Reviewed, m.changeset.Items),
	}
}

// spread puts left at the start of a line and right at its end, giving the
// right whatever it asks for and clipping the left into what is left over. The
// right is a total, and a clipped total misstates it.
func spread(left, right string, width int, mark lipgloss.Style) string {
	left = comp.Clip(left, max(width-lipgloss.Width(right)-1, 0), mark)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return comp.Clip(right, width, mark)
	}
	return left + strings.Repeat(" ", gap) + right
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

// status is the way out, against the left edge where the eye starts.
//
// The facts join it only when their own box did not fit. They have to be
// somewhere, and the hint keeps the left: dropping it drops the only thing on
// screen saying that ? exists, and the reader who needed it is the one on the
// small terminal.
func (m Model) status() string {
	hint := m.help.ShortHelpView(m.ShortHelp())
	if m.metaShown() {
		return m.pad(hint, m.width)
	}

	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	facts := comp.Clip(subtle.Render(strings.Join(m.facts(), dot)),
		max(m.width-lipgloss.Width(hint)-2, 0), subtle)

	gap := m.width - lipgloss.Width(hint) - lipgloss.Width(facts)
	if gap < 0 {
		return m.pad(hint, m.width)
	}
	return hint + strings.Repeat(" ", gap) + facts
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

// files is how much there is to read, at the weight the facts under it read at.
// The churn beside it is the coloured half of the row.
func (m Model) files() string {
	n := len(m.changeset.Files)

	text := strconv.Itoa(n) + " files"
	if n == 1 {
		text = "1 file"
	}
	return lipgloss.NewStyle().Foreground(m.theme.Subtle).Render(text)
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
