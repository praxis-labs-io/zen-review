package diffpane

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// draftID names the card being typed into. A stored comment's id is twelve hex
// characters, so no comment can answer to this one.
const draftID = "draft"

// draftRows is the shortest the box gets. It grows to hold what is typed into
// it, because a box that scrolls hides the sentence somebody is still writing.
const draftRows = 4

// draft is the comment being typed: where it will land, and the box it is typed
// in. The pane draws it as the card it is about to become.
type draft struct {
	at   store.Comment
	area textarea.Model

	// path is the file the box was opened over. A reload landing under it can
	// put another file in the pane, and the box belongs to neither.
	path string

	// edits names the comment the box is standing in for, and is empty for one
	// being written. That card comes down while the box is up.
	edits string
}

// FitsBox is whether the pane has the room to draw a box being typed in. A box
// nobody can see still holds every key, so the caller puts it somewhere else.
func (m Model) FitsBox() bool {
	_, width := m.cardBox()
	return m.file != nil && width >= cardMin && m.height >= draftRows+2
}

// Compose opens a box where a new comment will hang, and false for a pane with
// no room to draw one.
func (m *Model) Compose(c store.Comment) (tea.Cmd, bool) {
	c.State = store.CommentOpen
	return m.open(c, "", "")
}

// Edit opens the box over a card that is already there, holding what it says.
// The card comes down: the box is standing in its place.
func (m *Model) Edit(c store.Comment) (tea.Cmd, bool) {
	return m.open(c, c.Body, c.ID)
}

// open puts a box in the pane and takes the cursor onto it.
func (m *Model) open(c store.Comment, body, edits string) (tea.Cmd, bool) {
	if !m.FitsBox() {
		return nil, false
	}
	c.ID, c.Body = draftID, ""

	area := comp.Textarea(m.theme)
	area.SetWidth(m.draftWidth())
	area.SetValue(body)

	m.clearSelection()
	m.draft = &draft{at: c, area: area, path: m.file.Diff.Path, edits: edits}
	m.draft.area.SetHeight(m.draftHeight())

	// Focused before the box is drawn, or the first frame is one with no cursor
	// in it and the reader has to type to find out where they are.
	cmd := m.draft.area.Focus()
	m.layout()

	if at := m.rowAt(place{comment: draftID, seq: -1}); at >= 0 {
		m.point(at)
		m.showCard(m.rows[at].card)
	}
	return cmd, true
}

// Composing is whether a box is up, which is whether the pane has the keys.
func (m Model) Composing() bool { return m.draft != nil }

// TypingAt is where the terminal's cursor goes while a box is up, in the pane's
// own coordinates. It is nil for a pane holding no box, or one scrolled off it.
func (m Model) TypingAt() *tea.Cursor {
	if m.draft == nil {
		return nil
	}

	c := m.draft.area.Cursor()
	at := m.rowAt(place{comment: draftID, seq: -1})
	if c == nil || at < 0 {
		return nil
	}

	// Past the indent the card hangs at, its border and the gutter inside it,
	// and past the border row the box opens with.
	left, _ := m.cardBox()
	c.X += left + 1 + cardGutter
	c.Y += at - m.offset + 1

	if c.Y < 0 || c.Y >= m.height || c.X >= m.width {
		return nil
	}
	return c
}

// Draft is what has been typed, and empty when no box is up.
func (m Model) Draft() string {
	if m.draft == nil {
		return ""
	}
	return m.draft.area.Value()
}

// CloseDraft takes the box down and leaves the cursor where the reader was when
// they opened it: on the card the box stood in for, or on the code it hung off.
func (m *Model) CloseDraft() {
	if m.draft == nil {
		return
	}

	at, edits := m.anchorOf(draftID), m.draft.edits
	m.draft = nil
	m.layout()

	// A card that came back unlit is one x and e no longer reach, and the reader
	// pressed the key from it.
	if edits != "" {
		m.SelectComment(edits)
		return
	}

	if at >= 0 {
		m.point(min(at, len(m.rows)-1))
		m.reveal()
	}
}

// typing takes a key to the body and redraws the box in place. Nothing else on
// screen moves for it until the box grows, which pushes the file down a row.
func (m *Model) typing(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.draft.area, cmd = m.draft.area.Update(msg)

	if h := m.draftHeight(); h != m.draft.area.Height() {
		m.draft.area.SetHeight(h)
		m.grew()
		return cmd
	}

	if at := m.rowAt(place{comment: draftID, seq: -1}); at >= 0 {
		i := m.rows[at].card
		m.cards[i].plain, m.cards[i].lit = m.drawCard(m.draft.at, m.cards[i].anchor >= 0)
		m.repaintCard(at)
	}
	return cmd
}

// grew lays the file out around a box that changed height and keeps the whole of
// it on screen, the row it gained included.
func (m *Model) grew() {
	at := place{comment: draftID, seq: -1}
	m.relayout(at)

	if i := m.rowAt(at); i >= 0 {
		m.showCard(m.rows[i].card)
	}
}

// draftHeight is the rows the box needs: every line it holds at the width it is
// drawn in, never under draftRows and never taller than the pane.
func (m Model) draftHeight() int {
	width := m.draftWidth()

	// Floor and not ceiling: the cursor sits after the last character, so a line
	// filling the width exactly has already started the row under it.
	rows := 0
	for _, line := range strings.Split(m.draft.area.Value(), "\n") {
		rows += lipgloss.Width(line)/width + 1
	}
	// Its two borders, the line it hangs under, and the heading pinned over that.
	// A box past this loses its own footer to the clamp that keeps the line.
	return min(max(rows, draftRows), max(m.height-4, 1))
}

// anchorOf is the row a card hangs under, or its own first row when the diff
// has no line for it. It is where the cursor goes once the card is gone.
func (m Model) anchorOf(id string) int {
	for _, c := range m.cards {
		if c.id == id {
			if c.anchor >= 0 {
				return c.anchor
			}
			return c.at
		}
	}
	return -1
}

// draftWidth is what the box has to type in, which is what a card's prose gets.
func (m Model) draftWidth() int {
	_, width := m.cardBox()
	return max(width-2-2*cardGutter, 1)
}

// draftBody is the box as the card's rows, set in off the border the way prose
// is. A pane too narrow for a card never gets here.
func (m Model) draftBody() []string {
	gutter := strings.Repeat(" ", cardGutter)

	var out []string
	for _, line := range strings.Split(m.draft.area.View(), "\n") {
		out = append(out, gutter+line)
	}
	return out
}

// draftHints are the two keys the box answers to, where a card names its own.
// Nothing else on screen says how to get out of it.
func (m Model) draftHints(width int) string {
	line := "ctrl+s save · esc discard "
	if lipgloss.Width(line) > max(width-3, 0) {
		line = "ctrl+s save "
	}
	return lipgloss.NewStyle().Foreground(m.theme.Muted).Render(line)
}
