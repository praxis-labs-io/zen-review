package diffpane

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

// draftID cannot collide with a stored comment id, which is twelve hex characters.
const draftID = "draft"

const draftRows = 4

type draft struct {
	at   store.Comment
	area textarea.Model

	path string

	edits string
}

func (m Model) FitsBox() bool {
	_, width := m.cardBox()

	return m.file != nil && width >= cardMin && m.height >= draftRows+4
}

// Compose opens a box holding body for a new comment at c's anchor, returning its focus command. False when the pane cannot fit a box.
func (m *Model) Compose(c store.Comment, body string) (tea.Cmd, bool) {
	c.State = store.CommentOpen
	return m.open(c, body, "")
}

// Edit opens a box in place of comment c's card, holding its body. False when the pane cannot fit a box.
func (m *Model) Edit(c store.Comment) (tea.Cmd, bool) {
	return m.open(c, c.Body, c.ID)
}

func (m *Model) open(c store.Comment, body, edits string) (tea.Cmd, bool) {
	if !m.FitsBox() {
		return nil, false
	}
	c.ID, c.Body = draftID, ""

	area := comp.Textarea(m.theme)
	area.DynamicHeight = true
	area.MinHeight = draftRows

	area.SetWidth(m.draftWidth())
	area.SetValue(body)

	m.clearSelection()
	m.draft = &draft{at: c, area: area, path: m.file.Diff.Path, edits: edits}
	m.capBox()

	cmd := m.draft.area.Focus()
	m.layout()

	if at := m.rowAt(place{comment: draftID, seq: -1}); at >= 0 {
		m.point(at)
		m.showCard(m.rows[at].card)
	}
	return cmd, true
}

// Composing reports whether a box is up. While it is, Update sends every message to it.
func (m Model) Composing() bool { return m.draft != nil }

// TypingAt is the terminal cursor in pane coordinates, or nil with no box or with the box scrolled off.
func (m Model) TypingAt() *tea.Cursor {
	if m.draft == nil {
		return nil
	}

	c := m.draft.area.Cursor()
	at := m.rowAt(place{comment: draftID, seq: -1})
	if c == nil || at < 0 {
		return nil
	}

	left, _ := m.cardBox()
	c.X += left + 1 + cardGutter
	c.Y += at - m.offset + 1

	if c.Y < 0 || c.Y >= m.height || c.X >= m.width {
		return nil
	}
	return c
}

func (m Model) Draft() string {
	if m.draft == nil {
		return ""
	}
	return m.draft.area.Value()
}

// CloseDraft takes the box down, returning the cursor to the card it replaced or the line it hung under.
func (m *Model) CloseDraft() {
	if m.draft == nil {
		return
	}

	at, edits := m.anchorOf(draftID), m.draft.edits
	m.draft = nil
	m.layout()

	if edits != "" {
		m.SelectComment(edits)
		return
	}

	if at >= 0 {
		m.point(min(at, len(m.rows)-1))
		m.reveal()
	}
}

func (m *Model) typing(msg tea.Msg) tea.Cmd {
	was := m.draft.area.Height()

	var cmd tea.Cmd
	m.draft.area, cmd = m.draft.area.Update(msg)
	m.capBox()

	if m.draft.area.Height() != was {
		m.grew()
		return cmd
	}

	if at := m.rowAt(place{comment: draftID, seq: -1}); at >= 0 {
		i := m.rows[at].card
		m.cards[i].plain, m.cards[i].lit = m.drawCard(m.draft.at, nil, m.placementOf(m.cards[i].anchor))
		m.repaintCard(at)
	}
	return cmd
}

func (m *Model) grew() {
	at := place{comment: draftID, seq: -1}
	m.relayout(at)

	if i := m.rowAt(at); i >= 0 {
		m.showCard(m.rows[i].card)
	}
}

// capBox caps the height itself because textarea's MaxHeight also limits how many lines can be typed.
func (m *Model) capBox() {
	if room := max(m.height-4, draftRows); m.draft.area.Height() > room {
		m.draft.area.SetHeight(room)
	}
}

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

func (m Model) draftWidth() int {
	_, width := m.cardBox()
	return max(width-2-2*cardGutter, 1)
}

func (m Model) draftBody() []string {
	gutter := strings.Repeat(" ", cardGutter)

	var out []string
	for _, line := range strings.Split(m.draft.area.View(), "\n") {
		out = append(out, gutter+line)
	}
	return out
}

func (m Model) draftHints(width int) string {
	line := "ctrl+s save · esc discard "
	if lipgloss.Width(line) > max(width-3, 0) {
		line = "ctrl+s save "
	}
	return lipgloss.NewStyle().Foreground(m.theme.Muted).Render(line)
}
