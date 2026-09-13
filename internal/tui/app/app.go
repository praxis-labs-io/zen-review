// Package app is the reader's root model, routing keys between the panes.
package app

import (
	"context"
	"fmt"
	"os"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/compose"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
	"github.com/praxis-labs-io/zen-review/internal/tui/tree"
)

type focus int

const (
	focusTree focus = iota
	focusDiff
)

type Model struct {
	keys  KeyMap
	theme theme.Theme

	src  Source
	busy bool

	// reading is separate from busy because a read neither blocks a write nor is blocked by one.
	reading string

	repo      string
	base      review.Base
	gen       review.Generation
	changeset review.Changeset

	comments []store.Comment

	replaced map[string][]string

	summary string

	pending review.Note

	editing string

	stranded string

	note notice

	tree    tree.Model
	diff    diffpane.Model
	help    help.Model
	compose compose.Model
	picker  basePicker

	treePane comp.Pane
	diffPane comp.Pane

	cursor stop

	focus   focus
	showing bool

	width  int
	height int
}

// New builds the screen over r, focused on the diff pane at the first unread hunk.
// The panes point into r.Changeset's files, so it must not be appended to afterwards.
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
		replaced:  r.Replaced,
		summary:   r.Summary,
		tree:      tree.New(t, r.Changeset),
		diff:      diffpane.New(t),
		help:      comp.Help(t),
		compose:   compose.New(t),
		picker:    newBasePicker(t),
		treePane:  comp.NewPane(t),
		diffPane:  comp.NewPane(t),
	}
	m.setFocus(focusDiff)

	if s, ok := m.opening(); ok {
		m.land(s)
	}
	return m
}

// Run opens the reader on the terminal and returns when it closes. It can return
// with a Source call still running, so the caller must wait before releasing the session.
func Run(ctx context.Context, src Source, repo string, r Reload) error {
	t := theme.Terminal(theme.Query(os.Stdin, os.Stdout))

	if _, err := tea.NewProgram(New(t, src, repo, r), tea.WithContext(ctx)).Run(); err != nil {
		return fmt.Errorf("running the reader: %w", err)
	}
	return nil
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	cmd = next.asking(cmd)
	return next, cmd
}

// asking wraps all of update because the pane's file changes on reloads and writes, not only on keys.
func (m *Model) asking(cmd tea.Cmd) tea.Cmd {
	path, want := m.diff.NeedsBody()
	if !want || path == m.reading {
		return cmd
	}
	m.reading = path
	return tea.Batch(cmd, m.loadBody(path))
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

		cmd := m.crossOver()
		return m, cmd

	case tree.OpenMsg:
		m.setFocus(focusDiff)
		return m, nil

	case reloadedMsg:
		m.busy = false
		was, d, moved := m.apply(msg.r)
		m.note = said(was, d, m.gen.Seq, moved)
		return m, nil

	case basesLoadedMsg:
		m.busy = false
		cmd := m.picker.open(msg.candidates, m.base.Ref)
		return m, cmd

	case basesFailedMsg:
		m.busy = false
		m.note = notice{text: msg.err.Error(), bad: true}
		return m, nil

	case baseSetMsg:
		m.busy = false
		m.apply(msg.r)
		m.picker.close()
		m.note = notice{text: "base changed to " + msg.ref}
		return m, nil

	case baseSetFailedMsg:
		m.busy = false
		m.picker.err = msg.err.Error()
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

		if m.compose.Value() == msg.text {
			m.shut()
		}
		return m, nil

	case commentedMsg:
		m.busy = false
		m.apply(msg.r)
		m.note = notice{text: "comment saved"}

		m.shut()
		return m, nil

	case reanchoredMsg:
		m.busy = false
		cmd := m.reopen(msg)
		return m, cmd

	case anchorGoneMsg:
		m.busy = false
		m.strand(msg)
		return m, nil

	case reanchorFailedMsg:
		m.busy = false
		m.note = notice{text: msg.err.Error() + ": ctrl+s tries again", bad: true}
		return m, nil

	case editedMsg:
		m.busy = false
		m.apply(msg.r)
		m.note = notice{text: "comment updated"}
		m.shut()
		return m, nil

	case deletedMsg:
		m.busy = false
		m.apply(msg.r)

		m.diff.LeaveCard()
		m.note = notice{text: "comment deleted"}
		return m, nil

	case savedMsg:
		m.busy = false
		m.shut()
		m.note = notice{text: msg.err.Error() + ": press s", bad: true}
		return m, nil

	case staleMsg:
		m.busy = false
		m.note = notice{text: msg.err.Error() + ": press s", bad: true}
		return m, nil

	case writeFailedMsg:
		m.busy = false
		m.note = notice{text: msg.err.Error(), bad: true}
		return m, nil

	case bodyLoadedMsg:
		if msg.path == m.reading {
			m.reading = ""
		}

		if msg.gen != m.gen.ID {
			return m, nil
		}
		if !m.diff.SetBody(msg.path, msg.body) {
			m.note = notice{text: "no lines to show in " + comp.Safe(msg.path)}
		}
		return m, nil

	case bodyFailedMsg:
		if msg.path == m.reading {
			m.reading = ""
		}

		if msg.gen != m.gen.ID || msg.path != m.diff.Path() {
			return m, nil
		}
		m.diff.StopPreview()
		m.note = notice{text: msg.err.Error(), bad: true}
		return m, nil

	case reloadFailedMsg:
		m.busy = false
		m.note = notice{text: msg.err.Error(), bad: true}
		return m, nil

	case tea.KeyPressMsg:
		return m.press(msg)
	}

	if m.diff.Composing() {
		return m.drafting(msg)
	}
	if m.compose.Active() {
		var cmd tea.Cmd
		m.compose, cmd = m.compose.Update(msg)
		return m, cmd
	}
	if m.picker.active() {
		return m, m.picker.update(msg)
	}
	return m, nil
}

func (m Model) press(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.diff.Composing() {
		return m.drafting(msg)
	}
	if m.compose.Active() {
		return m.typing(msg)
	}
	if m.picker.active() {
		return m.picking(msg)
	}

	if m.focus == focusDiff && m.diff.Placing() {
		var cmd tea.Cmd
		m.diff, cmd = m.diff.Update(msg)
		m.syncCursor()
		return m, cmd
	}

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

	if m.showing {
		if key.Matches(msg, m.keys.Close) {
			m.showing = false
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Tree):
		m.setFocus(focusTree)
		return m, nil
	case key.Matches(msg, m.keys.Diff):
		m.setFocus(focusDiff)
		return m, nil
	case key.Matches(msg, m.keys.Left):
		if m.focus == focusDiff && m.diff.Column(store.SideBase) {
			m.syncCursor()
			return m, nil
		}
		m.setFocus(focusTree)
		return m, nil
	case key.Matches(msg, m.keys.Right):
		if m.focus == focusDiff && m.diff.Column(store.SideHead) {
			m.syncCursor()
			return m, nil
		}
		m.setFocus(focusDiff)
		return m, nil
	}

	if m.diff.Selecting() && key.Matches(msg, m.diff.Keys.Cancel) {
		var cmd tea.Cmd
		m.diff, cmd = m.diff.Update(msg)
		return m, cmd
	}

	if key.Matches(msg, m.keys.Reload) {
		if m.busy {
			return m, nil
		}
		m.note = notice{text: "reloading"}
		m.busy = true
		return m, m.reload()
	}

	if key.Matches(msg, m.keys.Base) {
		if m.busy {
			return m, nil
		}
		m.note = notice{text: "loading bases"}
		m.busy = true
		return m, m.loadBases()
	}

	if i, mine := m.marked(msg); mine {
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

	if key.Matches(msg, m.keys.Comment) {
		if m.busy {
			return m, nil
		}

		cmd, ok := m.commentOn()
		if !ok {
			return m, nil
		}
		return m, cmd
	}

	if key.Matches(msg, m.keys.Note) {
		cmd := m.composing()
		return m, cmd
	}

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

	if key.Matches(msg, m.keys.Edit) {
		if m.busy {
			return m, nil
		}
		cmd, ok := m.editOn()
		if !ok {
			return m, nil
		}
		return m, cmd
	}

	if key.Matches(msg, m.keys.Expand) {
		m.diff.Expand()
		return m, nil
	}

	if key.Matches(msg, m.keys.Preview) {
		if !m.diff.TogglePreview() {
			m.note = notice{text: "no lines to show in " + comp.Safe(m.diff.Path())}
		}
		m.syncCursor()
		return m, nil
	}

	if key.Matches(msg, m.keys.Split) {
		if short := m.diff.ToggleSplit(); short > 0 {
			m.note = notice{text: fmt.Sprintf("side-by-side needs %d more columns in the pane", short)}
		}
		m.syncCursor()
		return m, nil
	}

	if key.Matches(msg, m.keys.Delete) {
		id, on := m.diff.Comment()
		if !on {
			return m, nil
		}
		m.note = notice{text: "deleting"}
		if m.busy {
			return m, nil
		}
		cmd := m.deleteComment(id)
		return m, cmd
	}

	if by, mine := m.stepping(msg); mine {
		if c, ok := m.commentRing(by); ok {
			m.landComment(c)
		}
		return m, nil
	}

	if s, ok, moved := m.walk(msg); moved {
		if ok {
			m.land(s)
		}
		return m, nil
	}

	var cmd tea.Cmd

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

func (m Model) picking(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Interrupt):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Close):
		m.picker.close()
		return m, nil
	case msg.String() == "enter":
		ref, ok := m.picker.choice()
		if !ok {
			return m, nil
		}
		if ref == m.base.Ref {
			m.picker.close()
			return m, nil
		}
		if m.busy {
			return m, nil
		}
		m.picker.err = ""
		m.busy = true
		return m, m.setBase(ref)
	}
	return m, m.picker.update(msg)
}

func (m Model) typing(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if !m.busy {
		m.note = notice{}
	}

	switch {
	case key.Matches(msg, m.keys.Interrupt):
		return m, tea.Quit

	case key.Matches(msg, m.compose.Keys.Discard):
		m.shut()
		return m, nil

	case key.Matches(msg, m.compose.Keys.Save):
		if m.busy {
			m.note = notice{text: "still writing"}
			return m, nil
		}

		if m.pending.Path != "" || m.editing != "" {
			cmd := m.save(m.compose.Value())
			return m, cmd
		}

		cmd := m.saveNote(m.compose.Value())
		return m, cmd
	}

	var cmd tea.Cmd
	m.compose, cmd = m.compose.Update(msg)
	return m, cmd
}

func (m *Model) shut() {
	m.compose.Close()
	m.diff.CloseDraft()
	m.pending, m.editing = review.Note{}, ""
}

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

func (m Model) stepping(msg tea.KeyPressMsg) (by int, mine bool) {
	switch {
	case key.Matches(msg, m.keys.NextComment):
		return 1, true
	case key.Matches(msg, m.keys.PrevComment):
		return -1, true
	}
	return 0, false
}

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

// syncDiff reads the tree's path off the model because Bubble Tea runs commands concurrently and a held j could land them out of order.
func (m *Model) syncDiff() {
	path := m.tree.Path()
	if path == "" || path == m.diff.Path() {
		return
	}

	m.diff.SetFile(m.fileAt(path), m.comments, m.replaced, m.gen.ID)
	if s, ok := m.firstOf(path); ok {
		m.cursor = s
		m.diff.Select(s.side, s.line)
	}
}

// setFocus tells only the tree, because the ring moves the diff pane's cursor from either pane.
func (m *Model) setFocus(f focus) {
	m.focus = f
	if f == focusTree {
		m.tree.Focus()
		return
	}
	m.tree.Blur()
}

func (m *Model) fileAt(path string) *review.File {
	for i := range m.changeset.Files {
		if m.changeset.Files[i].Diff.Path == path {
			return &m.changeset.Files[i]
		}
	}
	return nil
}
