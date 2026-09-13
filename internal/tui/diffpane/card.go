package diffpane

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
)

// Comment states are diamonds, not the hunk badge's circles: the two ladders mean different things.
const (
	openGlyph      = "◇"
	addressedGlyph = "◈"
	resolvedGlyph  = "◆"

	orphanedGlyph = "✕" // a loss, not a stage
)

// cardMin is the narrowest a bordered card gets; under it the box holds little but an ellipsis.
const cardMin = 20

// foldGlyph matches zen-octo's folded-block marker.
const foldGlyph = "▸"

const cardGutter = 1

const noWords = "no words"

const replacedShown = 3

const responseRail = 2

// The elbow is never a tee, because a comment has only one response.
const (
	railDown  = "│ "
	railElbow = "╰─"
	railClear = "  "
)

type placement int

const (
	unplaced placement = iota
	underLine
	underHeading
)

type card struct {
	id string

	at     int
	anchor int

	plain []string
	lit   []string
}

func (c card) end() int { return c.at + len(c.plain) }

func (m Model) cardOf(i int) *card {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	if at := m.rows[i].card; at >= 0 {
		return &m.cards[at]
	}
	return nil
}

// LeaveCard moves the cursor off its card onto the nearest code row, above first.
func (m *Model) LeaveCard() {
	c := m.cardOf(m.cursor)
	if c == nil {
		return
	}

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

func (m Model) folds(c store.Comment) bool {
	return (c.State == store.CommentResolved) != m.folded[c.ID]
}

func (m Model) cardBox() (int, int) {
	at := min(m.codeColumn(), max(m.width-cardMin, 0))

	return at, min(m.width-at, comp.BodyWidth+2+2*cardGutter)
}

func (m *Model) addCard(c store.Comment, hunk, anchor int) {
	plain, lit := m.drawCard(c, m.replacedTokens(c), m.placementOf(anchor))

	at, which := len(m.rows), len(m.cards)
	m.cards = append(m.cards, card{id: c.ID, at: at, anchor: anchor, plain: plain, lit: lit})

	for _, line := range plain {
		m.rows = append(m.rows, row{kind: cardRow, hunk: hunk, card: which, seq: -1, text: line})
	}
}

func (m Model) placementOf(anchor int) placement {
	switch {
	case anchor < 0:
		return unplaced
	case m.rows[anchor].kind == headRow:
		return underHeading
	default:
		return underLine
	}
}

// replacedTokens is sized to the block, not to Chroma's output, which drops a trailing blank line.
func (m *Model) replacedTokens(c store.Comment) [][]syntax.Token {
	block := m.replaced[c.ID]
	if len(block) == 0 {
		return nil
	}

	safe := make([]string, len(block))
	for i, line := range block {
		safe[i] = comp.Code(line)
	}

	out := make([][]syntax.Token, len(block))
	copy(out, m.syntax.Lines(c.Path, strings.Join(safe, "\n")))
	return out
}

func (m Model) drawCard(c store.Comment, block [][]syntax.Token, placed placement) ([]string, []string) {
	if m.width <= 0 {
		return []string{""}, []string{""}
	}

	at, width := m.cardBox()
	if width < cardMin {
		return m.bareRow(c, placed, at, lipgloss.NewStyle()),
			m.bareRow(c, placed, at, lipgloss.NewStyle().
				Background(m.theme.SelectedBackground).Bold(true))
	}

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

	folded := m.folds(c)

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

	if !folded {
		box := m.responseBox(c, block, width)
		plain = append(plain, box...)
		lit = append(lit, box...)
	}

	for i := range plain {
		plain[i] = indent(plain[i], at)
		lit[i] = indent(lit[i], at)
	}
	return plain, lit
}

func (m Model) cardBody(c store.Comment, width int) []string {
	out := m.boxBody(c.Body, width)
	if len(out) == 0 {
		out = append(out, strings.Repeat(" ", cardGutter)+m.subtle().Render(noWords))
	}
	return out
}

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

func (m Model) responseBox(c store.Comment, block [][]syntax.Token, width int) []string {
	inner := width - responseRail
	if (c.Response == "" && len(block) == 0) || inner < cardMin {
		return nil
	}

	body := m.boxBody(c.Response, inner)
	if code := m.replacedBody(block, inner, m.expanded[c.ID]); len(code) > 0 {
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, code...)
	}

	box := comp.NewPane(m.theme).Label(" "+m.subtle().Render("response")+" ").
		Size(inner, len(body)+2)

	rail := lipgloss.NewStyle().Foreground(m.theme.BorderMutedOrSubtle())
	rows := lines(box.Focus(false).Render(strings.Join(body, "\n")))
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

func (m Model) replacedBody(block [][]syntax.Token, width int, expanded bool) []string {
	shown := block
	if !expanded && len(block) > replacedShown {
		shown = block[:replacedShown]
	}

	out := make([]string, 0, len(shown)+1)
	for _, tokens := range shown {
		out = append(out, m.painter.Body(paint.Line{Kind: paint.Removed, Tokens: tokens}, max(width-2, 1)))
	}

	if rest := len(block) - len(shown); rest > 0 {
		room := max(width-2-2*cardGutter, 1)
		muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
		more := "… " + strconv.Itoa(rest) + " more"
		out = append(out, strings.Repeat(" ", cardGutter)+comp.Clip(muted.Render(more), room, muted))
	}
	return out
}

func (m Model) foldedBody(c store.Comment, width int) []string {
	room := max(width-2-2*cardGutter, 1)

	line := foldGlyph + " " + noWords
	if first := firstLine(c.Body); first != "" {
		line = foldGlyph + " " + first
	}
	return []string{strings.Repeat(" ", cardGutter) +
		comp.Clip(m.subtle().Render(line), room, m.subtle())}
}

func (m Model) bareRow(c store.Comment, placed placement, at int, base lipgloss.Style) []string {
	lead := base.Render(strings.Repeat(" ", max(at, 0)))
	return []string{m.pad(lead+m.bareCard(c, placed, base), base)}
}

func (m Model) bareCard(c store.Comment, placed placement, base lipgloss.Style) string {
	row := m.cardHead(c, placed, base)
	if first := firstLine(c.Body); first != "" {
		row += base.Foreground(m.theme.Subtle).Render(" · " + first)
	}
	return row
}

func (m Model) cardLabel(c store.Comment, placed placement) string {
	return " " + m.cardHead(c, placed, lipgloss.NewStyle()) + " "
}

func (m Model) cardHead(c store.Comment, placed placement, base lipgloss.Style) string {
	glyph, on := m.commentBadge(c.State)

	word := string(c.State)
	if c.ID == draftID {
		word = "new"
		if m.draft.edits != "" {
			word = "editing"
		}
	}

	head := base.Foreground(on).Render(glyph) +
		base.Foreground(m.theme.Subtle).Render(" "+word)
	if where := commentWhere(c, placed, m.live(c)); where != "" {
		head += base.Foreground(m.theme.Subtle).Render(" · " + where)
	}
	return head
}

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

// commentWhere says "was" only for an orphan or a comment frozen at another generation.
func commentWhere(c store.Comment, placed placement, live bool) string {
	if c.Scope == store.ScopeFile {
		return "file"
	}
	if placed == underHeading {
		return "hunk"
	}

	if placed == underLine && c.Start == c.End {
		return ""
	}

	what := "line " + strconv.Itoa(c.Start)
	if c.Start != c.End {
		what = "lines " + span(c)
	}

	if placed == unplaced && (!live || c.State == store.CommentOrphaned) {
		return "was " + what
	}
	return what
}

func span(c store.Comment) string {
	return strconv.Itoa(c.Start) + "-" + strconv.Itoa(c.End)
}

func (m Model) cardHints(c store.Comment, width int, placed placement, folded bool) string {
	word := "space fold"
	if folded {
		word = "space open"
	}

	parts := []string{word}

	if !folded && len(m.replaced[c.ID]) > replacedShown {
		hint := "> more"
		if m.expanded[c.ID] {
			hint = "> less"
		}
		parts = append(parts, hint)
	}

	parts = append(parts, "e edit", "D delete")

	if c.State != store.CommentResolved {
		parts = append([]string{"x resolve"}, parts...)
	}
	if placed != unplaced {
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

func indent(row string, n int) string {
	if n <= 0 {
		return row
	}
	return strings.Repeat(" ", n) + row
}

func lines(block string) []string { return strings.Split(block, "\n") }

func firstLine(body string) string {
	for _, line := range strings.Split(comp.Prose(body), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}
