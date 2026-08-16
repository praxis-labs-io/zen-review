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

// draftRows is how tall the box gets. It is a note hanging off a line, and the
// code it is about is what the rest of the pane is for.
const draftRows = 4

// draft is the comment being typed: where it will land, and the box it is typed
// in. The pane draws it as the card it is about to become.
type draft struct {
	at   store.Comment
	area textarea.Model

	// path is the file the box was opened over. A reload landing under it can
	// put another file in the pane, and the box belongs to neither.
	path string
}

// Compose opens a box where the comment will hang, and false for a pane with no
// room to draw one: an unseen box holding every key is a reader typing at air.
func (m *Model) Compose(c store.Comment) (tea.Cmd, bool) {
	if _, width := m.cardBox(); m.file == nil || width < cardMin || m.height < draftRows+2 {
		return nil, false
	}
	c.ID, c.State, c.Body = draftID, store.CommentOpen, ""

	area := comp.Textarea(m.theme)
	area.SetHeight(draftRows)
	area.SetWidth(m.draftWidth())

	m.clearSelection()
	m.draft = &draft{at: c, area: area, path: m.file.Diff.Path}
	m.layout()

	if at := m.rowAt(place{comment: draftID, seq: -1}); at >= 0 {
		m.point(at)
		m.showCard(m.rows[at].card)
	}
	return m.draft.area.Focus(), true
}

// Composing is whether a box is up, which is whether the pane has the keys.
func (m Model) Composing() bool { return m.draft != nil }

// Draft is what has been typed, and empty when no box is up.
func (m Model) Draft() string {
	if m.draft == nil {
		return ""
	}
	return m.draft.area.Value()
}

// CloseDraft takes the box down and leaves the cursor on the code it hung off.
func (m *Model) CloseDraft() {
	if m.draft == nil {
		return
	}

	at := m.anchorOf(draftID)
	m.draft = nil
	m.layout()

	if at >= 0 {
		m.point(min(at, len(m.rows)-1))
		m.reveal()
	}
}

// typing takes a key to the body and redraws the box in place. Nothing else on
// screen moves, so only the box's own rows are repainted.
func (m *Model) typing(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.draft.area, cmd = m.draft.area.Update(msg)

	if at := m.rowAt(place{comment: draftID, seq: -1}); at >= 0 {
		i := m.rows[at].card
		m.cards[i].plain, m.cards[i].lit = m.drawCard(m.draft.at, m.cards[i].anchor >= 0)
		m.repaintCard(at)
	}
	return cmd
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
