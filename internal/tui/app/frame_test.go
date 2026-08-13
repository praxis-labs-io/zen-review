package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/testchangeset"
	"github.com/zen-review/zen-review/internal/tui/app"
)

// screen is a model with the runtime's job done by hand: a command a model
// returns is run and its message delivered, which is the difference between
// testing what the reader sees and testing what Update happened to return.
type screen struct {
	t   *testing.T
	m   tea.Model
	src *source
}

// source is the reload key with the git taken out: whatever the next press
// should bring back, and a count of the presses that took it.
//
// It answers with the same reload every time unless a test moves it, which is
// the work tree not having changed.
type source struct {
	at    app.Reload
	err   error
	calls int
}

func (s *source) Reload() (app.Reload, error) {
	s.calls++
	if s.err != nil {
		return app.Reload{}, s.err
	}
	return s.at, nil
}

func open(t *testing.T, width, height int) *screen {
	t.Helper()
	return named(t, "zen-review", width, height)
}

// named opens the reader on a repository of a given name, for the assertions
// about the heading.
func named(t *testing.T, repo string, width, height int) *screen {
	t.Helper()
	return build(t, repo, testchangeset.Nested(t), width, height)
}

// over opens the reader on a changeset of the test's own, for the assertions
// about how far down the review is.
func over(t *testing.T, c review.Changeset, width, height int) *screen {
	t.Helper()
	return build(t, "zen-review", c, width, height)
}

func build(t *testing.T, repo string, c review.Changeset, width, height int) *screen {
	t.Helper()

	r := app.Reload{
		Base:       review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"},
		Generation: review.Generation{ID: 2, Seq: 2},
		Changeset:  c,
	}

	src := &source{at: r}
	s := &screen{t: t, m: app.New(theme.RosePineMoon, src, repo, r), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

// press types a key, the way key.Matches reads one: Text is what a printable
// key carries, and a name alone is what everything else answers to.
func (s *screen) press(keys ...string) *screen {
	s.t.Helper()
	for _, k := range keys {
		s.send(keystroke(k))
	}
	return s
}

func (s *screen) send(msg tea.Msg) {
	s.t.Helper()
	s.drain(s.hold(msg))
}

// hold applies a message and stops before running the command it returned,
// which is the only place a frame shows work still in flight. The runtime runs
// that command on a goroutine and the reader sees this frame while it does.
func (s *screen) hold(msg tea.Msg) tea.Cmd {
	s.t.Helper()

	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
	return cmd
}

// drain runs a held command and delivers what it returned, and whatever that
// returns in turn.
func (s *screen) drain(cmd tea.Cmd) {
	s.t.Helper()

	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}
		s.m, cmd = s.m.Update(out)
	}
}

// frame is the screen with the escapes stripped, which is what a golden holds:
// layout, alignment and clipping survive, and a lipgloss bump does not churn
// every file.
func (s *screen) frame() string {
	s.t.Helper()
	return ansi.Strip(s.raw())
}

// raw is the screen as the terminal gets it, for the assertions about colour
// that a stripped frame cannot carry.
func (s *screen) raw() string {
	s.t.Helper()
	return s.m.View().Content
}

// treeColumns is how wide the tree's pane came out, read off the frame rather
// than assumed: it is a share of the terminal and moves with it.
func (s *screen) treeColumns() int {
	s.t.Helper()

	for i, r := range []rune(s.lines()[0]) {
		if r == '╮' {
			return i + 1
		}
	}
	s.t.Fatalf("the frame has no tree pane:\n%s", s.frame())
	return 0
}

// treeRow is one line of the tree's column with its borders and its padding
// taken off, which is what a row of that pane actually says.
func (s *screen) treeRow(i int) string {
	s.t.Helper()

	runes := []rune(s.lines()[i])
	return strings.TrimSpace(string(runes[1 : s.treeColumns()-1]))
}

func (s *screen) lines() []string {
	s.t.Helper()
	return strings.Split(s.frame(), "\n")
}

// bar is the status line, which is the last one whatever the frame is.
func (s *screen) bar() string {
	s.t.Helper()

	lines := s.lines()
	return lines[len(lines)-1]
}

// reloading points the source at a new changeset, as a generation the refresh
// built rather than one it found already there. The next press of s brings it
// back.
func (s *screen) reloading(c review.Changeset) *screen {
	s.t.Helper()

	g := s.src.at.Generation
	s.src.at = app.Reload{
		Base:       s.src.at.Base,
		Generation: review.Generation{ID: g.ID + 1, Seq: g.Seq + 1},
		Changeset:  c,
	}
	return s
}

func keystroke(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}

	r := []rune(k)
	if len(r) != 1 {
		panic("keystroke: " + k + " needs a case of its own")
	}
	return tea.KeyPressMsg{Code: r[0], Text: k}
}
