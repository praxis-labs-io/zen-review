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
	return []key.Binding{comp.Pair(k.Down, k.Up, "j/k", "move")}
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

// Model is the diff pane. It holds no review state: the root owns which hunk the
// ring is on, and reads it back off the pane's one cursor, a row of the file.
type Model struct {
	Keys KeyMap

	theme   theme.Theme
	painter paint.Painter
	syntax  syntax.Syntax

	file *review.File
	rows []row

	// cursor is the row the reader is on, -1 until the root names a hunk. headAt
	// is where each hunk starts, gutter the width its line numbers took.
	cursor int
	headAt []int
	gutter int

	offset int

	width  int
	height int
}

// rowKind is what a row of the pane draws as. The three draw differently and
// each keeps the inputs its own drawing takes.
type rowKind int

const (
	codeRow rowKind = iota
	headRow
	noteRow
)

// row is one line of the pane: what it says, and what it takes to say it again.
// A cursor moving over a row repaints that row rather than the whole file.
type row struct {
	text string

	kind rowKind
	line paint.Line
	note string

	// hunk is the one this row belongs to, and -1 for a row outside every one.
	// The blank between two hunks goes to the one above it, so this never falls.
	hunk int
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
		cursor:  -1,
	}
}

// SetFile puts a file in the pane and takes the reader back to the top of it.
//
// A nil file empties the pane, which is what a changeset with nothing in it
// looks like. The cursor comes with the file, from Select: the root decides
// which hunk it lands on and this pane never guesses.
func (m *Model) SetFile(f *review.File) {
	m.file, m.offset = f, 0
	m.layout()
	m.repaintAll()
}

// Cursor is the row the reader is on, and -1 when the pane has none. It is what
// Restore takes back.
func (m Model) Cursor() int { return m.cursor }

// Hunk is the hunk the cursor is in, named the way review names one. It is false
// for a pane with no cursor and for a file with no hunks.
func (m Model) Hunk() (store.Side, int, bool) {
	if m.cursor < 0 || m.hunkAt(m.cursor) < 0 {
		return "", 0, false
	}
	side, line := m.file.Hunks[m.hunkAt(m.cursor)].Name()
	return side, line, true
}

// Restore puts the cursor and the window back where they were, for a reload that
// changed nothing after landing has taken the reader to the hunk's heading.
func (m *Model) Restore(cursor, offset int) {
	m.point(cursor)
	m.offset = max(0, min(offset, m.maxOffset()))
}

// Select puts the cursor on a hunk's heading and scrolls it to the top row. A
// hunk already on screen whole is left where it is.
func (m *Model) Select(side store.Side, line int) {
	if m.file == nil || len(m.headAt) != len(m.file.Hunks) {
		return
	}
	for i, h := range m.file.Hunks {
		if s, l := h.Name(); s == side && l == line {
			m.point(m.headAt[i])
			m.scrollToCursor()
			return
		}
	}
}

// point puts the cursor on a row and repaints what moved: the two rows, and the
// heading of the hunk each is in. -1 takes the cursor off the pane.
func (m *Model) point(i int) {
	if i < -1 || i >= len(m.rows) {
		return
	}

	was := m.cursor
	m.cursor = i
	m.repaint(was, i, m.headOf(was), m.headOf(i))
}

// moveTo is point with the row clamped to the file and the window brought back
// onto it, which is what a movement key wants and Restore does not.
func (m *Model) moveTo(i int) {
	if len(m.rows) == 0 {
		return
	}
	m.point(max(0, min(i, len(m.rows)-1)))
	m.reveal()
}

// page moves the window half a screen and takes the cursor with it. One left
// behind is paged off the pane, and the next j hauls the window back to it.
func (m *Model) page(by int) {
	m.offset += by
	m.clampOffset()
	m.moveTo(m.cursor + by)
}

// reveal brings the window back onto the cursor by as little as it takes. The
// reader is already looking at the row; the window is what fell behind.
func (m *Model) reveal() {
	if m.height <= 0 || m.cursor < 0 {
		return
	}

	m.offset = min(m.offset, m.cursor)
	m.offset = max(m.offset, m.cursor-m.height+1)
	m.clampOffset()
}

// repaint redraws rows in place, skipping any the pane does not hold and a pane
// with no width to draw into.
func (m *Model) repaint(at ...int) {
	if m.width <= 0 {
		return
	}
	for _, i := range at {
		if i >= 0 && i < len(m.rows) {
			m.rows[i].text = m.draw(i)
		}
	}
}

// draw renders one row and finishes it to the pane's width in the style it was
// drawn in, so a filled row's background runs all the way across.
func (m Model) draw(i int) string {
	text, style := m.render(i)
	if gap := m.width - lipgloss.Width(text); gap > 0 {
		text += style.Render(strings.Repeat(" ", gap))
	}
	return text
}

// render is one row painted, and the style to finish it in. The cursor's fill
// beats the kind's tint and the heading's own, which is what paint's Fill is for.
func (m Model) render(i int) (string, lipgloss.Style) {
	r := m.rows[i]

	var fill color.Color
	if i == m.cursor {
		fill = m.theme.SelectedBackground
	}

	switch r.kind {
	case headRow:
		return m.painter.HunkHeader(m.header(r.hunk, fill), m.gutter, m.width), lipgloss.NewStyle()
	case codeRow:
		l := r.line
		l.Fill = fill
		return m.painter.Line(l, m.gutter, m.width), lipgloss.NewStyle()
	}

	// A note is the pane's own line about the file rather than a line of it, so
	// the painter has nothing to say about how it is filled.
	style := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	if fill != nil {
		style = style.Background(fill)
	}
	return comp.Clip(style.Render(r.note), m.width, style), style
}

// header is one hunk's @@ line. The caret says which hunk a mark would take, and
// runs the length of the hunk; the fill is only ever the row the reader is on.
func (m Model) header(i int, fill color.Color) paint.Header {
	h := m.file.Hunks[i]

	head := paint.Header{Text: comp.Safe(h.Diff.Header), Fill: fill}
	head.Badge, head.BadgeColor = m.badge(h.State)
	if i == m.hunkAt(m.cursor) {
		head.Marker = cursorGlyph
	}
	return head
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

// hunkAt is the hunk a row belongs to, and -1 for a row outside every one.
func (m Model) hunkAt(i int) int {
	if i < 0 || i >= len(m.rows) {
		return -1
	}
	return m.rows[i].hunk
}

// headOf is the row holding the @@ line of the hunk a row belongs to, and -1
// for a row outside every hunk.
func (m Model) headOf(i int) int {
	if h := m.hunkAt(i); h >= 0 {
		return m.headAt[h]
	}
	return -1
}

// scrollToCursor opens the window on the heading of the hunk the cursor is in.
// The size arrives after the model, so the first resize is what scrolls there.
func (m *Model) scrollToCursor() {
	at := m.headOf(m.cursor)
	if m.height <= 0 || at < 0 || m.fits(at) {
		return
	}
	m.offset = min(at, m.maxOffset())
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
	m.repaintAll()
	m.clampOffset()

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
		m.moveTo(m.cursor + 1)
	case key.Matches(press, m.Keys.Up):
		m.moveTo(m.cursor - 1)
	case key.Matches(press, m.Keys.HalfDown):
		m.page(m.half())
	case key.Matches(press, m.Keys.HalfUp):
		m.page(-m.half())
	case key.Matches(press, m.Keys.Top):
		m.moveTo(0)
	case key.Matches(press, m.Keys.Bottom):
		m.moveTo(len(m.rows) - 1)
	}
	return m, nil
}

func (m Model) View() string {
	out := make([]string, 0, m.height)
	for i := m.offset; i < len(m.rows) && len(out) < m.height; i++ {
		out = append(out, m.rows[i].text)
	}

	blank := strings.Repeat(" ", max(0, m.width))
	for len(out) < m.height {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// layout rebuilds the rows from the file. It takes no width, so the root can put
// the cursor on a hunk before the terminal has said how big the pane is.
func (m *Model) layout() {
	m.rows, m.headAt, m.cursor = nil, nil, -1
	if m.file == nil {
		return
	}

	tokens := m.tokens(*m.file)
	m.gutter = paint.Gutter(widest(*m.file))

	seen := 0
	for i, h := range m.file.Hunks {
		if i > 0 {
			m.rows = append(m.rows, row{kind: noteRow, hunk: i - 1})
		}

		m.headAt = append(m.headAt, len(m.rows))
		m.rows = append(m.rows, row{kind: headRow, hunk: i})

		for _, l := range h.Diff.Lines {
			m.rows = append(m.rows, row{kind: codeRow, hunk: i, line: paint.Line{
				Kind:   kindOf(l.Kind),
				Old:    l.Old,
				New:    l.New,
				Tokens: tokens[seen],
			}})
			seen++

			// It hangs under the line it was written about. Without it a file that
			// lost its trailing newline shows two rows of the same text.
			if l.NoEOL {
				m.rows = append(m.rows, row{kind: noteRow, hunk: i, note: `\ No newline at end of file`})
			}
		}
	}

	if len(m.file.Hunks) == 0 {
		m.rows = append(m.rows, row{kind: noteRow, hunk: -1, note: emptyReason(*m.file)})
	}
}

// repaintAll draws every row at the pane's width, which is all a resize needs.
// It is not in View, because tokenising writes a cache and View has to be pure.
func (m *Model) repaintAll() {
	if m.width <= 0 {
		return
	}
	for i := range m.rows {
		m.rows[i].text = m.draw(i)
	}
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
