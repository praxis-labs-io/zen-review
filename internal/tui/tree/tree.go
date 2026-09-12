// Package tree is the file pane: changeset files under their directories, with review progress.
package tree

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

const (
	indent = 2

	gutter = 1

	topPad    = 1
	bottomPad = 1

	nameMin = 10
)

// OpenMsg asks the root to open the file under the cursor. It carries no path, which could
// arrive stale; read Path.
type OpenMsg struct{}

type KeyMap struct {
	comp.Movement

	Toggle key.Binding
	Open   key.Binding
}

func NewKeyMap() KeyMap {
	return KeyMap{
		Movement: comp.NewMovement(),
		Toggle:   key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "fold")),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	}
}

// Bindings returns the tree's own keys, without the shared movement.
func (k KeyMap) Bindings() []key.Binding {
	return []key.Binding{k.Toggle, k.Open}
}

// Hints returns the keys the status bar names while the tree has focus, most useful first.
func (k KeyMap) Hints() []key.Binding {
	return []key.Binding{
		comp.Pair(k.Down, k.Up, "j/k", "move"),
		k.Open,
		k.Toggle,
	}
}

type Model struct {
	Keys KeyMap

	theme theme.Theme

	roots []*node
	rows  []row

	cursor int
	offset int

	width   int
	height  int
	focused bool
}

// New builds the tree over c. Rows point into c's files, which must outlive the model.
func New(t theme.Theme, c review.Changeset) Model {
	m := Model{
		Keys:  NewKeyMap(),
		theme: t,
		roots: build(c.Files),
	}
	m.rows = flatten(m.roots, 0, nil)
	return m
}

// SetChangeset rebuilds over a new generation, keeping folds and the cursor's file if it survives.
// Rows point into c's files, as with New.
func (m *Model) SetChangeset(c review.Changeset) {
	was := m.Path()

	shut := make(map[string]bool)
	folded(m.roots, shut)

	m.roots = build(c.Files)
	refold(m.roots, shut)
	m.rows = flatten(m.roots, 0, nil)

	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	if was == "" || !m.Select(was) {
		m.scrollToCursor()
	}
}

// SetSize sets the area inside the frame the pane draws into.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.scrollToCursor()
}

func (m *Model) Focus() { m.focused = true }
func (m *Model) Blur()  { m.focused = false }

func (m Model) total() int { return len(m.rows) + topPad + bottomPad }

func (m Model) maxOffset() int { return max(m.total()-m.height, 0) }

// First returns the path of the first file row, or "" when there are no files.
func (m Model) First() string {
	for _, r := range m.rows {
		if !r.n.dir() {
			return r.n.path
		}
	}
	return ""
}

// Path returns the selected file's path, or "" on a directory row.
func (m Model) Path() string {
	n := m.node()
	if n == nil || n.dir() {
		return ""
	}
	return n.path
}

// Select moves the cursor to path, unfolding its directories. Reports whether path is present.
func (m *Model) Select(path string) bool {
	if !m.reveal(m.roots, path) {
		return false
	}
	m.rows = flatten(m.roots, 0, nil)

	for i, r := range m.rows {
		if !r.n.dir() && r.n.path == path {
			m.cursor = i
			m.scrollToCursor()
			return true
		}
	}
	return false
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch {
	case key.Matches(press, m.Keys.Down):
		m.move(1)
	case key.Matches(press, m.Keys.Up):
		m.move(-1)
	case key.Matches(press, m.Keys.Top):
		m.move(-len(m.rows))
	case key.Matches(press, m.Keys.Bottom):
		m.move(len(m.rows))

	case key.Matches(press, m.Keys.Toggle):
		m.toggle()
		return m, nil

	case key.Matches(press, m.Keys.Open):
		if n := m.node(); n != nil && n.dir() {
			m.toggle()
			return m, nil
		}
		if m.Path() != "" {
			return m, open()
		}
	}
	return m, nil
}

func (m Model) node() *node {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].n
}

func (m *Model) move(by int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+by, 0), len(m.rows)-1)
	m.scrollToCursor()
}

func (m *Model) toggle() {
	n := m.node()
	if n == nil || !n.dir() {
		return
	}
	n.open = !n.open
	m.rows = flatten(m.roots, 0, nil)
	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	m.scrollToCursor()
}

func (m *Model) reveal(nodes []*node, path string) bool {
	for _, n := range nodes {
		if !n.dir() {
			if n.path == path {
				return true
			}
			continue
		}
		if !m.reveal(n.kids, path) {
			continue
		}
		n.open = true
		return true
	}
	return false
}

// scrollToCursor takes the shortest scroll, which suits only a key that steps one row.
func (m *Model) scrollToCursor() {
	if m.height <= 0 {
		return
	}

	at := m.cursor + topPad
	m.offset = min(m.offset, at)
	m.offset = max(m.offset, at-m.height+1)

	if m.height > 1 {
		switch {
		case m.cursor == 0:
			m.offset = 0
		case m.cursor == len(m.rows)-1:
			m.offset = m.maxOffset()
		}
	}
	m.offset = max(0, min(m.offset, m.maxOffset()))
}

func open() tea.Cmd {
	return func() tea.Msg { return OpenMsg{} }
}
