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
	// The tree takes two fifths of the frame, between treeMin and treeMax. A
	// path is worth more columns on a wide terminal than on a narrow one, and
	// worth no more than treeMax anywhere: past that the pane is mostly the gap
	// between a name and its churn.
	//
	// It is a share of the terminal and not of the content, so it moves when the
	// window does and never under the reader.
	treeNum, treeDen = 2, 5
	treeMin, treeMax = 34, 44

	// paneChrome is the two lines and two columns a pane spends on its border.
	paneChrome = 2

	// statusRow is the line the panes do not get.
	statusRow = 1

	// metaLines is the facts under the tree: what the changeset is measured
	// against, which generation is on screen, the burn-down, and how much there
	// is.
	//
	// They sit inside the tree's pane, ruled off at its foot, so the rows
	// scrolling above them never move them. metaIndent is the tree's own gutter,
	// so a fact lines up under a filename.
	metaLines  = 5
	metaIndent = 1

	// fileGlyph heads the changed-file count. It is the folders' family, so the
	// two read as one set.
	fileGlyph = "\uf15b"

	// minPane is a pane showing anything at all: two borders and a row.
	minPane = paneChrome + 1

	// minWidth leaves the diff pane twenty columns to draw in. minHeight leaves
	// each pane its two borders and a row of content between them.
	minWidth  = treeMin + paneChrome + 20
	minHeight = statusRow + minPane

	// dot separates the facts when the status bar has to carry them.
	dot = "  ·  "
)

func (m *Model) resize(width, height int) {
	m.width, m.height = width, height
	m.help.SetWidth(width)

	// Sized first, because the facts are laid out to the width the pane reports
	// and the pane decides for itself whether it has room for them.
	m.treePane = m.treePane.Size(m.treeWidth(), m.bodyHeight())
	m.treePane = m.treePane.Note(m.meta())
	m.diffPane = m.diffPane.Size(m.diffWidth(), m.bodyHeight())

	m.tree.SetSize(m.treePane.InnerWidth(), m.treePane.ContentHeight())
	m.diff.SetSize(m.diffPane.InnerWidth(), m.diffPane.InnerHeight())

	// The box is placed over the whole frame rather than inside a pane, so it
	// takes the frame's size and not a pane's.
	m.compose.SetSize(m.width, m.height)
}

func (m Model) bodyHeight() int { return max(m.height-statusRow, 0) }
func (m Model) diffWidth() int  { return max(m.width-m.treeWidth(), 0) }

// treeWidth is the tree's share of this frame, clamped at both ends.
func (m Model) treeWidth() int {
	return min(max(m.width*treeNum/treeDen, treeMin), treeMax)
}

// metaShown is whether the pane found room for the facts. It did not on a
// frame that could not spare them, and the status bar carries them instead.
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

// typingAt is where the terminal's cursor goes: into whichever box is up, and
// nowhere at all while neither is, which is most of the time.
func (m Model) typingAt() *tea.Cursor {
	if m.compose.Active() {
		return m.compose.TypingAt()
	}
	if m.width < minWidth || m.height < minHeight {
		return nil
	}

	// Past the tree's pane and the diff pane's own border, which is where the
	// pane's content starts in the frame.
	c := m.diff.TypingAt()
	if c == nil {
		return nil
	}
	c.X += m.treeWidth() + 1
	c.Y++
	return c
}

// content is the frame with whichever box is up drawn over it, the too-small
// frame included: a box owning the keys off screen is a reader with nowhere to go.
func (m Model) content() string {
	frame := m.tooSmall()
	if m.width >= minWidth && m.height >= minHeight {
		frame = strings.Join([]string{m.body(), m.status()}, "\n")
	}

	// One or the other. The composer has the keys while it is up, and the key
	// that opens the help is a keystroke in the body.
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

// body is the two framed panes, joined flush. Their borders meet, so the seam
// between them is one line rather than a rule with a gap either side.
func (m Model) body() string {
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)

	// The note is set again here, not just at resize. Its height is fixed at
	// metaLines, which is what resize needed it for, but its text is derived
	// from the changeset and has to be built on the frame that shows it.
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

// meta is the facts at the foot of the tree's pane, read as a key and a value:
// the label against the left edge and the answer against the right.
//
// Nothing here takes a key, and a box of its own would read as a third pane to
// move into.
func (m Model) meta() string {
	width := max(m.treePane.InnerWidth()-metaIndent*2, 0)
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	rows := make([]string, 0, metaLines)
	for _, f := range m.facts() {
		rows = append(rows, spread(f.label, f.value, width, subtle))
	}

	// The pane pads each row out to its own width, which is the gutter on the
	// right; this is the one on the left.
	indent := strings.Repeat(" ", metaIndent)
	for i, r := range rows {
		rows[i] = indent + comp.Clip(r, width, subtle)
	}
	return strings.Join(rows, "\n")
}

// fact is one row of that list. Both sides come back styled: the label is
// chrome and the value is the part worth looking at.
type fact struct {
	label string
	value string
}

// facts are the base, the generation on screen, the burn-down and how much
// there is. Three carry a colour of their own; the rest are chrome.
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

// baseLabel is the ref, and quieter beside it the tag a base nobody asked for
// wears. The tag goes second, so the clip a narrow pane takes eats it first.
func (m Model) baseLabel() string {
	label := lipgloss.NewStyle().Foreground(m.theme.Muted).Render(m.base.Name())
	if m.base.Fallback == "" {
		return label
	}

	// Bracketed rather than dotted: the bar carrying the facts on one line
	// separates them by a dot, and the sha would read as the tag's value.
	return label + lipgloss.NewStyle().Foreground(m.theme.Subtle).
		Render(" ("+comp.Safe(m.base.Fallback)+")")
}

// settled is the comments answered over all of them, and the only thing on
// screen saying one exists before the reader scrolls into a card.
func (m Model) settled() string {
	total := len(m.comments)
	done := total - len(m.unresolved())

	// Anything outstanding is gold. Nothing to answer is not nothing answered.
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

// overlay is the full keymap, in a box over the frame rather than in place of
// it. The reader asked what the keys are, not to be taken off the page.
func (m Model) overlay(frame string) string {
	full := m.help
	full.ShowAll = true

	return comp.Over(frame, comp.Modal(m.theme, "Keys", full.View(m), m.width, m.height), m.width, m.height)
}

// status is what the pane holding the keys can do, against the left edge where
// the eye starts.
//
// The facts join it only when their own box did not fit. Both have to be on
// screen somewhere, and the keys drop from the tail until the two share the
// line.
//
// A notice takes that right edge from the facts for the one press it lasts. It
// answers the key just pressed, which outranks a total the next frame shows
// again.
func (m Model) status() string {
	subtle := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	// The room the right side gets, measured against the way out so the bar
	// shrinks around it rather than clipping it.
	room := max(m.width-lipgloss.Width(m.help.ShortHelpView(m.wayOut()))-2, 0)

	right := m.said()
	if right == "" && !m.metaShown() {
		right = m.factLine(room)
	}
	if right == "" {
		return m.pad(m.bar(m.width), m.width)
	}

	// A refusal is a sentence rather than a label, so it is not cut to leave the
	// keys their room. The keys give way instead, being one press from the help.
	if !m.note.bad {
		// A total or an answer to the key just pressed misstates itself when it
		// is cut, so the keys are measured against what the right side left.
		right = comp.Clip(right, room, subtle)
	}
	left := m.bar(max(m.width-lipgloss.Width(right)-2, 0))

	// Every notice reads from the right, so there is one place to look for what
	// just happened. A line with no room for both is the sentence's.
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return m.pad(right, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// said is the notice as it draws, in the theme's error colour when the reload
// failed and in the facts' own subtle otherwise.
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

// factLine is the facts on one line, for the bar that has to carry them. It
// drops from the tail the way the keys do: a cut label states nothing.
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

// bar is the status line, with the pane's own keys dropped off the tail until
// it fits.
//
// ShortHelpView cuts the line from the right, which would take the way out with
// it and leave a bar that names three keys and no way to see the rest. Dropping
// a whole key is what the overlay does with a hint too: half a key teaches
// nothing.
func (m Model) bar(width int) string {
	// Measured against a help with no width of its own. One that has a width
	// truncates before returning, so every candidate comes back already cut and
	// the first one measures as fitting.
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
