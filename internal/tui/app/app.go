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
	"github.com/zen-review/zen-review/internal/tui/comp"
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

	repo      string
	base      review.Base
	gen       review.Generation
	changeset review.Changeset

	tree tree.Model
	diff diffpane.Model
	help help.Model

	// The frames the two panes are drawn in. They hold the size, so the model
	// asks them what is left inside rather than subtracting the border twice.
	treePane comp.Pane
	diffPane comp.Pane

	focus   focus
	showing bool

	width  int
	height int
}

// New builds the screen over a changeset, open on its first file.
//
// The changeset is held by value and the panes point into its files, so it
// must not be appended to after this.
func New(t theme.Theme, repo string, base review.Base, g review.Generation, c review.Changeset) Model {
	m := Model{
		keys:      NewKeyMap(),
		theme:     t,
		repo:      repo,
		base:      base,
		gen:       g,
		changeset: c,
		tree:      tree.New(t, c),
		diff:      diffpane.New(t),
		help:      comp.Help(t),
		treePane:  comp.NewPane(t),
		diffPane:  comp.NewPane(t),
	}
	m.tree.Focus()

	// The tree's first file, not the changeset's. The tree sorts directories
	// above the files beside them, so opening on git's first would land the
	// cursor somewhere down the pane and scroll it there.
	if first := m.tree.First(); first != "" {
		m.tree.Select(first)
		m.syncDiff()
	}
	return m
}

// Run opens the reader on the terminal and returns when it closes.
func Run(ctx context.Context, t theme.Theme, repo string, base review.Base, g review.Generation, c review.Changeset) error {
	if _, err := tea.NewProgram(New(t, repo, base, g, c), tea.WithContext(ctx)).Run(); err != nil {
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

	case tea.KeyPressMsg:
		return m.press(msg)
	}
	return m, nil
}

func (m Model) press(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

	var cmd tea.Cmd

	// The half-page keys page the diff from either pane, so they are routed
	// before the focus is. Walking the tree is how the reader gets to a file;
	// reading it is what they came for, and the pane they are reading is the one
	// worth paging.
	if key.Matches(msg, m.diff.Keys.Scrolling()...) {
		m.diff, cmd = m.diff.Update(msg)
		return m, cmd
	}

	switch m.focus {
	case focusTree:
		m.tree, cmd = m.tree.Update(msg)
		m.syncDiff()
	case focusDiff:
		m.diff, cmd = m.diff.Update(msg)
	}
	return m, cmd
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
func (m *Model) syncDiff() {
	path := m.tree.Path()
	if path == "" || path == m.diff.Path() {
		return
	}
	m.diff.SetFile(m.fileAt(path))
}

// setFocus points the keys at a pane.
//
// Only the tree is told, because only the tree draws differently for it: the
// diff pane says which one has the keys through the title, which the root
// draws. It grows a Focus of its own when it grows a cursor.
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
