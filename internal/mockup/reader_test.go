package mockup_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/mockup"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
)

// The fixture has to mutate through the reader, not only through the Source: a demo that shows a
// hunk staying unread when r is pressed is worse than no demo.
func TestPressingRReadsAHunkAndTheBarCountsIt(t *testing.T) {
	s := reader(t, 120, 40)

	if got := s.bar(); strings.Contains(got, "4/10 read") {
		t.Fatalf("the bar already counts the mark before the key:\n%s", got)
	}

	s.press("r")

	if got := s.bar(); !strings.Contains(got, "5/10 read") {
		t.Errorf("the bar reads %q after r, want it to count 5 of 10", got)
	}

	s.press("{", "u")

	if got := s.bar(); !strings.Contains(got, "4/10 read") {
		t.Errorf("the bar reads %q after u, want the mark taken back", got)
	}
}

func TestTheNoteBoxWritesThroughToTheFixture(t *testing.T) {
	s := reader(t, 120, 40)

	s.press("C")
	s.press("r", "e", "a", "d")
	s.press("ctrl+s")

	if got := s.bar(); !strings.Contains(got, "note saved") {
		t.Errorf("the bar reads %q after the note was saved", got)
	}
}

func TestPreviewFillsTheFileBehindTheHunks(t *testing.T) {
	s := reader(t, 120, 40)

	was := len(strings.Split(s.frame(), "\n"))
	s.press("p")

	if strings.Contains(s.bar(), "no lines to show") {
		t.Fatalf("p found no body for the file the reader opened on:\n%s", s.bar())
	}
	if got := len(strings.Split(s.frame(), "\n")); got != was {
		t.Errorf("the frame is %d rows after p, want the pane's %d", got, was)
	}
	if !strings.Contains(s.frame(), "StaleGenerationError") {
		t.Errorf("the preview drew no line from outside the hunks:\n%s", s.frame())
	}
}

type screen struct {
	t *testing.T
	m tea.Model
}

func reader(t *testing.T, width, height int) *screen {
	t.Helper()

	src := mockup.New()
	r, err := src.Reload()
	if err != nil {
		t.Fatal(err)
	}

	s := &screen{t: t, m: app.New(testtheme.Dark, src, mockup.Repo, r, app.Launch{})}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

func (s *screen) press(keys ...string) {
	s.t.Helper()

	for _, k := range keys {
		s.send(keystroke(k))
	}
}

func (s *screen) send(msg tea.Msg) {
	s.t.Helper()

	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
	s.drain(cmd)
}

func (s *screen) drain(cmd tea.Cmd) {
	s.t.Helper()

	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}

		if batch, ok := out.(tea.BatchMsg); ok {
			for _, next := range batch {
				s.drain(next)
			}
			return
		}
		s.m, cmd = s.m.Update(out)
	}
}

func (s *screen) frame() string {
	s.t.Helper()
	return ansi.Strip(s.m.View().Content)
}

func (s *screen) bar() string {
	s.t.Helper()

	lines := strings.Split(s.frame(), "\n")
	return lines[len(lines)-1]
}

func keystroke(k string) tea.KeyPressMsg {
	if mod, rest, ok := strings.Cut(k, "+"); ok && mod == "ctrl" {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// The fixture carries an orphan to show the "was" label, which only draws for an anchor the pane
// found no row for. An orphan parked on a line the hunks still hold shows nothing.
func TestTheOrphanedCardSaysWhereItsAnchorWas(t *testing.T) {
	s := reader(t, 140, 44)

	for range 6 {
		s.press("]")
		if strings.Contains(s.frame(), "was line") {
			return
		}
	}
	t.Errorf("no card said where its anchor was:\n%s", s.frame())
}
