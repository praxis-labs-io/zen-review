package app

import (
	"charm.land/bubbles/v2/key"

	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

// KeyMap is what the root answers to, whichever pane has focus.
//
// The ring is here rather than on the diff pane because it crosses files: it
// moves the tree's selection as well as the diff's cursor, and a pane reaching
// into its sibling is the thing the layout rules forbid. It also answers from
// either pane, the same as the paging keys.
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
	Note    key.Binding

	Reload key.Binding
	Base   key.Binding

	Left  key.Binding
	Right key.Binding
	Help  key.Binding
	Close key.Binding
	Quit  key.Binding

	// Interrupt is the half of Quit that reaches out of the composer. q is a
	// letter while a body is being typed and ctrl+c is never one.
	Interrupt key.Binding
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		// A hunk is the block a paragraph motion moves by, which is what } and {
		// do everywhere else. Vim's own diff mode says ]c and [c, and the bracket
		// pair is spoken for: ] and [ are next and previous comment.
		//
		// Nothing in vim moves a whole file in one key. tab is the TUI answer
		// rather than the editor one, and the tree does the same job by hand.
		NextHunk: key.NewBinding(key.WithKeys("}"), key.WithHelp("}", "next hunk")),
		PrevHunk: key.NewBinding(key.WithKeys("{"), key.WithHelp("{", "previous hunk")),
		NextRead: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next unread")),
		PrevRead: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "previous unread")),
		NextFile: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next file")),
		PrevFile: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous file")),

		// They walk what is unresolved, the way n walks what is unread. A ring
		// stepping through settled work is a ring nobody holds down.
		NextComment: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next comment")),
		PrevComment: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "previous comment")),

		// r advances after marking, so r r r r walks the whole thing. It does not
		// toggle: advancing off a hunk just unmarked is a key with two jobs.
		Mark:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "mark read")),
		MarkFile:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "mark file")),
		Unmark:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unmark")),
		UnmarkFile: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "unmark file")),

		// c writes one and x settles it, from the card's own footer: that is where
		// a key reaching one row on the screen belongs. C is the session's note.
		Comment: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
		Resolve: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "resolve comment")),

		// e opens the box holding what the card says, and D takes the card away at
		// once: the capital does the whole of the thing, the way R and U do.
		Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit comment")),
		Delete: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete comment")),

		Note: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "note")),

		// These session actions reach past the screen to the repository.
		Reload: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "reload")),
		Base:   key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "change base")),

		// The digits are the badges the panes carry in their borders. They join
		// the bindings that already move focus rather than being declared beside
		// them, so the help lists one entry per pane.
		Left:  key.NewBinding(key.WithKeys("h", "left", "1"), key.WithHelp("h/1", "tree pane")),
		Right: key.NewBinding(key.WithKeys("l", "right", "2"), key.WithHelp("l/2", "diff pane")),
		Help:  key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Close: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close help")),
		Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),

		// No help of its own. Quit's entry names the key a reader looks for, and
		// this one only exists to get out of a box that eats every other key.
		Interrupt: key.NewBinding(key.WithKeys("ctrl+c")),
	}
}

// ShortHelp is the line on the status bar: what the pane holding the keys can
// do, then the two that answer from anywhere.
//
// It changes with focus, because the point of the line is what the next press
// would do. The rest is one keypress away.
func (m Model) ShortHelp() []key.Binding {
	if m.diff.Composing() {
		return m.composeKeys()
	}
	return append(m.paneKeys(), m.wayOut()...)
}

// composeKeys is what the bar carries while the box in the pane is up: its two,
// and no way out, because q and ? are keystrokes in a body being typed.
func (m Model) composeKeys() []key.Binding {
	return []key.Binding{m.compose.Keys.Save, m.compose.Keys.Discard}
}

// paneKeys is what the pane holding the keys can do, then what the ring can do
// from either. The bar drops from the tail, so the last of these is the first
// to go.
//
// n comes before the rest of the ring because a review is a burn-down and n is
// the key held until the count reaches zero.
//
// The file keys are last, so a hundred columns drops them and a wider terminal
// keeps them. They are the ring's least-used pair and the only one whose job the
// tree beside it already does. The reload goes above them and below the paging
// keys, which are pressed far more often.
//
// A hundred columns with the tree focused drops the reload too, so the overlay
// is where a reader on that frame finds it. The two keys the bar never drops
// are the way out, and a third would take the room the ring needs at
// fifty-six.
func (m Model) paneKeys() []key.Binding {
	own := m.diff.Keys.Hints()
	if m.focus == focusTree {
		own = m.tree.Keys.Hints()
	}

	// A selection is the one thing on screen with an end to it, so the bar names
	// the keys that end it. j extends only in the pane the selection is drawn in.
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
	)
}

// markKeys is what changes what has been read, in the order the overlay lists
// them. The undo pair is one entry: this column is the tallest and has to fit.
func (m Model) markKeys() []key.Binding {
	return []key.Binding{
		m.keys.Mark, m.keys.MarkFile,
		comp.Pair(m.keys.Unmark, m.keys.UnmarkFile, "u/U", "unmark"),
	}
}

// ringKeys is what the ring answers to, in the order the overlay lists them. The
// hunk pair is one entry the way ]/[ is: this column is the tallest and has to fit.
func (m Model) ringKeys() []key.Binding {
	return []key.Binding{
		// n and N keep a row each, being the pair a burn-down is held down on, and
		// the file pair keeps two: tab/shift+tab as one label is wider than the rest.
		m.keys.NextRead, m.keys.PrevRead,
		comp.Pair(m.keys.NextHunk, m.keys.PrevHunk, "}/{", "hunk"),
		m.keys.NextFile, m.keys.PrevFile,
	}
}

// wayOut is the two the bar never drops. They are the only thing on screen
// saying the overlay exists, and the reader who needs that is the one on the
// terminal too narrow for the rest.
func (m Model) wayOut() []key.Binding {
	return []key.Binding{m.keys.Help, m.keys.Quit}
}

// FullHelp is the overlay, in three columns: a fourth clips against the eighty
// cells it fits in. Its bindings come off the pane that would match them.
func (m Model) FullHelp() [][]key.Binding {
	panes := []key.Binding{m.keys.Left, m.keys.Right}

	// The diff pane adds nothing to the pane column: everything it answers to is
	// movement. The tree has keys of its own.
	movement := m.diff.Keys.Bindings()
	if m.focus == focusTree {
		movement = m.tree.Keys.Movement.Bindings()
		panes = append(panes, m.tree.Keys.Bindings()...)
	}

	// The half-page keys are listed under whichever pane has the keys, because
	// they answer from both.
	movement = append(movement, m.diff.Keys.Scrolling()...)

	// The comment ring only moves, where the ring column acts on what it lands
	// on. One entry, because this column is the tallest at sixteen rows.
	movement = append(movement, comp.Pair(m.keys.NextComment, m.keys.PrevComment, "]/[", "comment"))

	// The z keys move the window under the cursor and the card keys act on the
	// card it is on, so both reach one pane and are listed only where they work.
	if m.focus == focusDiff {
		movement = append(movement, m.diff.Keys.Place, m.diff.Keys.Select)
		movement = append(movement, m.diff.Keys.Cards()...)
	}

	// These verbs act from either pane, so they sit beside the shared ring.
	verbs := append(m.markKeys(), m.keys.Comment, m.keys.Resolve,
		comp.Pair(m.keys.Edit, m.keys.Delete, "e/D", "edit, delete"))

	return [][]key.Binding{
		movement,
		append(m.ringKeys(), verbs...),
		append(panes, m.keys.Reload, m.keys.Base, m.keys.Note, m.keys.Help, m.keys.Quit),
	}
}
