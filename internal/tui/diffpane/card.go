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

// The comment states as glyphs. They are the hunk badges' family, so one ladder
// reads the same wherever the pane draws one.
const (
	openGlyph      = "○"
	addressedGlyph = "⊙"
	resolvedGlyph  = "●"
	orphanedGlyph  = "⊘"
)

// cardMin is the narrowest a bordered card gets: two borders, a gutter each
// side, and a column of text. Under it the card is drawn as a bare row.
const cardMin = 6

// cardGutter is the space between a card's border and what it holds. Text
// against the border reads as a rendering fault rather than as a box.
const cardGutter = 1

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

// folds is whether a comment draws folded: settled by default, and flipped by
// whatever the reader last pressed space on.
func (m Model) folds(c store.Comment) bool {
	return (c.State == store.CommentResolved) != m.folded[c.ID]
}

// cardBox is where a card starts and how wide it is. It hangs at the column the
// code starts in, and gives that up rather than shrink past what a border needs.
func (m Model) cardBox() (int, int) {
	at := min(paint.CodeColumn(m.gutter), max(m.width-cardMin, 0))
	return at, m.width - at
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
	if width < cardMin || m.folds(c) {
		plain := lipgloss.NewStyle()
		lit := lipgloss.NewStyle().Background(m.theme.SelectedBackground)
		return []string{m.pad(indent(m.bareCard(c, placed, plain), at), plain)},
			[]string{m.pad(indent(m.bareCard(c, placed, lit), at), lit)}
	}

	body := m.cardBody(c, width)
	box := comp.NewPane(m.theme).Label(m.cardLabel(c, placed)).Size(width, len(body)+2)

	plain := lines(box.Focus(false).Render(strings.Join(body, "\n")))
	lit := lines(box.Focus(true).Footer("", m.cardHints(width, placed)).
		Render(strings.Join(body, "\n")))

	for i := range plain {
		plain[i] = indent(plain[i], at)
		lit[i] = indent(lit[i], at)
	}
	return plain, lit
}

// cardBody is the comment's words, folded to what the box leaves them and set
// in off the border. Prose is capped: a card on a wide terminal still reads.
func (m Model) cardBody(c store.Comment, width int) []string {
	room := max(width-2-2*cardGutter, 1)

	text := lipgloss.NewStyle().Foreground(m.theme.Text)
	gutter := strings.Repeat(" ", cardGutter)

	var out []string
	for _, line := range comp.Wrap(comp.Safe(c.Body), min(room, comp.BodyWidth)) {
		out = append(out, gutter+text.Render(line))
	}
	if len(out) == 0 {
		out = append(out, gutter+m.subtle().Render("no words"))
	}
	return out
}

// bareCard is the one-row form: the badge, the state, and enough of the body to
// recall it. A folded card and a pane too narrow for a border both take it.
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

	head := base.Foreground(on).Render(glyph) +
		base.Foreground(m.theme.Subtle).Render(" "+string(c.State))
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

// cardHints is what the lit card answers to, dropped from the tail until the
// line fits its border. enter is absent on a card with no line to go to.
func (m Model) cardHints(width int, placed bool) string {
	parts := []string{"space fold"}
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
	for _, line := range strings.Split(comp.Safe(body), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}
