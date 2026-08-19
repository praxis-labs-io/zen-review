// Package diffpane is the right pane: the selected file's diff.
//
// It draws through tui/paint, which every diff row in the tool goes through, so
// nothing here grows a second line painter.
package diffpane

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
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

	// Place is the z that waits, and the three that answer it. Vim spells them
	// zz, zt and zb, and z on its own does nothing.
	Place    key.Binding
	Centre   key.Binding
	ToTop    key.Binding
	ToBottom key.Binding

	// Fold and Jump act on the comment card the cursor is on, and do nothing
	// anywhere else. They are the tree's two keys doing the tree's two jobs.
	Fold key.Binding
	Jump key.Binding

	// Select anchors a range at the cursor, and Cancel takes it back.
	Select key.Binding
	Cancel key.Binding
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		Movement: comp.NewMovement(),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "diff half page up")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "diff half page down")),

		// One entry, and labelled by the key it is actually bound to. Spelling
		// out zz/zt/zb widens the overlay past the frame it has to fit at eighty.
		Place:    key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "place cursor: z/t/b")),
		Centre:   key.NewBinding(key.WithKeys("z")),
		ToTop:    key.NewBinding(key.WithKeys("t")),
		ToBottom: key.NewBinding(key.WithKeys("b")),

		Fold: key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "fold comment")),
		Jump: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "go to its line")),

		// Cancel carries no help of its own. The bar names it while a selection is
		// up, which is the only time it does anything.
		Select: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "select lines")),
		Cancel: key.NewBinding(key.WithKeys("esc")),
	}
}

// Cards is the keys a comment card answers to, for the column of the help the
// pane holding one owns.
func (k KeyMap) Cards() []key.Binding {
	return []key.Binding{k.Fold, k.Jump}
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

	file     *review.File
	comments []store.Comment
	rows     []row

	// cards are the comments on this file, in the order they were laid out.
	// folded is the ones the reader has toggled away from their default.
	cards  []card
	folded map[string]bool

	// replaced is the code each answered comment was written against, by id.
	// expanded is the cards showing all of it rather than the opening few lines.
	replaced map[string][]string
	expanded map[string]bool

	// gen is the generation the file is measured in. A comment anchored to an
	// older one froze there and its line numbers name code that has since moved.
	gen int64

	// cursor is the row the reader is on, -1 until the root names a hunk. headAt
	// is where each hunk starts, gutter the width its line numbers took.
	cursor int
	headAt []int
	gutter int

	// waiting is a z held for the key that says where to put the cursor. Any
	// landing clears it, so a ring key between the two does not arm the next.
	waiting bool

	// anchor is where v was pressed, held as a place rather than a row: a card's
	// height moves with the width, and every row after it renumbers.
	anchor place

	// draft is the comment being typed, laid out as the card it will become. It
	// holds every key while it is up, and nil is nobody typing.
	draft *draft

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
	cardRow
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

	// card is the comment this row is part of, indexing m.cards, and -1 on every
	// other row.
	card int

	// seq is the row's place among the ones a width change cannot move, which is
	// all of them but a card's. It is what carries the cursor over a relayout.
	seq int
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
		anchor:  place{seq: -1},
	}
}

// SetFile puts a file in the pane and takes the reader back to the top of it.
//
// A nil file empties the pane, which is what a changeset with nothing in it
// looks like. The cursor comes with the file, from Select: the root decides
// which hunk it lands on and this pane never guesses.
func (m *Model) SetFile(f *review.File, comments []store.Comment, replaced map[string][]string, at int64) {
	m.file, m.comments, m.replaced, m.gen, m.offset = f, comments, replaced, at, 0
	m.anchor = place{seq: -1}
	m.layout()
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
	m.clearSelection()
	m.point(cursor)
	m.offset = max(0, min(offset, m.maxOffset()))
}

// Select puts the cursor on a hunk's heading and scrolls it to the top row. A
// hunk already on screen whole is left where it is.
func (m *Model) Select(side store.Side, line int) {
	if m.file == nil || len(m.headAt) != len(m.file.Hunks) {
		return
	}
	m.clearSelection()

	// A file with no hunks is one stop and one row, and the ring lands on it the
	// same as any other. Its row carries no hunk, so Hunk still answers false.
	if len(m.file.Hunks) == 0 {
		m.point(0)
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

// SelectComment lands the cursor on a comment's card, with the code it answers
// still above it. Nothing happens for a comment this file does not hold.
func (m *Model) SelectComment(id string) {
	m.clearSelection()
	for i := range m.cards {
		if m.cards[i].id == id {
			m.point(m.cards[i].at)
			m.showCard(i)
			return
		}
	}
}

// Comment is the card the cursor is on, and false when it is not on one.
func (m Model) Comment() (string, bool) {
	if c := m.cardOf(m.cursor); c != nil {
		return c.id, true
	}
	return "", false
}

// showCard brings a card's last row on screen without scrolling past the line it
// answers. Topping the card would scroll that line away.
func (m *Model) showCard(i int) {
	if m.height <= 0 {
		return
	}
	c := m.cards[i]

	// top is the highest the window goes, and hangs the lowest it goes and still
	// shows the line the card is written under.
	top, hangs := c.at, c.at
	if c.anchor >= 0 {
		top, hangs = c.anchor, c.at-1
	}
	top, hangs = m.abovePin(top), m.abovePin(hangs)

	// An anchor deeper than the window would take the card off the bottom with it.
	// The scroll gives the rest of the span up rather than the card.
	m.offset = max(min(m.offset, top), min(c.end()-m.height, hangs))
	m.clampOffset()
}

// abovePin is a row the window can open on without the pinned heading covering
// it: one higher where a heading sits above, and the row itself where none does.
func (m Model) abovePin(at int) int {
	if h := m.headOf(at); h >= 0 && h < at {
		return at - 1
	}
	return at
}

// point puts the cursor on a row and repaints what moved: the two rows, and the
// heading of the hunk each is in. -1 takes the cursor off the pane.
func (m *Model) point(i int) {
	if i < -1 || i >= len(m.rows) {
		return
	}

	was := m.cursor
	m.cursor, m.waiting = i, false
	m.repaint(was, i, m.headOf(was), m.headOf(i))

	// The span and the row the cursor left, which is the run a selection just
	// grew or shrank by. A relayout arrives here with was at -1 and all of it new.
	if lo, hi, on := m.span(); on {
		if was >= 0 {
			lo, hi = min(lo, was), max(hi, was)
		}
		for at := lo; at <= hi; at++ {
			m.repaint(at)
		}
	}

	// A card's whole border changes with the cursor, so both cards it moved
	// between are redrawn rather than the one row it left and the one it took.
	m.repaintCard(was)
	m.repaintCard(i)
}

// repaintCard redraws every row of the card a row belongs to, and nothing for a
// row outside every one.
func (m *Model) repaintCard(i int) {
	c := m.cardOf(i)
	if c == nil {
		return
	}
	for at := c.at; at < c.end(); at++ {
		m.repaint(at)
	}
}

// moveTo is point with the row clamped to the file and the window brought back
// onto it, which is what a movement key wants and Restore does not.
func (m *Model) moveTo(i int) {
	if len(m.rows) == 0 {
		return
	}
	i = max(0, min(i, len(m.rows)-1))

	// The blank between two hunks is the pane's own spacing rather than a line
	// of the file, so the cursor steps over it the way it was already going.
	if m.blank(i) {
		by := 1
		if i < m.cursor {
			by = -1
		}
		i = max(0, min(i+by, len(m.rows)-1))
	}

	// A card is one block and one stop. Walking its border and its prose a row
	// at a time would be six presses to clear one comment.
	if c := m.cardOf(i); c != nil && i != c.at {
		switch {
		case i < m.cursor:
			i = c.at
		case c.end() < len(m.rows):
			i = c.end()
		default:
			i = c.at
		}
	}

	m.point(i)
	m.reveal()
}

// blank is whether a row is the spacing between two hunks, which is the one row
// the pane draws that says nothing.
func (m Model) blank(i int) bool {
	if i < 0 || i >= len(m.rows) {
		return false
	}
	r := m.rows[i]
	return r.kind == noteRow && r.note == ""
}

// page moves the cursor half a screen and parks it mid-window, so the file runs
// past a cursor that stays put and the eye keeps one place to read from.
func (m *Model) page(by int) {
	// The ends are where it moves instead: the window stops at the first row
	// and the last, and the cursor goes on alone to the end of the file.
	m.moveTo(m.cursor + by)
	m.place(m.middle())
}

// middle is the window row a paging key parks the cursor on, and the one zz
// asks for by name.
func (m Model) middle() int { return (m.height - 1) / 2 }

// reveal brings the window back onto the cursor by as little as it takes. The
// reader is already looking at the row; the window is what fell behind.
func (m *Model) reveal() {
	if m.cursor >= 0 && m.height > 0 {
		m.offset = min(m.offset, m.cursor)
		m.offset = max(m.offset, m.cursor-m.height+1)
	}
	m.clampOffset()
	m.clearPin()
}

// clearPin keeps the cursor off the row the pinned heading covers, opening the
// window one row higher so the pin has a line of its own.
func (m *Model) clearPin() {
	// A one-row pane has nowhere to open into, and the row it would give up is
	// the cursor's. pinned stands down there instead.
	if m.height <= 1 {
		return
	}

	// Suppressing the pin instead cost the heading on every paging key: those
	// move the cursor and the window by the same amount, so it sat here always.
	if m.cursor != m.offset || m.offset <= 0 {
		return
	}
	if at := m.headOf(m.cursor); at >= 0 && at < m.offset {
		m.offset--
	}
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
	if i == m.cursor || m.inSelection(i) {
		fill = m.theme.SelectedBackground
	}

	switch r.kind {
	case headRow:
		return m.painter.HunkHeader(m.header(r.hunk, fill), m.gutter, m.width), lipgloss.NewStyle()
	case codeRow:
		l := r.line
		l.Fill = fill
		return m.painter.Line(l, m.gutter, m.width), lipgloss.NewStyle()
	case cardRow:
		// A card is one block, so a cursor anywhere in it lights the whole thing.
		// Both drawings are already the pane's width and take no padding.
		c := m.cards[r.card]
		if on := m.cardOf(m.cursor); on != nil && on.id == c.id {
			return c.lit[i-c.at], lipgloss.NewStyle()
		}
		return c.plain[i-c.at], lipgloss.NewStyle()
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

// pinned is the heading to hold on the top line, or -1 for none. It follows the
// window and not the cursor: a heading names the lines under it.
func (m Model) pinned() int {
	if m.offset >= len(m.rows) {
		return -1
	}

	// Only reachable on a pane too short for clearPin to have opened a line. The
	// cursor keeps the row: a reader who cannot see their own has lost more.
	if m.cursor == m.offset {
		return -1
	}

	// Pinning above the blank between two hunks would stack a heading directly
	// on top of the next one arriving right below it.
	if m.blank(m.offset) {
		return -1
	}

	at := m.headOf(m.offset)
	if at < 0 || at >= m.offset {
		return -1
	}
	return at
}

// place puts the cursor's row at a position in the window, 0 being the top row.
// It stops at the ends of the file the way vim does, rather than scrolling past.
func (m *Model) place(row int) {
	if m.cursor < 0 || m.height <= 0 {
		return
	}
	m.offset = m.cursor - row
	m.clampOffset()

	// zt asks for the top row, which is the pin's. The cursor takes the one
	// under it rather than being drawn over by the heading it asked to see.
	m.clearPin()
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

// SetSize gives the pane the room it draws into, the inside of the frame. The
// first sizing scrolls to the cursor; a later one only keeps it on screen.
func (m *Model) SetSize(width, height int) {
	first := m.width == 0 && m.height == 0
	was := m.placeOf(m.cursor)

	m.width, m.height = width, height

	// Width first: how many rows the box wraps into is a question about the width
	// it is asked at.
	if m.draft != nil {
		m.draft.area.SetWidth(m.draftWidth())
		m.capBox()
	}
	m.relayout(was)
	m.reveal()

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

// Placing reports that z owns the next key, before the root claims it.
func (m Model) Placing() bool { return m.waiting }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// The box takes every message while it is up, a paste included. The root
	// answers the two keys it owns and never gets here with one.
	if m.draft != nil {
		return m, m.typing(msg)
	}

	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	// A z is one press of a two-press key, and anything else after it is
	// swallowed rather than acted on, the way vim swallows it.
	if m.waiting {
		m.waiting = false
		switch {
		case key.Matches(press, m.Keys.Centre):
			m.place(m.middle())
		case key.Matches(press, m.Keys.ToTop):
			m.place(0)
		case key.Matches(press, m.Keys.ToBottom):
			m.place(m.height - 1)
		}
		return m, nil
	}
	if key.Matches(press, m.Keys.Place) {
		m.waiting = true
		return m, nil
	}

	switch {
	case key.Matches(press, m.Keys.Select):
		m.selectRange()
	case key.Matches(press, m.Keys.Cancel):
		m.clearSelection()
	case key.Matches(press, m.Keys.Fold):
		m.fold()
	case key.Matches(press, m.Keys.Jump):
		m.jump()
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

// fold takes a card down to its one row, or opens it back up. It changes the
// card's height, so the rows are rebuilt and the cursor put back on it.
func (m *Model) fold() {
	c := m.cardOf(m.cursor)
	if c == nil {
		return
	}

	if m.folded == nil {
		m.folded = make(map[string]bool)
	}
	m.folded[c.id] = !m.folded[c.id]

	m.relayout(place{comment: c.id, seq: -1})
	m.reveal()
}

// Expand shows the whole block or puts it back to the lines a card opens with,
// rebuilding the rows the way folding does. The root calls it: the key is its.
func (m *Model) Expand() {
	c := m.cardOf(m.cursor)
	if c == nil {
		return
	}

	if m.expanded == nil {
		m.expanded = make(map[string]bool)
	}
	m.expanded[c.id] = !m.expanded[c.id]

	m.relayout(place{comment: c.id, seq: -1})
	m.reveal()
}

// jump takes the cursor from a card to the first line it is about. A card the
// diff has no line for has nowhere to go, and the key says nothing on it.
func (m *Model) jump() {
	if c := m.cardOf(m.cursor); c != nil && c.anchor >= 0 {
		m.moveTo(c.anchor)
	}
}

// View pins the top row's own hunk heading there once that heading has scrolled
// above the window, covering the line it sits on.
func (m Model) View() string {
	if m.file == nil {
		return comp.Placeholder(m.theme, "nothing to review", m.width, m.height)
	}

	out := make([]string, 0, m.height)
	for i := m.offset; i < len(m.rows) && len(out) < m.height; i++ {
		out = append(out, m.rows[i].text)
	}

	if at := m.pinned(); at >= 0 && len(out) > 0 {
		out[0] = m.rows[at].text
	}

	blank := strings.Repeat(" ", max(0, m.width))
	for len(out) < m.height {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// layout rebuilds the rows from the file and the comments written against it. A
// card's height moves with the width, so relayout is what carries a cursor over.
func (m *Model) layout() {
	m.rows, m.headAt, m.cards, m.cursor = nil, nil, nil, -1
	if m.file == nil {
		return
	}

	tokens := m.tokens(*m.file)
	m.gutter = paint.Gutter(widest(*m.file))

	mine := m.mine()

	// The box lands where the card it will become lands, which is what the same
	// pass over the lines gives it.
	if m.draft != nil && m.draft.path == m.file.Diff.Path {
		at := m.draft.at

		// One being retyped takes the card's own place, generation included: a
		// comment frozen at an older one hangs at the foot and the box goes there.
		if i := index(mine, m.draft.edits); i >= 0 {
			mine[i] = at
		} else {
			at.GenerationID = m.gen
			mine = append(mine, at)
		}
	}

	placed := make([]bool, len(mine))

	// A file comment names the whole file rather than a line in it, so it heads
	// the file the way a whole-file reviewed range covers one.
	for i, c := range mine {
		if c.Scope == store.ScopeFile {
			placed[i] = true
			m.addCard(c, -1, -1)
		}
	}

	// first is the row a comment's own first line landed on, which is where enter
	// goes and how far up the ring scrolls. The card hangs under its last.
	first := make(map[string]int, len(mine))

	seen, seq := 0, 0
	add := func(r row) {
		r.card, r.seq = -1, seq
		seq++
		m.rows = append(m.rows, r)
	}

	for i, h := range m.file.Hunks {
		if i > 0 {
			add(row{kind: noteRow, hunk: i - 1})
		}

		m.headAt = append(m.headAt, len(m.rows))
		add(row{kind: headRow, hunk: i})

		for _, l := range h.Diff.Lines {
			add(row{kind: codeRow, hunk: i, line: paint.Line{
				Kind:   kindOf(l.Kind),
				Old:    l.Old,
				New:    l.New,
				Tokens: tokens[seen],
			}})
			at := len(m.rows) - 1
			seen++

			// It hangs under the line it was written about. Without it a file that
			// lost its trailing newline shows two rows of the same text.
			if l.NoEOL {
				add(row{kind: noteRow, hunk: i, note: `\ No newline at end of file`})
			}

			// A card hangs under the last line of what it answers, so that code is
			// above it and stays on screen when the ring lands on the card.
			for j, c := range mine {
				if !m.live(c) {
					continue
				}
				if _, seen := first[c.ID]; !seen && on(c, l, c.Start) {
					first[c.ID] = at
				}
				if !placed[j] && on(c, l, c.End) {
					placed[j] = true

					// A range whose first line the diff does not show anchors to its
					// last, which is the row the card is already hanging under.
					anchor, ok := first[c.ID]
					if !ok {
						anchor = at
					}
					m.addCard(c, i, anchor)
				}
			}
		}
	}

	if len(m.file.Hunks) == 0 {
		add(row{kind: noteRow, hunk: -1, note: emptyReason(*m.file)})
	}

	// What the diff had no line for still draws. An open comment can anchor at a
	// line the changeset has moved past, and dropping it loses what was asked.
	for i, c := range mine {
		if !placed[i] {
			m.addCard(c, -1, -1)
		}
	}

	m.repaintAll()
}

// mine is the comments written against the file in the pane, in the order they
// came, which is the order review sorted them in.
func (m Model) mine() []store.Comment {
	if m.file == nil {
		return nil
	}

	out := make([]store.Comment, 0, len(m.comments))
	for _, c := range m.comments {
		if m.file.Owns(c) {
			out = append(out, c)
		}
	}
	return out
}

// index is where a comment sits in a list of them, and -1 for one that is not
// there. An empty id names none of them.
func index(cs []store.Comment, id string) int {
	if id == "" {
		return -1
	}
	for i := range cs {
		if cs[i].ID == id {
			return i
		}
	}
	return -1
}

// live is whether a comment's anchor is measured in the generation on screen. A
// frozen one keeps the anchor it stopped at, naming whatever is there now.
func (m Model) live(c store.Comment) bool {
	return c.GenerationID == m.gen
}

// on is whether a diff line is the file's line n on the side a comment was
// written against. A context line sits on both sides and answers to each.
func on(c store.Comment, l diff.Line, n int) bool {
	if n == 0 {
		return false
	}
	if c.Side == store.SideBase {
		return l.Old == n
	}
	return l.New == n
}

// repaintAll draws every row at the pane's width, which is all a relayout needs.
// It is not in View, because tokenising writes a cache and View has to be pure.
func (m *Model) repaintAll() {
	if m.width <= 0 {
		return
	}
	for i := range m.rows {
		m.rows[i].text = m.draw(i)
	}
}

// place is what the cursor is on rather than which row it is, so it survives a
// relayout: a card's height moves with the width, and every row after it.
type place struct {
	comment string
	seq     int
}

// placeOf is what the row at i is, and a place naming nothing for no cursor.
func (m Model) placeOf(i int) place {
	if i < 0 || i >= len(m.rows) {
		return place{seq: -1}
	}
	if c := m.cardOf(i); c != nil {
		return place{comment: c.id, seq: -1}
	}
	return place{seq: m.rows[i].seq}
}

// rowAt is where a place landed this time round, and -1 when it is gone.
func (m Model) rowAt(p place) int {
	if p.comment != "" {
		for i := range m.cards {
			if m.cards[i].id == p.comment {
				return m.cards[i].at
			}
		}
		return -1
	}
	if p.seq < 0 {
		return -1
	}
	for i := range m.rows {
		if m.rows[i].card < 0 && m.rows[i].seq == p.seq {
			return i
		}
	}
	return -1
}

// relayout rebuilds the rows and puts the cursor back on what it was on. One
// whose row went keeps a cursor rather than none: it fell off a resize.
func (m *Model) relayout(p place) {
	had := m.cursor >= 0
	m.layout()

	at := m.rowAt(p)
	if at < 0 && had && len(m.rows) > 0 {
		at = 0
	}
	m.point(at)
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
