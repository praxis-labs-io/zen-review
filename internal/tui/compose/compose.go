// Package compose is the note box over the frame. It writes nothing; the root reads Value.
package compose

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

const bodyRows = 8

const margin = 4

const chrome = 4

type KeyMap struct {
	Save    key.Binding
	Discard key.Binding
}

func NewKeyMap() KeyMap {
	return KeyMap{
		Save:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		Discard: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "discard")),
	}
}

type Model struct {
	Keys KeyMap

	theme theme.Theme
	area  textarea.Model
	title string
	open  bool

	width  int
	height int
}

func New(t theme.Theme) Model {
	return Model{Keys: NewKeyMap(), theme: t, area: comp.Textarea(t)}
}

// Open shows the box titled title, holding body with the cursor at its end, and focuses it.
func (m *Model) Open(title, body string) tea.Cmd {
	m.title, m.open = title, true
	m.area.SetValue(body)
	return m.area.Focus()
}

// Close hides the box and discards what was typed.
func (m *Model) Close() {
	m.open = false
	m.area.Blur()
	m.area.Reset()
}

// Active reports whether the box is up, and so holds the keys.
func (m Model) Active() bool { return m.open }

func (m Model) Value() string { return m.area.Value() }

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.area.SetWidth(max(min(width-margin-chrome, comp.BodyWidth), 1))
	m.area.SetHeight(max(min(height-margin-2, bodyRows), 1))
}

// Update passes msg to the body. The root handles Save and Discard before calling it.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.area, cmd = m.area.Update(msg)
	return m, cmd
}

// View renders the box sized into the frame, or "" when it is closed.
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

// TypingAt is the terminal cursor's position in frame coordinates, or nil when closed or off it.
func (m Model) TypingAt() *tea.Cursor {
	if !m.open {
		return nil
	}
	c := m.area.Cursor()
	if c == nil {
		return nil
	}

	w, h := lipgloss.Size(m.View())
	c.X += max(0, (m.width-w)/2) + 2
	c.Y += max(0, (m.height-h)/2) + 1

	if c.X >= m.width || c.Y >= m.height {
		return nil
	}
	return c
}

func (m Model) hints() string {
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	return muted.Render(hint(m.Keys.Save) + " · " + hint(m.Keys.Discard) + " ")
}

func hint(b key.Binding) string { return b.Help().Key + " " + b.Help().Desc }
