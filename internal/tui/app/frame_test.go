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
	t *testing.T
	m tea.Model
}

func open(t *testing.T, width, height int) *screen {
	t.Helper()
	return named(t, "zen-review", width, height)
}

// named opens the reader on a repository of a given name, for the assertions
// about the heading.
func named(t *testing.T, repo string, width, height int) *screen {
	t.Helper()

	base := review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"}
	g := review.Generation{Seq: 2}

	s := &screen{t: t, m: app.New(theme.RosePineMoon, repo, base, g, testchangeset.Nested(t))}
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

	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
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

func (s *screen) lines() []string {
	s.t.Helper()
	return strings.Split(s.frame(), "\n")
}

func keystroke(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
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
