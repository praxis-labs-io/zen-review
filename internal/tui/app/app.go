// Package app is the root model: two panes, a status bar, and the keys that
// move between them.
//
// It routes and it lays out. Every state change is a call into review, and the
// model renders what review returned rather than its own guess at what the
// call did.
package app

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
	"github.com/zen-review/zen-review/internal/tui/compose"
	"github.com/zen-review/zen-review/internal/tui/diffpane"
	"github.com/zen-review/zen-review/internal/tui/tree"
)

// focus is the pane the keys are pointed at.
type focus int

const (
	focusTree focus = iota
	focusDiff
)

// Model is the whole screen.
type Model struct {
	keys  KeyMap
	theme theme.Theme

	// src is what the reload and mark keys reach. It is called off the update
	// loop, and busy is what keeps two calls from being in flight at once.
	src  Source
	busy bool

	repo      string
	base      review.Base
	gen       review.Generation
	changeset review.Changeset

	// comments are every comment of the session, read at the generation the
	// changeset was derived at. The diff pane draws the ones on the file it holds.
	comments []store.Comment

	// summary is the session note. Nothing draws it: C opens the composer over
	// it, and the report is where it is read back.
	summary string

	// note is what the last reload found, until the next key clears it.
	note notice

	tree    tree.Model
	diff    diffpane.Model
	help    help.Model
	compose compose.Model

	// The frames the two panes are drawn in. They hold the size, so the model
	// asks them what is left inside rather than subtracting the border twice.
	treePane comp.Pane
	diffPane comp.Pane

	// cursor is the hunk the ring is on, held as an identity and resolved to a
	// position on every move. The diff pane draws it; nothing else owns it.
	cursor stop

	focus   focus
	showing bool

	width  int
	height int
}

// New builds the screen over a changeset, open on the first hunk of it that has
// not been read, with the diff pane holding the keys.
//
// zen-octo's conversation opens unfocused because the reader came to read. You
// came here to burn a review down, and the first thing to press is a ring key.
//
// The changeset is held by value and the panes point into its files, so it
// must not be appended to after this. A reload replaces it wholesale and
// re-points both panes rather than growing the one they hold.
func New(t theme.Theme, src Source, repo string, r Reload) Model {
	m := Model{
		keys:      NewKeyMap(),
		theme:     t,
		src:       src,
		repo:      repo,
		base:      r.Base,
		gen:       r.Generation,
		changeset: r.Changeset,
		comments:  r.Comments,
		summary:   r.Summary,
		tree:      tree.New(t, r.Changeset),
		diff:      diffpane.New(t),
		help:      comp.Help(t),
		compose:   compose.New(t),
		treePane:  comp.NewPane(t),
		diffPane:  comp.NewPane(t),
	}
	m.setFocus(focusDiff)

	if s, ok := m.opening(); ok {
		m.land(s)
	}
	return m
}

// Run opens the reader on the terminal and returns when it closes.
//
// It can return with a reload still in git. Bubble Tea does not wait for a
// command it started, so a caller holding the session has to before it lets go
// of it.
func Run(ctx context.Context, t theme.Theme, src Source, repo string, r Reload) error {
	if _, err := tea.NewProgram(New(t, src, repo, r), tea.WithContext(ctx)).Run(); err != nil {
		return fmt.Errorf("running the reader: %w", err)
	}
	return nil
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tree.OpenMsg:
		m.setFocus(focusDiff)
		return m, nil

	case reloadedMsg:
		m.busy = false
		was, d, moved := m.apply(msg.r)
		m.note = said(was, d, m.gen.Seq, moved)
		return m, nil

	case wroteMsg:
		m.busy = false
		m.applyWrite(msg)
		return m, nil

	case resolvedMsg:
		m.busy = false
		m.apply(msg.r)
		m.note = notice{text: m.answered()}
		return m, nil

	case notedMsg:
		m.busy = false
		m.summary = msg.text
		m.note = noted(msg.text)

		// Down here and not at the key, so a write that failed leaves the box up
		// holding the words. Typing on while it was out keeps it up too.
		if m.compose.Value() == msg.text {
			m.compose.Close()
		}
		return m, nil

	case staleMsg:
		m.busy = false
		m.note = notice{text: msg.err.Error() + ": press s", bad: true}
		return m, nil

	case writeFailedMsg:
		m.busy = false
		m.note = notice{text: msg.err.Error(), bad: true}
		return m, nil

	case reloadFailedMsg:
		// The changeset on screen is left alone. These writes are a local
		// transaction that committed or did not, and there is no half-applied
		// state to paint over.
		m.busy = false
		m.note = notice{text: msg.err.Error(), bad: true}
		return m, nil

	case tea.KeyPressMsg:
		return m.press(msg)
	}

	// A paste arrives as a message of its own rather than as keys, so a box
	// routed by press alone would silently drop one.
	if m.compose.Active() {
		var cmd tea.Cmd
		m.compose, cmd = m.compose.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) press(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The box takes every key, quit and help included. A q mid-sentence is a q,
	// and one key let out is one more thing to keep in mind while typing.
	if m.compose.Active() {
		return m.typing(msg)
	}

	// One press long, so it goes before the press that ends it is read. A reload
	// still in git is the exception: the press did not end that, and the bar is
	// the only thing saying it is happening.
	if !m.busy {
		m.note = notice{}
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.showing = !m.showing
		return m, nil
	}

	// The overlay covers the panes, so it takes the keys too. Routing them
	// underneath would scroll a pane the reader cannot see.
	if m.showing {
		if key.Matches(msg, m.keys.Close) {
			m.showing = false
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Left):
		m.setFocus(focusTree)
		return m, nil
	case key.Matches(msg, m.keys.Right):
		m.setFocus(focusDiff)
		return m, nil
	}

	// The reload runs off the update loop, one at a time. Two refreshes on one
	// session race the ref swap against itself, and the loser of that race is a
	// generation the database never hears about.
	//
	// The notice is set either way, so the second press does not blank the bar
	// while the first is still in git.
	if key.Matches(msg, m.keys.Reload) {
		if m.busy {
			return m, nil
		}
		m.note = notice{text: "reloading"}
		m.busy = true
		return m, m.reload()
	}

	// The mark keys go through the same one-at-a-time gate. A write is a local
	// transaction, but the source it goes through may be held by a refresh.
	if i, mine := m.marked(msg); mine {
		// The note goes up either way, so a press refused while the last one is
		// still in git leaves the bar saying what is happening rather than blank.
		// A refused press loses nothing: nothing was written and the cursor did
		// not move, so the next press acts on the same hunk.
		m.note = notice{text: "marking"}
		if m.busy {
			return m, nil
		}
		cmd, ok := m.start(i)
		if !ok {
			return m, nil
		}
		return m, cmd
	}

	// The note is the session's rather than a hunk's, so it sits with the reload
	// rather than in a pane. Opening it writes nothing and waits on nothing.
	if key.Matches(msg, m.keys.Note) {
		// Called before the return rather than in it: the order of a plain
		// operand against a call beside it is the spec's to choose, not ours.
		cmd := m.composing()
		return m, cmd
	}

	// A press with nothing to settle under it is most presses of this key, and it
	// has nothing to act on rather than something to refuse.
	if key.Matches(msg, m.keys.Resolve) {
		id, on := m.settling()
		if !on {
			return m, nil
		}
		m.note = notice{text: "resolving"}
		if m.busy {
			return m, nil
		}
		cmd := m.resolve(id)
		return m, cmd
	}

	// The comment ring crosses files the way the hunk ring does, so it is routed
	// here rather than in the pane that holds the cards.
	if by, mine := m.stepping(msg); mine {
		if c, ok := m.commentRing(by); ok {
			m.landComment(c)
		}
		return m, nil
	}

	// The ring answers from either pane and moves both, so it is routed before
	// the focus is. A key that found nowhere to go leaves everything alone: n on
	// a changeset with nothing left unread is a press that has done its job.
	if s, ok, moved := m.walk(msg); moved {
		if ok {
			m.land(s)
		}
		return m, nil
	}

	var cmd tea.Cmd

	// The half-page keys page the diff from either pane, so they are routed
	// before the focus is. Walking the tree is how the reader gets to a file;
	// reading it is what they came for, and the pane they are reading is the one
	// worth paging.
	if key.Matches(msg, m.diff.Keys.Scrolling()...) {
		m.diff, cmd = m.diff.Update(msg)
		m.syncCursor()
		return m, cmd
	}

	switch m.focus {
	case focusTree:
		m.tree, cmd = m.tree.Update(msg)
		m.syncDiff()
	case focusDiff:
		m.diff, cmd = m.diff.Update(msg)
		m.syncCursor()
	}
	return m, cmd
}

// typing routes a key into the composer, answering the two it owns first. The
// box stays up when the save is refused, or the press would lose what was typed.
func (m Model) typing(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	// The way out of anywhere. Raw mode sends no interrupt, so without this the
	// box is the one place in the program where ctrl+c does nothing at all.
	case key.Matches(msg, m.keys.Interrupt):
		return m, tea.Quit

	case key.Matches(msg, m.compose.Keys.Discard):
		m.compose.Close()
		return m, nil

	case key.Matches(msg, m.compose.Keys.Save):
		// Neutral, because busy covers a mark and a resolve as well as a reload.
		if m.busy {
			m.note = notice{text: "still writing"}
			return m, nil
		}

		cmd := m.saveNote(m.compose.Value())
		return m, cmd
	}

	var cmd tea.Cmd
	m.compose, cmd = m.compose.Update(msg)
	return m, cmd
}

// syncCursor puts the ring on the hunk the diff pane's cursor is in, so a mark
// takes the hunk the reader is looking at. It reads the pane, as syncDiff does.
func (m *Model) syncCursor() {
	side, line, ok := m.diff.Hunk()
	if !ok {
		return
	}

	want := stop{path: m.diff.Path(), side: side, line: line}
	for _, s := range m.stops() {
		if s.same(want) {
			m.cursor = s
			return
		}
	}
}

// stepping is the direction a comment key asks for, and whether the press was
// one at all.
func (m Model) stepping(msg tea.KeyPressMsg) (by int, mine bool) {
	switch {
	case key.Matches(msg, m.keys.NextComment):
		return 1, true
	case key.Matches(msg, m.keys.PrevComment):
		return -1, true
	}
	return 0, false
}

// walk is the stop a ring key asks for. The third value says whether the press
// was a ring key at all, which is what tells "nowhere to go" from "not mine".
func (m Model) walk(msg tea.KeyPressMsg) (s stop, ok, mine bool) {
	switch {
	case key.Matches(msg, m.keys.NextHunk):
		s, ok = m.ring(1, anyStop)
	case key.Matches(msg, m.keys.PrevHunk):
		s, ok = m.ring(-1, anyStop)
	case key.Matches(msg, m.keys.NextRead):
		s, ok = m.ring(1, unreadStop)
	case key.Matches(msg, m.keys.PrevRead):
		s, ok = m.ring(-1, unreadStop)
	case key.Matches(msg, m.keys.NextFile):
		s, ok = m.file(1)
	case key.Matches(msg, m.keys.PrevFile):
		s, ok = m.file(-1)
	default:
		return stop{}, false, false
	}
	return s, ok, true
}

// syncDiff points the diff pane at whatever the tree is on.
//
// The tree does not send the path in a message. Bubble Tea runs the commands a
// model returns concurrently, so two of them raced by a held-down j can land
// out of order and leave the pane on a file the cursor has already left. The
// path is on the model, and reading it needs no message at all.
//
// A directory row leaves the pane alone. Blanking it on the way past one would
// punish walking the tree.
//
// The ring goes to the file's first hunk, not its first unread one. The reader
// picked the file to read it, and n is the key that walks the burn-down.
//
// A cursor already on the file in the pane has not moved onto it, so the ring
// stays where it is. Pressing enter on the file being read is not a move, and
// putting the ring back at the top would throw away where the reader had got to.
func (m *Model) syncDiff() {
	path := m.tree.Path()
	if path == "" || path == m.diff.Path() {
		return
	}

	m.diff.SetFile(m.fileAt(path), m.comments, m.gen.ID)
	if s, ok := m.firstOf(path); ok {
		m.cursor = s
		m.diff.Select(s.side, s.line)
	}
}

// setFocus points the keys at a pane.
//
// Only the tree is told. The diff pane draws its cursor whichever pane has the
// keys, because the ring moves it from both and a mark that came and went with
// focus would leave the reader hunting for where n put them.
func (m *Model) setFocus(f focus) {
	m.focus = f
	if f == focusTree {
		m.tree.Focus()
		return
	}
	m.tree.Blur()
}

// fileAt resolves a path to the file the panes share. It returns nil for a
// path the changeset does not hold, which empties the diff pane rather than
// leaving it showing a file nobody asked for.
func (m *Model) fileAt(path string) *review.File {
	for i := range m.changeset.Files {
		if m.changeset.Files[i].Diff.Path == path {
			return &m.changeset.Files[i]
		}
	}
	return nil
}
