// Package compose is the box the reader types in: a textarea over the frame.
//
// It holds no session and makes no call. The root opens it, reads Value when
// the save key lands, and does the writing.
package compose

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// bodyRows is how tall the box gets on a terminal with the room for it. Past
// this a note is a document, and the frame behind it has gone.
const bodyRows = 8

// margin is what the box leaves around itself, so the frame under it still
// reads as a frame rather than as a border the box grew.
const margin = 4

// chrome is what a row spends on the pane's border and the gutter inside it.
const chrome = 4

// KeyMap is what the box answers to. Every other key is a keystroke in the body,
// the quit key included.
type KeyMap struct {
	Save    key.Binding
	Discard key.Binding
}

// NewKeyMap is the two bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		// enter is a newline in a body of prose, so the save key is not it. Raw
		// mode makes ctrl+s a key rather than the flow control a cooked one reads.
		Save:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		Discard: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "discard")),
	}
}

// Model is the box: a textarea, the frame it is placed in, and whether it is up.
type Model struct {
	Keys KeyMap

	theme theme.Theme
	area  textarea.Model
	title string
	open  bool

	width  int
	height int
}

// New builds a composer, closed, painted from the theme.
func New(t theme.Theme) Model {
	return Model{Keys: NewKeyMap(), theme: t, area: comp.Textarea(t)}
}

// Open puts the box up over body, titled and holding the keys. The cursor lands
// at the end of what was already there, which is where a note is added to.
func (m *Model) Open(title, body string) tea.Cmd {
	m.title, m.open = title, true
	m.area.SetValue(body)
	return m.area.Focus()
}

// Close takes the box down and drops what was typed in it.
func (m *Model) Close() {
	m.open = false
	m.area.Blur()
	m.area.Reset()
}

// Active is whether the box is up, which is whether it has the keys.
func (m Model) Active() bool { return m.open }

// Value is what has been typed.
func (m Model) Value() string { return m.area.Value() }

// SetSize fits the box to the frame it is placed in.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.area.SetWidth(max(min(width-margin-chrome, comp.BodyWidth), 1))
	m.area.SetHeight(max(min(height-margin-2, bodyRows), 1))
}

// Update takes a key to the body. The root answers the two the box owns and
// never gets here with one.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.area, cmd = m.area.Update(msg)
	return m, cmd
}

// View is the box, and empty when the composer is down. It is sized into the
// frame rather than left to overflow it, the way comp.Modal is.
func (m Model) View() string {
	if !m.open {
		return ""
	}

	body := lipgloss.NewStyle().Padding(0, 1).Render(m.area.View())
	w, h := lipgloss.Size(body)

	return comp.NewPane(m.theme).Title(m.title).Focus(true).
		Footer("", m.hints()).
		Size(min(w+2, m.width), min(h+2, m.height)).
		Render(body)
}

// TypingAt is where the terminal's cursor goes while the box is up, in the
// frame's coordinates: this box is placed over the frame rather than in a pane.
func (m Model) TypingAt() *tea.Cursor {
	if !m.open {
		return nil
	}
	c := m.area.Cursor()
	if c == nil {
		return nil
	}

	// Centred by Over, then past the pane's border and the padding on the body.
	w, h := lipgloss.Size(m.View())
	c.X += max(0, (m.width-w)/2) + 2
	c.Y += max(0, (m.height-h)/2) + 1

	// A frame with no room for the chrome puts it off the screen, and a cursor
	// there is one the terminal parks wherever it likes.
	if c.X >= m.width || c.Y >= m.height {
		return nil
	}
	return c
}

// hints are the two keys, in the bottom border where a pane keeps its counter.
// Nothing else on screen says how to get out of the box.
func (m Model) hints() string {
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	return muted.Render(hint(m.Keys.Save) + " · " + hint(m.Keys.Discard) + " ")
}

func hint(b key.Binding) string { return b.Help().Key + " " + b.Help().Desc }
