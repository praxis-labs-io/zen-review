package app

import (
	"charm.land/bubbles/v2/key"

	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

type KeyMap struct {
	NextHunk key.Binding
	PrevHunk key.Binding
	NextRead key.Binding
	PrevRead key.Binding
	NextFile key.Binding
	PrevFile key.Binding

	NextComment key.Binding
	PrevComment key.Binding

	Mark       key.Binding
	MarkFile   key.Binding
	Unmark     key.Binding
	UnmarkFile key.Binding

	Comment key.Binding
	Resolve key.Binding
	Edit    key.Binding
	Delete  key.Binding
	Expand  key.Binding
	Note    key.Binding

	Reload  key.Binding
	Base    key.Binding
	Split   key.Binding
	Preview key.Binding

	Left  key.Binding
	Right key.Binding
	Tree  key.Binding
	Diff  key.Binding
	Help  key.Binding
	Close key.Binding
	Quit  key.Binding

	Interrupt key.Binding
}

func NewKeyMap() KeyMap {
	return KeyMap{
		NextHunk: key.NewBinding(key.WithKeys("}"), key.WithHelp("}", "next hunk")),
		PrevHunk: key.NewBinding(key.WithKeys("{"), key.WithHelp("{", "previous hunk")),
		NextRead: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next unread")),
		PrevRead: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "previous unread")),
		NextFile: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next file")),
		PrevFile: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous file")),

		NextComment: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next comment")),
		PrevComment: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "previous comment")),

		Mark:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "mark read")),
		MarkFile:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "mark file")),
		Unmark:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unmark")),
		UnmarkFile: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "unmark file")),

		Comment: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
		Resolve: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "resolve comment")),

		Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit comment")),
		Delete: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete comment")),

		Expand: key.NewBinding(key.WithKeys(">")),

		Note: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "note")),

		Reload: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "reload")),
		Base:   key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "change base")),

		Split: key.NewBinding(key.WithKeys("|"), key.WithHelp("|", "split view")),

		Preview: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "full file")),

		Left:  key.NewBinding(key.WithKeys("h", "left")),
		Right: key.NewBinding(key.WithKeys("l", "right")),
		Tree:  key.NewBinding(key.WithKeys("1")),
		Diff:  key.NewBinding(key.WithKeys("2")),
		Help:  key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Close: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close help")),
		Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),

		Interrupt: key.NewBinding(key.WithKeys("ctrl+c")),
	}
}

func (m Model) ShortHelp() []key.Binding {
	if m.diff.Composing() {
		return m.composeKeys()
	}
	return append(m.paneKeys(), m.wayOut()...)
}

func (m Model) composeKeys() []key.Binding {
	return []key.Binding{m.compose.Keys.Save, m.compose.Keys.Discard}
}

func (m Model) paneKeys() []key.Binding {
	own := m.diff.Keys.Hints()
	if m.focus == focusTree {
		own = m.tree.Keys.Hints()
	}

	if m.diff.Selecting() {
		if m.focus == focusDiff {
			own = []key.Binding{comp.Pair(m.diff.Keys.Down, m.diff.Keys.Up, "j/k", "extend")}
		}
		return append(own, m.keys.Comment,
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")))
	}

	return append(own,
		comp.Pair(m.keys.NextRead, m.keys.PrevRead, "n/N", "unread"),
		comp.Pair(m.keys.NextHunk, m.keys.PrevHunk, "}/{", "hunk"),
		m.diff.Keys.Paging(),
		comp.Pair(m.keys.Mark, m.keys.MarkFile, "r/R", "read"),
		m.keys.Comment,
		m.keys.Reload,
		m.keys.Base,
		comp.Pair(m.keys.NextComment, m.keys.PrevComment, "]/[", "comment"),
		comp.Pair(m.keys.NextFile, m.keys.PrevFile, "tab", "file"),
		m.keys.Preview,
		m.keys.Split,
	)
}

func (m Model) markKeys() []key.Binding {
	return []key.Binding{
		m.keys.Mark, m.keys.MarkFile,
		comp.Pair(m.keys.Unmark, m.keys.UnmarkFile, "u/U", "unmark"),
	}
}

func (m Model) ringKeys() []key.Binding {
	return []key.Binding{
		m.keys.NextRead, m.keys.PrevRead,
		comp.Pair(m.keys.NextHunk, m.keys.PrevHunk, "}/{", "hunk"),
		m.keys.NextFile, m.keys.PrevFile,
	}
}

func (m Model) wayOut() []key.Binding {
	return []key.Binding{m.keys.Help, m.keys.Quit}
}

func (m Model) FullHelp() [][]key.Binding {
	panes := []key.Binding{
		comp.Pair(m.keys.Left, m.keys.Tree, "h/1", "tree pane"),
		comp.Pair(m.keys.Right, m.keys.Diff, "l/2", "diff pane"),
	}

	movement := m.diff.Keys.Bindings()
	if m.focus == focusTree {
		movement = m.tree.Keys.Movement.Bindings()
		panes = append(panes, m.tree.Keys.Bindings()...)
	}

	movement = append(movement, m.diff.Keys.Scrolling()...)

	movement = append(movement, comp.Pair(m.keys.NextComment, m.keys.PrevComment, "]/[", "comment"))

	if m.focus == focusDiff {
		movement = append(movement, m.diff.Keys.Place, m.diff.Keys.Select)
		movement = append(movement, m.diff.Keys.Cards()...)
	}

	verbs := append(m.markKeys(), m.keys.Comment, m.keys.Resolve,
		comp.Pair(m.keys.Edit, m.keys.Delete, "e/D", "edit, delete"))

	return [][]key.Binding{
		movement,
		append(m.ringKeys(), verbs...),
		append(panes, m.keys.Reload, m.keys.Base, m.keys.Preview, m.keys.Split,
			m.keys.Note, m.keys.Help, m.keys.Quit),
	}
}
