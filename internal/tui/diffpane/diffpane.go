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

type KeyMap struct {
	comp.Movement

	HalfUp   key.Binding
	HalfDown key.Binding

	// Place arms Centre, ToTop and ToBottom for the next key press.
	Place    key.Binding
	Centre   key.Binding
	ToTop    key.Binding
	ToBottom key.Binding

	// Fold and Jump act only on the comment card under the cursor.
	Fold key.Binding
	Jump key.Binding

	Select key.Binding
	Cancel key.Binding
}

func NewKeyMap() KeyMap {
	return KeyMap{
		Movement: comp.NewMovement(),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "diff half page up")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "diff half page down")),

		Place:    key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "place cursor: z/t/b")),
		Centre:   key.NewBinding(key.WithKeys("z")),
		ToTop:    key.NewBinding(key.WithKeys("t")),
		ToBottom: key.NewBinding(key.WithKeys("b")),

		Fold: key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "fold comment")),
		Jump: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "go to its line")),

		Select: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "select lines")),
		Cancel: key.NewBinding(key.WithKeys("esc")),
	}
}

func (k KeyMap) Cards() []key.Binding {
	return []key.Binding{k.Fold, k.Jump}
}

func (k KeyMap) Scrolling() []key.Binding {
	return []key.Binding{k.HalfDown, k.HalfUp}
}

func (k KeyMap) Hints() []key.Binding {
	return []key.Binding{comp.Pair(k.Down, k.Up, "j/k", "move")}
}

func (k KeyMap) Paging() key.Binding {
	return comp.Pair(k.HalfDown, k.HalfUp, "ctrl+d/u", "page")
}

// cursorGlyph must stay one cell wide, or the heading's text shifts off the code under it.
const cursorGlyph = ""

// The ring glyphs are the tree pane's, so a state reads the same in both.
const (
	readGlyph    = "●"
	partialGlyph = "⊙"
	unreadGlyph  = "○"
)

type Model struct {
	Keys KeyMap

	theme   theme.Theme
	painter paint.Painter
	syntax  syntax.Syntax

	file     *review.File
	comments []store.Comment
	rows     []row

	cards  []card
	folded map[string]bool

	replaced map[string][]string
	expanded map[string]bool

	gen int64

	cursor int
	headAt []int
	// hunkEnd is kept apart from headAt because preview rows between hunks belong to neither.
	hunkEnd []int
	gutter  int

	waiting bool

	// split is what the reader asked for; a pane too narrow still draws unified.
	split bool
	side  store.Side

	// preview names a path rather than a mode, because the whole file is asked for one file at a time.
	preview string
	bodies  map[string]body
	// bodiesAt is not gen, because a reload blanks gen to zero and back.
	bodiesAt int64

	anchor place

	draft *draft

	offset int

	width  int
	height int
}

type rowKind int

const (
	codeRow rowKind = iota
	headRow
	noteRow
	cardRow
)

type row struct {
	text string

	kind rowKind
	line paint.Line
	note string

	hunk int

	right paint.Line

	card int

	seq int
}

// New returns an empty pane. An unknown syntax style degrades to other colours rather than failing.
func New(t theme.Theme) Model {
	s, _ := syntax.New(t.Syntax)

	return Model{
		Keys:    NewKeyMap(),
		theme:   t,
		painter: paint.Painter{Theme: t},
		syntax:  s,
		cursor:  -1,
		side:    store.SideHead,
		anchor:  place{seq: -1},
	}
}

// SetFile shows f, measured at generation at, from the top; nil empties the pane. The cursor waits for Select.
func (m *Model) SetFile(f *review.File, comments []store.Comment, replaced map[string][]string, at int64) {
	if at != 0 && at != m.bodiesAt {
		m.bodies, m.bodiesAt = nil, at
	}

	if f != nil && f.Diff.Path != m.preview {
		m.preview = ""
	}

	m.file, m.comments, m.replaced, m.gen, m.offset = f, comments, replaced, at, 0
	m.anchor = place{seq: -1}
	m.layout()
}

// Cursor returns the row under the cursor, or -1 for none.
func (m Model) Cursor() int { return m.cursor }

// Hunk names the hunk under the cursor as review does, and false with no cursor or no hunk under it.
func (m Model) Hunk() (store.Side, int, bool) {
	if m.cursor < 0 || m.hunkAt(m.cursor) < 0 {
		return "", 0, false
	}
	side, line := m.file.Hunks[m.hunkAt(m.cursor)].Name()
	return side, line, true
}

// Restore puts back a cursor and offset read from Cursor and Scroll.
func (m *Model) Restore(cursor, offset int) {
	m.clearSelection()
	m.point(cursor)
	m.offset = max(0, min(offset, m.maxOffset()))
}

// Select puts the cursor on the named hunk's heading and scrolls it to the top unless it already fits whole.
func (m *Model) Select(side store.Side, line int) {
	if m.file == nil || len(m.headAt) != len(m.file.Hunks) {
		return
	}
	m.clearSelection()

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

// SelectComment lands the cursor on a comment's card, with its line still on screen. No-op if the file lacks it.
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

func (m Model) Comment() (string, bool) {
	if c := m.cardOf(m.cursor); c != nil {
		return c.id, true
	}
	return "", false
}

func (m *Model) showCard(i int) {
	if m.height <= 0 {
		return
	}
	c := m.cards[i]

	top, hangs := c.at, c.at
	if c.anchor >= 0 {
		top, hangs = c.anchor, c.at-1
	}
	top, hangs = m.abovePin(top), m.abovePin(hangs)

	m.offset = max(min(m.offset, top), min(c.end()-m.height, hangs))
	m.clampOffset()
}

func (m Model) abovePin(at int) int {
	if h := m.headOf(at); h >= 0 && h < at {
		return at - 1
	}
	return at
}

func (m *Model) point(i int) {
	if i < -1 || i >= len(m.rows) {
		return
	}

	was := m.cursor
	m.cursor, m.waiting = i, false
	m.repaint(was, i, m.headOf(was), m.headOf(i))

	if lo, hi, on := m.span(); on {
		if was >= 0 {
			lo, hi = min(lo, was), max(hi, was)
		}
		for at := lo; at <= hi; at++ {
			m.repaint(at)
		}
	}

	m.repaintCard(was)
	m.repaintCard(i)
}

func (m *Model) repaintCard(i int) {
	c := m.cardOf(i)
	if c == nil {
		return
	}
	for at := c.at; at < c.end(); at++ {
		m.repaint(at)
	}
}

func (m *Model) moveTo(i int) {
	if len(m.rows) == 0 {
		return
	}
	i = max(0, min(i, len(m.rows)-1))

	if !m.reachable(i) {
		by := 1
		if i < m.cursor {
			by = -1
		}
		i = m.seek(i, by)
	}

	if c := m.cardOf(i); c != nil && i != c.at {
		leaving := m.cursor == c.at && i > m.cursor && c.end() < len(m.rows)
		i = c.at
		if leaving {
			i = c.end()
		}
	}

	m.point(i)
	m.reveal()
}

func (m Model) blank(i int) bool {
	if i < 0 || i >= len(m.rows) {
		return false
	}
	r := m.rows[i]
	return r.kind == noteRow && r.note == ""
}

func (m *Model) page(by int) {
	if m.scrollCard(by) {
		return
	}
	m.moveTo(m.cursor + by)
	m.place(m.middle())
	if by < 0 {
		m.showCardEnd()
	}
}

func (m *Model) step(by int) {
	if !m.scrollCard(by) {
		m.moveTo(m.cursor + by)
	}
}

func (m *Model) scrollCard(by int) bool {
	c := m.cardOf(m.cursor)
	if c == nil || m.height <= 0 {
		return false
	}

	switch {
	case by > 0 && c.end() > m.offset+m.height:
		m.offset = min(m.offset+by, c.end()-m.height)
	case by < 0 && c.at < m.offset:
		m.offset = max(m.offset+by, c.at)
	default:
		return false
	}
	m.clampOffset()
	return true
}

func (m *Model) showCardEnd() {
	if c := m.cardOf(m.cursor); c != nil && m.height > 0 {
		m.offset = max(m.offset, c.end()-m.height)
		m.clampOffset()
	}
}

func (m Model) middle() int { return (m.height - 1) / 2 }

func (m *Model) reveal() {
	if m.cursor >= 0 && m.height > 0 {
		m.offset = min(m.offset, m.lowestTop())
		m.offset = max(m.offset, m.cursor-m.height+1)
	}
	m.clampOffset()
	m.clearPin()
}

// lowestTop keeps a tall card's end on screen rather than its first row, so k onto it from below skips nothing.
func (m Model) lowestTop() int {
	if c := m.cardOf(m.cursor); c != nil {
		return max(c.at, c.end()-m.height)
	}
	return m.cursor
}

func (m *Model) clearPin() {
	if m.height <= 1 {
		return
	}

	if m.cursor != m.offset || m.offset <= 0 {
		return
	}
	if at := m.headOf(m.cursor); at >= 0 && at < m.offset {
		m.offset--
	}
}

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

func (m Model) draw(i int) string {
	text, style := m.render(i)
	if gap := m.width - lipgloss.Width(text); gap > 0 {
		text += style.Render(strings.Repeat(" ", gap))
	}
	return text
}

func (m Model) render(i int) (string, lipgloss.Style) {
	r := m.rows[i]

	var fill color.Color
	if i == m.cursor || m.inSelection(i) {
		fill = m.theme.SelectedBackground
	}

	var bar color.Color
	if i == m.cursor {
		bar = m.theme.Accent
	}

	weight := fill == nil && m.inSelection(i)

	switch r.kind {
	case headRow:
		return m.painter.HunkHeader(m.header(r.hunk, fill, bar), m.codeColumn(), m.width), lipgloss.NewStyle()
	case codeRow:
		if m.splitting() {
			return m.halves(r, fill, bar, weight), lipgloss.NewStyle()
		}
		l := r.line
		l.Fill, l.Bar, l.Weight = fill, bar, weight
		return m.painter.Line(l, m.gutter, m.width), lipgloss.NewStyle()
	case cardRow:
		c := m.cards[r.card]
		if on := m.cardOf(m.cursor); on != nil && on.id == c.id {
			return c.lit[i-c.at], lipgloss.NewStyle()
		}
		return c.plain[i-c.at], lipgloss.NewStyle()
	}

	style := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	if fill != nil {
		style = style.Background(fill)
	}
	return comp.Clip(style.Render(r.note), m.width, style), style
}

func (m Model) header(i int, fill, bar color.Color) paint.Header {
	h := m.file.Hunks[i]

	head := paint.Header{Text: comp.Safe(h.Diff.Header), Fill: fill, Bar: bar}
	head.Badge, head.BadgeColor = m.badge(h.State)
	if i == m.hunkAt(m.cursor) {
		head.Marker = cursorGlyph
	}
	return head
}

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

func (m Model) hunkAt(i int) int {
	if i < 0 || i >= len(m.rows) {
		return -1
	}
	return m.rows[i].hunk
}

func (m Model) headOf(i int) int {
	if h := m.hunkAt(i); h >= 0 {
		return m.headAt[h]
	}
	return -1
}

func (m *Model) scrollToCursor() {
	at := m.headOf(m.cursor)
	if m.height <= 0 || at < 0 || m.fits(at) {
		return
	}
	m.offset = min(at, m.maxOffset())
}

func (m Model) pinned() int {
	if m.offset >= len(m.rows) {
		return -1
	}

	if m.cursor == m.offset {
		return -1
	}

	if m.blank(m.offset) {
		return -1
	}

	at := m.headOf(m.offset)
	if at < 0 || at >= m.offset {
		return -1
	}
	return at
}

func (m *Model) place(row int) {
	if m.cursor < 0 || m.height <= 0 {
		return
	}
	m.offset = m.cursor - row
	m.clampOffset()

	m.clearPin()
}

func (m Model) fits(at int) bool {
	end := len(m.rows)
	for i, head := range m.headAt {
		if head == at {
			end = m.hunkEnd[i]
			break
		}
	}
	return at >= m.offset && end <= m.offset+m.height
}

// SetSize sets the pane's inner size. Only the first call scrolls to the cursor.
func (m *Model) SetSize(width, height int) {
	first := m.width == 0 && m.height == 0
	was := m.placeOf(m.cursor)
	mode := m.splitting()

	m.width, m.height = width, height

	if m.draft != nil {
		m.draft.area.SetWidth(m.draftWidth())
		m.capBox()
	}

	if m.splitting() != mode {
		m.remode()
	} else {
		m.relayout(was)
	}
	m.reveal()

	if first {
		m.scrollToCursor()
	}
}

func (m Model) Scroll() comp.Scroll {
	return comp.Scroll{Offset: m.offset, Height: m.height, Total: len(m.rows)}
}

func (m Model) Path() string {
	if m.file == nil {
		return ""
	}
	return m.file.Diff.Path
}

// Placing reports whether a z is waiting for its second key.
func (m Model) Placing() bool { return m.waiting }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.draft != nil {
		return m, m.typing(msg)
	}

	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

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
		m.step(1)
	case key.Matches(press, m.Keys.Up):
		m.step(-1)
	case key.Matches(press, m.Keys.HalfDown):
		m.page(m.half())
	case key.Matches(press, m.Keys.HalfUp):
		m.page(-m.half())
	case key.Matches(press, m.Keys.Top):
		m.moveTo(0)
	case key.Matches(press, m.Keys.Bottom):
		m.moveTo(len(m.rows) - 1)
		m.showCardEnd()
	}
	return m, nil
}

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

// Expand toggles the card under the cursor between its whole replaced block and the first few lines.
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

func (m *Model) jump() {
	if c := m.cardOf(m.cursor); c != nil && c.anchor >= 0 {
		m.moveTo(c.anchor)
	}
}

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

func (m *Model) layout() {
	m.rows, m.headAt, m.hunkEnd, m.cards, m.cursor = nil, nil, nil, nil, -1
	if m.file == nil {
		return
	}

	tokens := m.tokens(*m.file)
	m.gutter = paint.Gutter(m.widest())

	mine := m.mine()

	if m.draft != nil && m.draft.path == m.file.Diff.Path {
		at := m.draft.at

		if i := index(mine, m.draft.edits); i >= 0 {
			mine[i] = at
		} else {
			at.GenerationID = m.gen
			mine = append(mine, at)
		}
	}

	placed := make([]bool, len(mine))

	for i, c := range mine {
		if c.Scope == store.ScopeFile {
			placed[i] = true
			m.addCard(c, -1, -1)
		}
	}

	first := make(map[string]int, len(mine))

	base, seq := 0, 0
	add := func(r row) {
		r.card, r.seq = -1, seq
		seq++
		m.rows = append(m.rows, r)
	}

	split := m.splitting()

	var runs []fill
	if m.previewing() {
		runs = m.fills()
	}
	whole := m.body()

	hang := func(lines []diff.Line, p pair, hunk int) {
		at := len(m.rows) - 1

		for _, k := range sides(p) {
			l := lines[k]

			for j, c := range mine {
				if !m.live(c) {
					continue
				}
				if _, seen := first[c.ID]; !seen && on(c, l, c.Start) {
					first[c.ID] = at
				}
				if !placed[j] && on(c, l, c.End) {
					placed[j] = true

					anchor, ok := first[c.ID]
					if !ok {
						anchor = at
					}
					m.addCard(c, hunk, anchor)
				}
			}
		}
	}

	source := func(lines []diff.Line, toks [][]syntax.Token, hunk int) {
		for _, p := range pairs(lines, split) {
			add(m.code(lines, p, toks, hunk, split))

			if eol(lines, p) {
				add(row{kind: noteRow, hunk: hunk, note: `\ No newline at end of file`})
			}

			hang(lines, p, hunk)
		}
	}

	frame := func() {
		if n := len(m.rows); n > 0 && !m.blank(n-1) {
			add(row{kind: noteRow, hunk: -1})
		}
	}

	fillIn := func(f fill) {
		lines := f.lines(whole)
		if len(lines) == 0 {
			return
		}
		frame()
		source(lines, f.tokens(whole), -1)
	}

	for i, h := range m.file.Hunks {
		switch {
		case runs != nil:
			fillIn(runs[i])
			frame()
		case i > 0:
			add(row{kind: noteRow, hunk: i - 1})
		}

		m.headAt = append(m.headAt, len(m.rows))
		add(row{kind: headRow, hunk: i})

		source(h.Diff.Lines, tokens[base:], i)

		base += len(h.Diff.Lines)
		m.hunkEnd = append(m.hunkEnd, len(m.rows))
	}

	switch {
	case runs != nil:
		fillIn(runs[len(runs)-1])
	case len(m.file.Hunks) == 0:
		add(row{kind: noteRow, hunk: -1, note: emptyReason(*m.file)})
	}

	for i, c := range mine {
		if !placed[i] {
			m.addCard(c, -1, -1)
		}
	}

	m.repaintAll()
}

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

func (m Model) live(c store.Comment) bool {
	return c.GenerationID == m.gen
}

func on(c store.Comment, l diff.Line, n int) bool {
	if n == 0 {
		return false
	}
	if c.Side == store.SideBase {
		return l.Old == n
	}
	return l.New == n
}

func (m *Model) repaintAll() {
	if m.width <= 0 {
		return
	}
	for i := range m.rows {
		m.rows[i].text = m.draw(i)
	}
}

// place survives a relayout where a row index does not, because a card's height moves with the width.
type place struct {
	comment string
	seq     int
}

func (m Model) placeOf(i int) place {
	if i < 0 || i >= len(m.rows) {
		return place{seq: -1}
	}
	if c := m.cardOf(i); c != nil {
		return place{comment: c.id, seq: -1}
	}
	return place{seq: m.rows[i].seq}
}

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

func (m *Model) relayout(p place) {
	had := m.cursor >= 0
	m.layout()

	at := m.rowAt(p)
	if at < 0 && had && len(m.rows) > 0 {
		at = 0
	}
	m.point(at)
}

// tokens lexes each side as one body under that side's own path, since a rename can change the language.
func (m *Model) tokens(f review.File) [][]syntax.Token {
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

func basePath(f diff.File) string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}

func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, m.maxOffset()))
}

func (m Model) maxOffset() int { return max(len(m.rows)-m.height, 0) }

func (m Model) half() int {
	return max(m.height/2, 1)
}

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

func (m Model) widest() int {
	n := 0
	for _, h := range m.file.Hunks {
		n = max(n, last(h.Diff.OldStart, h.Diff.OldLines), last(h.Diff.NewStart, h.Diff.NewLines))
	}
	if !m.previewing() {
		return n
	}
	for _, f := range m.fills() {
		n = max(n, f.to, f.to+f.delta)
	}
	return n
}

func last(start, lines int) int {
	if lines == 0 {
		return start
	}
	return start + lines - 1
}

func emptyReason(f review.File) string {
	if f.Diff.Omitted != "" {
		return f.Diff.Omitted
	}
	return "no changed lines"
}
