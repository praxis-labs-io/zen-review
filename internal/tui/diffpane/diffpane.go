// Package diffpane is the right pane: the selected file's diff.
//
// It draws through zen-kit's painter, so this tool and zen-octo render a diff
// the same way and neither grows a second line painter.
package diffpane

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/paint"
	"github.com/zen-kit/zen-kit/syntax"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// KeyMap is what the pane answers to.
//
// The half-page keys are the pane's rather than the shared movement's, because
// they page the diff from either pane. The reader walking the tree is reading
// the diff beside it, and a key that paged the tree under the cursor would take
// them somewhere they did not ask to go.
type KeyMap struct {
	comp.Movement

	HalfUp   key.Binding
	HalfDown key.Binding
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		Movement: comp.NewMovement(),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "diff half page up")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "diff half page down")),
	}
}

// Scrolling is the keys that page the diff, which answer from either pane.
func (k KeyMap) Scrolling() []key.Binding {
	return []key.Binding{k.HalfDown, k.HalfUp}
}

// Hints is what the status bar says this pane can do while it holds the keys.
func (k KeyMap) Hints() []key.Binding {
	return []key.Binding{comp.Pair(k.Down, k.Up, "j/k", "scroll")}
}

// Paging is the half-page keys as the status bar names them, which is once.
//
// The bindings say "diff" and the bar's label does not: the bar has one entry
// for the pair and no room for the rest, while the overlay lists them in the
// column of whichever pane holds the keys, where an unqualified "half page down"
// would read as one more way to move the tree.
func (k KeyMap) Paging() key.Binding {
	return comp.Pair(k.HalfDown, k.HalfUp, "ctrl+d/u", "page")
}

// cursorGlyph marks the hunk the ring is on. It is the tree's Nerd Font family,
// so the two panes read as one set, and it is one cell wide: a two-cell glyph
// would eat the space after it and put the heading's text out of step with the
// code under it.
const cursorGlyph = ""

// The rings say how much of the hunk under the heading has been read. They are
// the tree's own, so one glyph reads the same in both panes.
const (
	readGlyph    = "●"
	partialGlyph = "⊙"
	unreadGlyph  = "○"
)

// Model is the diff pane. It renders the file it was given and holds no review
// state of its own.
//
// The cursor is not review state either. The root owns which hunk the ring is
// on, because the ring crosses files and this pane holds one; what is here is
// the name the root last handed down, so a row can be drawn as the one it is on.
type Model struct {
	Keys KeyMap

	theme   theme.Theme
	painter paint.Painter
	syntax  syntax.Syntax

	file *review.File
	rows []string

	// cur is the hunk the ring is on, by the side and line review names it
	// under, and headAt is the row each of this file's hunks starts on. gutter
	// is the width the file's line numbers were laid out at, kept so a heading
	// can be repainted without laying the file out again.
	cur    hunkName
	headAt []int
	gutter int

	offset int

	width  int
	height int
}

// hunkName is a hunk's identity: the side and line review.Hunk.Name gives.
//
// An index would name whatever is third in the file after an agent inserts a
// hunk above it, which is the whole reason review names them this way.
type hunkName struct {
	side store.Side
	line int
}

func nameOf(h review.Hunk) hunkName {
	side, line := h.Name()
	return hunkName{side: side, line: line}
}

// New is an empty pane. A changeset with no files leaves it that way.
//
// The theme names the Chroma style. A name Chroma does not know still yields a
// working colorizer, so a palette naming one degrades to different colours
// rather than to no diff, and there is nothing here worth failing to open over.
func New(t theme.Theme) Model {
	s, _ := syntax.New(t.Syntax)

	return Model{
		Keys:    NewKeyMap(),
		theme:   t,
		painter: paint.Painter{Theme: t},
		syntax:  s,
	}
}

// SetFile puts a file in the pane and takes the reader back to the top of it.
//
// A nil file empties the pane, which is what a changeset with nothing in it
// looks like. The cursor comes with the file, from Select: the root decides
// which hunk it lands on and this pane never guesses.
func (m *Model) SetFile(f *review.File) {
	m.file, m.offset, m.cur = f, 0, hunkName{}
	m.relayout()
}

// Restore puts the window back where it was, clamped to the file now in the
// pane.
//
// It is for a reload that found nothing had changed. Replacing the file takes
// the reader to the top and Select then takes them to their hunk's heading,
// which throws away where they had scrolled to inside it. A reload that changed
// nothing owes them that place back.
func (m *Model) Restore(offset int) {
	m.offset = max(0, min(offset, m.maxOffset()))
}

// Select puts the cursor on a hunk of the file in the pane and scrolls to it.
//
// The heading goes on the top row. A key that lands on a block is taking the
// reader somewhere, and the shortest scroll leaves the heading wherever the
// last one happened to end. A hunk already on screen whole is left where it is,
// because moving a block the reader can already read is movement for nothing.
// The two headings it moves between are repainted where they sit. Laying the
// file out again would re-tokenise both sides of every hunk to move a one-cell
// marker, and this is a key the reader holds down.
func (m *Model) Select(side store.Side, line int) {
	was := m.cur
	m.cur = hunkName{side: side, line: line}

	m.repaint(was)
	m.repaint(m.cur)
	m.scrollToCursor()
}

// repaint redraws one hunk's heading in place, and does nothing for a hunk the
// file does not hold or a pane that has not been laid out yet.
func (m *Model) repaint(name hunkName) {
	if m.file == nil || len(m.headAt) != len(m.file.Hunks) {
		return
	}
	for i, h := range m.file.Hunks {
		if nameOf(h) == name {
			m.rows[m.headAt[i]] = m.heading(h)
		}
	}
}

// heading is one hunk's @@ line, marked and filled when it is the one the ring
// is on.
func (m Model) heading(h review.Hunk) string {
	head := paint.Header{Text: comp.Safe(h.Diff.Header)}
	head.Badge, head.BadgeColor = m.badge(h.State)
	if nameOf(h) == m.cur {
		head.Marker, head.Fill = cursorGlyph, m.theme.SelectedBackground
	}
	return m.fill(m.painter.HunkHeader(head, m.gutter, m.width))
}

// badge is the hunk's state as a glyph and the weight it reads at. A hunk a
// refresh cut half of is partial, and collapsing that into unread loses it.
func (m Model) badge(s review.State) (string, color.Color) {
	switch s {
	case review.Reviewed:
		return readGlyph, m.theme.Accent
	case review.Partial:
		return partialGlyph, m.theme.Warning
	default:
		return unreadGlyph, m.theme.Subtle
	}
}

// scrollToCursor opens the window on the cursor's heading, and does nothing
// when the pane has no size yet or the hunk is already on screen whole.
//
// The size arrives after the model is built, so a reader opening on a hunk part
// way down a file is scrolled there by the first resize rather than by Select.
func (m *Model) scrollToCursor() {
	at := m.hunkRow()
	if at < 0 || m.fits(at) {
		return
	}
	m.offset = min(at, m.maxOffset())
}

// hunkRow is the row the cursor's heading is on. It is -1 when the file in the
// pane does not hold that hunk, and when the pane has not been laid out yet.
func (m Model) hunkRow() int {
	if m.file == nil || len(m.headAt) != len(m.file.Hunks) {
		return -1
	}
	for i, h := range m.file.Hunks {
		if nameOf(h) == m.cur {
			return m.headAt[i]
		}
	}
	return -1
}

// fits is whether the hunk starting at a row is on screen whole already, its
// heading and every line of it.
func (m Model) fits(at int) bool {
	end := len(m.rows)
	for _, next := range m.headAt {
		if next > at {
			// The blank line between two hunks belongs to neither.
			end = next - 1
			break
		}
	}
	return at >= m.offset && end <= m.offset+m.height
}

// SetSize gives the pane the room it draws into, which is the inside of the
// frame and not the frame.
// The first sizing scrolls to the cursor, and every one after it only keeps the
// window in range. The size arrives after the model is built, so the reader
// opening on a hunk part way down a file is put there by that first resize; a
// later one is the terminal changing shape under someone who has scrolled
// somewhere, and yanking them back to the cursor throws that away.
func (m *Model) SetSize(width, height int) {
	first := m.width == 0 && m.height == 0

	m.width, m.height = width, height
	m.relayout()

	if first {
		m.scrollToCursor()
	}
}

// Scroll is where the window sits in the file, for the counter the frame draws.
func (m Model) Scroll() comp.Scroll {
	return comp.Scroll{Offset: m.offset, Height: m.height, Total: len(m.rows)}
}

// Path is the file in the pane, and is empty when there is none.
func (m Model) Path() string {
	if m.file == nil {
		return ""
	}
	return m.file.Diff.Path
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch {
	case key.Matches(press, m.Keys.Down):
		m.scroll(1)
	case key.Matches(press, m.Keys.Up):
		m.scroll(-1)
	case key.Matches(press, m.Keys.HalfDown):
		m.scroll(m.half())
	case key.Matches(press, m.Keys.HalfUp):
		m.scroll(-m.half())
	case key.Matches(press, m.Keys.Top):
		m.scroll(-len(m.rows))
	case key.Matches(press, m.Keys.Bottom):
		m.scroll(len(m.rows))
	}
	return m, nil
}

func (m Model) View() string {
	out := make([]string, 0, m.height)
	for i := m.offset; i < len(m.rows) && len(out) < m.height; i++ {
		out = append(out, m.rows[i])
	}

	blank := strings.Repeat(" ", max(0, m.width))
	for len(out) < m.height {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// relayout repaints the file at the current width.
//
// The rows are built here rather than in View, because tokenising is where the
// syntax cache is written and View has to stay a pure read of the model.
func (m *Model) relayout() {
	m.rows, m.headAt = nil, nil
	if m.file == nil || m.width <= 0 {
		return
	}

	tokens := m.tokens(*m.file)
	m.gutter = paint.Gutter(widest(*m.file))

	seen := 0
	for i, h := range m.file.Hunks {
		if i > 0 {
			m.add("")
		}

		m.headAt = append(m.headAt, len(m.rows))
		m.add(m.heading(h))

		for _, l := range h.Diff.Lines {
			m.add(m.painter.Line(paint.Line{
				Kind:   kindOf(l.Kind),
				Old:    l.Old,
				New:    l.New,
				Tokens: tokens[seen],
			}, m.gutter, m.width))
			seen++

			// The annotation takes no line number of its own, so it hangs under
			// the line it was written about. Without it a file that lost its
			// trailing newline shows a removal and an addition of the same text.
			if l.NoEOL {
				m.add(m.note("\\ No newline at end of file"))
			}
		}
	}

	if len(m.file.Hunks) == 0 {
		m.add(m.note(emptyReason(*m.file)))
	}
	m.clampOffset()
}

// add takes a painted line, finishing it to the pane's width.
func (m *Model) add(text string) { m.rows = append(m.rows, m.fill(text)) }

// fill finishes a painted row to the pane's width.
//
// The painter leaves a row that fits short, because a row it tinted runs its
// own background out and a context row does not. The pane is the one that knows
// how wide it is, so it is the one that finishes the line.
func (m Model) fill(text string) string {
	if gap := m.width - lipgloss.Width(text); gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	return text
}

// tokens colours the file one side at a time, and hands back one entry per diff
// line in the order the hunks hold them.
//
// A lexer carries state across lines, so highlighting line by line comes apart
// on the first multi-line string, and running the two sides together would feed
// it a file holding both halves of every change. A context line goes into both
// sides so neither reads as source with its unchanged lines missing, and takes
// its colour from the head.
//
// Each side is lexed by the name it has on that side. A rename that changes the
// extension is a different language on the base, and lexing its removals as the
// head's colours them by a grammar they were never written in.
func (m *Model) tokens(f review.File) [][]syntax.Token {
	// at is where one diff line landed: which side's body, and which line of it.
	type at struct {
		base bool
		i    int
	}

	var oldSrc, newSrc []string
	var index []at

	for _, h := range f.Hunks {
		for _, l := range h.Diff.Lines {
			text := comp.Code(l.Text)
			switch l.Kind {
			case diff.Removed:
				index = append(index, at{base: true, i: len(oldSrc)})
				oldSrc = append(oldSrc, text)
			case diff.Added:
				index = append(index, at{i: len(newSrc)})
				newSrc = append(newSrc, text)
			default:
				index = append(index, at{i: len(newSrc)})
				oldSrc = append(oldSrc, text)
				newSrc = append(newSrc, text)
			}
		}
	}

	oldTok := m.syntax.Lines(basePath(f.Diff), strings.Join(oldSrc, "\n"))
	newTok := m.syntax.Lines(f.Diff.Path, strings.Join(newSrc, "\n"))

	out := make([][]syntax.Token, len(index))
	for i, a := range index {
		src := newTok
		if a.base {
			src = oldTok
		}
		if a.i < len(src) {
			out[i] = src[a.i]
		}
	}
	return out
}

// basePath is the name the file has on the base side, which a rename or a copy
// makes a different one from its own.
func basePath(f diff.File) string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}

// note is a line about the file rather than a line of it, for a file the
// painter has nothing to draw.
func (m Model) note(text string) string {
	style := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	return comp.Clip(style.Render(text), m.width, style)
}

func (m *Model) scroll(by int) {
	m.offset += by
	m.clampOffset()
}

func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, m.maxOffset()))
}

// maxOffset is as far down as the window goes, which is the last row of the
// file on the bottom row and not one further.
func (m Model) maxOffset() int { return max(len(m.rows)-m.height, 0) }

// half is how far ctrl+u and ctrl+d go, and never zero on a pane too short to
// halve.
func (m Model) half() int {
	return max(m.height/2, 1)
}

// kindOf is a parsed line's kind as the painter names it. The two packages name
// the same three things and neither has to agree with the other on a value:
// diff.Kind is a string the JSON output prints, and paint.Kind is an iota.
func kindOf(k diff.Kind) paint.Kind {
	switch k {
	case diff.Added:
		return paint.Added
	case diff.Removed:
		return paint.Removed
	default:
		return paint.Context
	}
}

// widest is the highest line number the file reaches, which is what sizes the
// gutter. Both columns take the same width so the marker between them does not
// move from one row to the next.
//
// Start plus Lines is the line after the hunk, not its last, and a hunk ending
// at 99 sized off 100 buys a third column the file never fills. An empty range
// has Start 0 and no last line to name.
func widest(f review.File) int {
	n := 0
	for _, h := range f.Hunks {
		n = max(n, last(h.Diff.OldStart, h.Diff.OldLines), last(h.Diff.NewStart, h.Diff.NewLines))
	}
	return n
}

func last(start, lines int) int {
	if lines == 0 {
		return start
	}
	return start + lines - 1
}

// emptyReason says why a file has no hunks. The parser already worked it out
// for a binary or an oversized one; a file with neither has no changed lines.
func emptyReason(f review.File) string {
	if f.Diff.Omitted != "" {
		return f.Diff.Omitted
	}
	return "no changed lines"
}
