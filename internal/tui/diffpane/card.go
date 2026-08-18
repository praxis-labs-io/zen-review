package diffpane

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/paint"

	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// The comment states as glyphs: a diamond, where a hunk's badge is a circle.
// The two ladders mean different things and cannot share a shape.
const (
	// Hollow, centred and filled is the progression the circles make too, so a
	// column of either still reads at a glance.
	openGlyph      = "◇"
	addressedGlyph = "◈"
	resolvedGlyph  = "◆"

	// Orphaned leaves the family. It is a loss rather than a stage.
	orphanedGlyph = "✕"
)

// cardMin is the narrowest a bordered card gets, which is room for a few words.
// Under it a card is a box round an ellipsis, so the indent goes then the border.
const cardMin = 20

// foldGlyph opens a folded card's one row. It is zen-octo's, which marks a
// folded block the same way.
const foldGlyph = "▸"

// cardGutter is the space between a card's border and what it holds. Text
// against the border reads as a rendering fault rather than as a box.
const cardGutter = 1

// noWords stands in for a body that has none. The engine refuses an empty one,
// so this is the card saying the row is not a rendering fault.
const noWords = "no words"

// answerRail is the gutter the answer's box hangs in. The rail is drawn in it,
// so the box is that much narrower and that much further in.
const answerRail = 2

// The rail, read down: past the box's top border, into its first row of words,
// then clear. There is only ever one answer, so the elbow is never a tee.
const (
	railDown  = "│ "
	railElbow = "╰─"
	railClear = "  "
)

// card is one comment on screen, and both the ways it draws. The cursor moving
// in or out changes its whole border, and layout is where that is paid for.
type card struct {
	id string

	// at is the card's first row and the only one the cursor lands on. anchor is
	// the first row of the code it answers, and -1 when the diff has none.
	at     int
	anchor int

	plain []string
	lit   []string
}

// end is one past the card's last row.
func (c card) end() int { return c.at + len(c.plain) }

// cardOf is the card a row belongs to, and nil for a row outside every one.
func (m Model) cardOf(i int) *card {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	if at := m.rows[i].card; at >= 0 {
		return &m.cards[at]
	}
	return nil
}

// LeaveCard takes the cursor off whatever card it is on and onto the nearest
// code, above by preference. It is where a delete leaves the reader.
func (m *Model) LeaveCard() {
	c := m.cardOf(m.cursor)
	if c == nil {
		return
	}

	// The card that went is gone from the rows, so the cursor is left on the one
	// that slid up into them, and the next press of the key takes that.
	for i := c.at - 1; i >= 0; i-- {
		if m.rows[i].card < 0 {
			m.point(i)
			m.reveal()
			return
		}
	}
	for i := c.end(); i < len(m.rows); i++ {
		if m.rows[i].card < 0 {
			m.point(i)
			m.reveal()
			return
		}
	}
}

// folds is whether a comment draws folded: settled by default, and flipped by
// whatever the reader last pressed space on.
func (m Model) folds(c store.Comment) bool {
	return (c.State == store.CommentResolved) != m.folded[c.ID]
}

// cardBox is where a card starts and how wide it is. It hangs at the column the
// code starts in, and gives that up rather than shrink past what a border needs.
func (m Model) cardBox() (int, int) {
	at := min(paint.CodeColumn(m.gutter), max(m.width-cardMin, 0))

	// It stops at what prose reads at, where the code beside it runs the whole
	// pane. A wide box around one column of words reads as a fault, not a card.
	return at, min(m.width-at, comp.BodyWidth+2+2*cardGutter)
}

// addCard renders one comment into rows and records where they landed. The rows
// carry the drawn text; the card carries both drawings and is what lands.
func (m *Model) addCard(c store.Comment, hunk, anchor int) {
	plain, lit := m.drawCard(c, anchor >= 0)

	at, which := len(m.rows), len(m.cards)
	m.cards = append(m.cards, card{id: c.ID, at: at, anchor: anchor, plain: plain, lit: lit})

	for _, line := range plain {
		m.rows = append(m.rows, row{kind: cardRow, hunk: hunk, card: which, seq: -1, text: line})
	}
}

// drawCard is a comment as its rows, unlit and lit. A pane with no width yet
// gets one blank row, and the first resize gives the card its real height.
func (m Model) drawCard(c store.Comment, placed bool) ([]string, []string) {
	if m.width <= 0 {
		return []string{""}, []string{""}
	}

	at, width := m.cardBox()
	if width < cardMin {
		return m.bareRow(c, placed, at, lipgloss.NewStyle()),
			m.bareRow(c, placed, at, lipgloss.NewStyle().Background(m.theme.SelectedBackground))
	}

	// The box being typed in is always lit and names its own two keys, because
	// it holds every key on the keyboard while it is up.
	if c.ID == draftID {
		box := comp.NewPane(m.theme).Label(m.cardLabel(c, placed)).
			Size(width, m.draft.area.Height()+2)
		rows := lines(box.Focus(true).Footer("", m.draftHints(width)).
			Render(strings.Join(m.draftBody(), "\n")))

		for i := range rows {
			rows[i] = indent(rows[i], at)
		}
		return rows, rows
	}

	// A folded card keeps its box. Without one it is a line of grey text in a
	// column of diff, which is what the diff's own notes look like.
	folded := m.folds(c)

	// Built one way or the other, never both: either sanitises the whole body,
	// and the loser's pass is work every relayout pays for nothing.
	var body []string
	if folded {
		body = m.foldedBody(c, width)
	} else {
		body = m.cardBody(c, width)
	}

	box := comp.NewPane(m.theme).Label(m.cardLabel(c, placed)).Size(width, len(body)+2)

	plain := lines(box.Focus(false).Render(strings.Join(body, "\n")))
	lit := lines(box.Focus(true).Footer("", m.cardHints(c, width, placed, folded)).
		Render(strings.Join(body, "\n")))

	// A folded card takes its answer with it. One row is what folding means, and
	// a box still hanging off it says the card is open.
	if !folded {
		plain = append(plain, m.answerBox(c, width, false)...)
		lit = append(lit, m.answerBox(c, width, true)...)
	}

	for i := range plain {
		plain[i] = indent(plain[i], at)
		lit[i] = indent(lit[i], at)
	}
	return plain, lit
}

// cardBody is the comment's words, and the stand-in when it has none.
func (m Model) cardBody(c store.Comment, width int) []string {
	out := m.boxBody(c.Body, width)
	if len(out) == 0 {
		out = append(out, strings.Repeat(" ", cardGutter)+m.subtle().Render(noWords))
	}
	return out
}

// boxBody is words folded to what a box leaves them and set in off the border.
// Prose is capped: a card on a wide terminal still reads.
func (m Model) boxBody(words string, width int) []string {
	room := max(width-2-2*cardGutter, 1)

	text := lipgloss.NewStyle().Foreground(m.theme.Text)
	gutter := strings.Repeat(" ", cardGutter)

	var out []string
	for _, line := range comp.Wrap(comp.Prose(words), min(room, comp.BodyWidth)) {
		out = append(out, gutter+text.Render(line))
	}
	return out
}

// answerBox is what an address left behind, as its own box hanging off the card
// on a rail. It is nil for a comment with no answer and for a pane with no room
// to draw a second border, where the card is left whole rather than shrunk.
//
// It takes the card's focus rather than its own. The card is one stop for the
// cursor, so there is no state where one of the two boxes is lit and not the
// other. The rail never lights: it is the only cue for the nesting, and a cue
// that changes colour is a second thing to read.
func (m Model) answerBox(c store.Comment, width int, lit bool) []string {
	inner := width - answerRail
	if c.Answer == "" || inner < cardMin {
		return nil
	}

	body := m.boxBody(c.Answer, inner)
	box := comp.NewPane(m.theme).Label(" "+m.subtle().Render("answer")+" ").
		Size(inner, len(body)+2)

	rail := lipgloss.NewStyle().Foreground(m.theme.BorderMutedOrSubtle())
	rows := lines(box.Focus(lit).Render(strings.Join(body, "\n")))
	for i := range rows {
		switch i {
		case 0:
			rows[i] = rail.Render(railDown) + rows[i]
		case 1:
			rows[i] = rail.Render(railElbow) + rows[i]
		default:
			rows[i] = railClear + rows[i]
		}
	}
	return rows
}

// foldedBody is a settled card's one row: the mark saying it is folded, and
// enough of the body to know which comment the box is standing for.
func (m Model) foldedBody(c store.Comment, width int) []string {
	room := max(width-2-2*cardGutter, 1)

	line := foldGlyph + " " + noWords
	if first := firstLine(c.Body); first != "" {
		line = foldGlyph + " " + first
	}
	return []string{strings.Repeat(" ", cardGutter) +
		comp.Clip(m.subtle().Render(line), room, m.subtle())}
}

// bareRow is the one-row form finished to the pane, its indent painted in the
// row's own style so a lit one fills from the edge the way a code row does.
func (m Model) bareRow(c store.Comment, placed bool, at int, base lipgloss.Style) []string {
	lead := base.Render(strings.Repeat(" ", max(at, 0)))
	return []string{m.pad(lead+m.bareCard(c, placed, base), base)}
}

// bareCard is the borderless form: the badge, the state, and enough of the body
// to recall it. Only a pane too narrow for a box takes it.
func (m Model) bareCard(c store.Comment, placed bool, base lipgloss.Style) string {
	row := m.cardHead(c, placed, base)
	if first := firstLine(c.Body); first != "" {
		row += base.Foreground(m.theme.Subtle).Render(" · " + first)
	}
	return row
}

// cardLabel is the top border's text: the badge for the glance and the word for
// the scan. One weight down a column cannot say which state a card is in.
func (m Model) cardLabel(c store.Comment, placed bool) string {
	return " " + m.cardHead(c, placed, lipgloss.NewStyle()) + " "
}

// cardHead is the badge, the state and whatever the card's own position cannot
// say, over whatever background the row is painted on.
func (m Model) cardHead(c store.Comment, placed bool, base lipgloss.Style) string {
	glyph, on := m.commentBadge(c.State)

	// The box is not a comment yet, and open is a state it reaches by landing.
	// One being retyped keeps its badge, because its state is not what changes.
	word := string(c.State)
	if c.ID == draftID {
		word = "new"
		if m.draft.edits != "" {
			word = "editing"
		}
	}

	head := base.Foreground(on).Render(glyph) +
		base.Foreground(m.theme.Subtle).Render(" "+word)
	if where := commentWhere(c, placed); where != "" {
		head += base.Foreground(m.theme.Subtle).Render(" · " + where)
	}
	return head
}

// commentBadge is a comment's state as a glyph and the weight it reads at.
func (m Model) commentBadge(s store.CommentState) (string, color.Color) {
	switch s {
	case store.CommentAddressed:
		return addressedGlyph, m.theme.Warning
	case store.CommentResolved:
		return resolvedGlyph, m.theme.Subtle
	case store.CommentOrphaned:
		return orphanedGlyph, m.theme.Error
	default:
		return openGlyph, m.theme.Accent
	}
}

// commentWhere is the part of a comment's anchor the card's own position cannot
// say. A card under its line needs no number: the gutter beside it has one.
func commentWhere(c store.Comment, placed bool) string {
	switch {
	case c.Scope == store.ScopeFile:
		return "file"
	case !placed && c.Start == c.End:
		return "was line " + strconv.Itoa(c.Start)
	case !placed:
		return "was lines " + span(c)
	case c.Start != c.End:
		return "lines " + span(c)
	}
	return ""
}

func span(c store.Comment) string {
	return strconv.Itoa(c.Start) + "-" + strconv.Itoa(c.End)
}

// cardHints is what the lit card answers to, dropped from the tail until it
// fits. x, e and D are the root's, named here because the card is what they reach.
func (m Model) cardHints(c store.Comment, width int, placed, folded bool) string {
	// The word is the direction the key goes, not the state it is in. A folded
	// card naming the fold says the press would do what has been done.
	word := "space fold"
	if folded {
		word = "space open"
	}

	// Last, so a card too narrow for all five keeps the three it had. They reach a
	// comment in any state: a typo in a resolved one is still a typo.
	parts := []string{word, "e edit", "D delete"}
	if c.State != store.CommentResolved {
		parts = append([]string{"x resolve"}, parts...)
	}
	if placed {
		parts = append([]string{"⏎ line"}, parts...)
	}

	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	for len(parts) > 0 {
		line := strings.Join(parts, " · ") + " "
		if lipgloss.Width(line) <= max(width-3, 0) {
			return muted.Render(line)
		}
		parts = parts[:len(parts)-1]
	}
	return ""
}

// pad fits a row to the pane, finishing it in the style it was drawn in so a
// filled one runs its background all the way across.
func (m Model) pad(row string, style lipgloss.Style) string {
	row = comp.Clip(row, m.width, style)
	if gap := m.width - lipgloss.Width(row); gap > 0 {
		row += style.Render(strings.Repeat(" ", gap))
	}
	return row
}

func (m Model) subtle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme.Subtle)
}

// indent sets a rendered row in by n columns.
func indent(row string, n int) string {
	if n <= 0 {
		return row
	}
	return strings.Repeat(" ", n) + row
}

func lines(block string) []string { return strings.Split(block, "\n") }

// firstLine is enough of a body to recall which comment it is, on one row.
func firstLine(body string) string {
	for _, line := range strings.Split(comp.Prose(body), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}
