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

	// metaLines is the facts under the tree, a blank line at each end of them:
	// what the changeset is measured against, which generation is on screen, the
	// burn-down, and how much there is.
	//
	// They sit inside the tree's pane, ruled off at its foot, so the rows
	// scrolling above them never move them. metaIndent is the tree's own gutter,
	// so a fact lines up under a filename.
	metaLines  = 6
	metaIndent = 1

	// fileGlyph heads the changed-file count. It is the folders' family, so the
	// two read as one set.
	fileGlyph = "\uf15b"

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

// meta is the facts at the foot of the tree's pane, read as a key and a value:
// the label against the left edge and the answer against the right.
//
// Nothing here takes a key, and a box of its own would read as a third pane to
// move into. The blank line at each end matches the list above it.
func (m Model) meta() string {
	width := max(m.treePane.InnerWidth()-metaIndent*2, 0)
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	rows := []string{""}
	for _, f := range m.facts() {
		rows = append(rows, spread(muted.Render(f.label), f.value, width, subtle))
	}
	rows = append(rows, "")

	// The pane pads each row out to its own width, which is the gutter on the
	// right; this is the one on the left.
	indent := strings.Repeat(" ", metaIndent)
	for i, r := range rows {
		rows[i] = indent + comp.Clip(r, width, subtle)
	}
	return strings.Join(rows, "\n")
}

// fact is one row of that list. The label is chrome and reads at one weight
// throughout; the value is the part worth looking at.
type fact struct {
	label string
	value string
}

// facts are what the changeset is measured against, which generation is on
// screen, how far down the burn-down has come, and how much there is.
//
// The values come back styled. Only two of them carry a colour: the burn-down
// wears the state it is at, and the churn says which way it went.
func (m Model) facts() []fact {
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	return []fact{
		{m.base.Ref, subtle.Render(short(m.base.SHA))},
		{"Generation", subtle.Render(strconv.Itoa(m.gen.Seq))},
		{"Reviewed", m.burndown()},
		{"Changes", m.size()},
	}
}

// burndown reads at the state a file at the same fraction would: nothing read
// is the unreviewed grey, part of it the partial gold, all of it the reviewed
// accent. It is the same ladder as the glyphs beside the filenames, so the one
// number and the whole column agree at a glance.
//
// A changeset with nothing in it stays grey. Vacuously complete is still a
// claim that work was done.
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

// size is how many files changed and by how much.
func (m Model) size() string {
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	return subtle.Render(fileGlyph+" "+strconv.Itoa(len(m.changeset.Files))) + "  " +
		comp.Churn(lipgloss.NewStyle(), m.theme, m.changeset.Additions, m.changeset.Deletions)
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

	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	joined := make([]string, 0, len(m.facts()))
	for _, f := range m.facts() {
		joined = append(joined, muted.Render(f.label)+" "+f.value)
	}

	facts := comp.Clip(strings.Join(joined, subtle.Render(dot)),
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
