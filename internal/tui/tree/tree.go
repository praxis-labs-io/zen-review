// Package tree is the left pane: the changeset's files under the directories
// that hold them, with how much of each has been read.
package tree

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

const (
	// indent is one level of nesting, in columns.
	indent = 2

	// barWidth is the cursor column and the space after it, which every row pays
	// for so the rows below the selected one do not shift sideways.
	barWidth = 2

	// nameMin is the columns a name keeps whatever else wants them. A row that
	// names no file names nothing.
	nameMin = 10
)

// OpenMsg says the reader picked the file under the cursor and wants to be
// reading it.
//
// It carries no path. The root reads Path off the model, which is where the
// answer already is, and a message that carries one can be delivered after the
// cursor has moved off the row it was about.
type OpenMsg struct{}

// KeyMap is what the tree answers to on top of the shared movement keys.
type KeyMap struct {
	comp.Movement

	// Toggle folds a directory. Open reads as "go there", which on a directory
	// is the same fold, so the two share the row and differ only on a file.
	Toggle key.Binding
	Open   key.Binding
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		Movement: comp.NewMovement(),
		Toggle:   key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "fold")),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open the file")),
	}
}

// Bindings is the tree's own keys, without the movement it shares.
func (k KeyMap) Bindings() []key.Binding {
	return []key.Binding{k.Toggle, k.Open}
}

// Model is the tree pane. It renders the changeset it was built from and holds
// no review state of its own.
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

// New builds the tree over a changeset. Its rows point into the changeset's
// files, so the caller has to keep them where they are.
func New(t theme.Theme, c review.Changeset) Model {
	m := Model{
		Keys:  NewKeyMap(),
		theme: t,
		roots: build(c.Files),
	}
	m.rows = flatten(m.roots, 0, nil)
	return m
}

// SetSize gives the pane the room it draws into, which is the inside of the
// frame and not the frame.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.scrollToCursor()
}

func (m *Model) Focus() { m.focused = true }
func (m *Model) Blur()  { m.focused = false }

// Path is the selected file's path, and is empty on a directory row.
func (m Model) Path() string {
	n := m.node()
	if n == nil || n.dir() {
		return ""
	}
	return n.path
}

// Select puts the cursor on a path, opening whatever directories hold it. It
// reports whether the path is in the changeset.
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
	case key.Matches(press, m.Keys.HalfDown):
		m.move(m.half())
	case key.Matches(press, m.Keys.HalfUp):
		m.move(-m.half())
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

// half is how far ctrl+u and ctrl+d go, and never zero on a pane too short to
// halve.
func (m Model) half() int {
	return max(m.height/2, 1)
}

// toggle folds the directory under the cursor. The cursor stays on it: folding
// only removes rows below, so the index it sits at still names the same row.
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

// reveal opens every directory above a path, so a selection made from outside
// the pane has a row to land on.
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

// scrollToCursor moves the window the least it can, which is right for a key
// that steps. A key that jumps says where it wants the row and moves the
// offset itself.
func (m *Model) scrollToCursor() {
	if m.height <= 0 {
		return
	}
	m.offset = min(m.offset, m.cursor)
	m.offset = max(m.offset, m.cursor-m.height+1)
	m.offset = max(0, min(m.offset, max(len(m.rows)-m.height, 0)))
}

func open() tea.Cmd {
	return func() tea.Msg { return OpenMsg{} }
}
