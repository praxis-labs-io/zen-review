package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/golden"
)

// TestGoldenFrames locks the layout at the widths that prove something.
//
// The goldens hold the frame with its escapes stripped, so a diff in review is
// readable and a lipgloss bump does not churn every file. What they prove is
// alignment and clipping; colour is asserted directly further down.
func TestGoldenFrames(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		keys          []string
	}{
		{"open", 100, 16, nil},

		// Narrow enough that a path outruns the tree and a hunk header outruns
		// the diff pane, which is the only width that proves the panes clip
		// before they draw.
		{"narrow", 56, 16, nil},

		{"help", 100, 16, []string{"?"}},
		{"folded", 100, 16, []string{"j", "space"}},
		{"deep", 100, 16, []string{"G"}},

		// Two rows shorter than the tree has rows, so the pane has to scroll
		// rather than run off the status bar.
		{"short", 100, 8, []string{"G"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := open(t, tt.width, tt.height).press(tt.keys...)
			golden.Compare(t, tt.name, []byte(s.frame()+"\n"))
		})
	}
}

// TestTheFrameIsExactlyTheTerminal is the clipping proof the goldens cannot
// give on their own.
//
// A pane clips overflow silently: a row wider than the terminal loses its
// trailing columns mid-cell with no ellipsis, and a golden written from that
// row looks just as tidy as a correct one.
func TestTheFrameIsExactlyTheTerminal(t *testing.T) {
	sizes := []struct{ width, height int }{
		{100, 16},
		{56, 16},
		{56, 6},
		{200, 40},

		// A base short enough that the facts do not reach the hint on their own,
		// which is the case that used to leave the last row short of the screen.
		{72, 10},
	}

	for _, size := range sizes {
		s := open(t, size.width, size.height)
		lines := s.lines()

		if len(lines) != size.height {
			t.Errorf("%dx%d drew %d lines, want %d", size.width, size.height, len(lines), size.height)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w != size.width {
				t.Errorf("%dx%d line %d is %d columns, want %d: %q",
					size.width, size.height, i, w, size.width, line)
			}
		}
	}
}

// TestFocusMovesBetweenThePanes asserts the colour the frame carries, because
// which pane has the keys is said in colour and a stripped golden cannot show
// it.
func TestFocusMovesBetweenThePanes(t *testing.T) {
	active := lipgloss.NewStyle().Foreground(theme.RosePineMoon.Secondary).Bold(true)
	quiet := lipgloss.NewStyle().Foreground(theme.RosePineMoon.Faint)

	s := open(t, 100, 16)
	if got := s.raw(); !strings.Contains(got, active.Render("CHANGES")) {
		t.Errorf("the tree opens without the focused title")
	}

	s.press("l")
	if got := s.raw(); !strings.Contains(got, quiet.Render("CHANGES")) {
		t.Errorf("l left the tree title looking focused")
	}
	if got := s.raw(); !strings.Contains(got, active.Render("README.md")) {
		t.Errorf("l did not focus the diff pane")
	}

	s.press("h")
	if got := s.raw(); !strings.Contains(got, active.Render("CHANGES")) {
		t.Errorf("h did not put the focus back on the tree")
	}
}

// TestTheStatusBarSaysWhereTheReviewIs keeps the three facts the bar exists for
// out of the goldens, where a layout change would hide their loss.
func TestTheStatusBarSaysWhereTheReviewIs(t *testing.T) {
	s := open(t, 100, 16)
	bar := s.lines()[15]

	for _, want := range []string{"origin/main (a1b2c3d)", "generation 2", "2 / 7 reviewed"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the status bar does not say %q: %q", want, bar)
		}
	}
}

// TestOpeningAFileMovesTheReaderToIt separates the two things enter does from
// what walking the tree does.
func TestOpeningAFileMovesTheReaderToIt(t *testing.T) {
	active := lipgloss.NewStyle().Foreground(theme.RosePineMoon.Secondary).Bold(true)

	s := open(t, 100, 16).press("j", "j", "j", "j")
	if title := s.lines()[0]; !strings.Contains(title, "docs/superpowers/specs/design.md") {
		t.Errorf("walking onto a file did not open it in the diff pane: %q", title)
	}
	if !strings.Contains(s.raw(), active.Render("CHANGES")) {
		t.Errorf("walking the tree gave the focus away")
	}

	s.press("enter")
	if !strings.Contains(s.raw(), active.Render("docs/superpowers/specs/design.md")) {
		t.Errorf("enter did not move the focus to the diff pane")
	}
}

// TestADirectoryLeavesTheDiffPaneAlone. Blanking the pane on the way past a
// directory row would punish walking the tree.
func TestADirectoryLeavesTheDiffPaneAlone(t *testing.T) {
	s := open(t, 100, 16).press("j")
	if got := s.lines()[0]; !strings.Contains(got, "README.md") {
		t.Errorf("stepping onto a directory changed the diff pane: %q", got)
	}
}

// TestHelpTakesTheKeys. Routing keys under an overlay scrolls a pane the reader
// cannot see, and they are still scrolled when it closes.
func TestHelpTakesTheKeys(t *testing.T) {
	s := open(t, 100, 16)
	before := s.frame()

	s.press("?", "j", "j", "G")
	s.press("esc")

	if got := s.frame(); got != before {
		t.Errorf("keys reached the panes under the overlay:\n%s\nwant\n%s", got, before)
	}
}

// TestQuitting. q and ctrl+c both leave, from either pane.
func TestQuitting(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		quit tea.KeyPressMsg
	}{
		{"q from the tree", nil, keystroke("q")},
		{"q from the diff pane", []string{"l"}, keystroke("q")},
		{"ctrl+c from the overlay", []string{"?"}, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := open(t, 100, 16).press(tt.keys...)

			_, cmd := s.m.Update(tt.quit)
			if cmd == nil {
				t.Fatalf("no command came back, so nothing quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("the command was not a quit")
			}
		})
	}
}

// TestATerminalTooSmallSaysSo, rather than drawing a frame out of negative
// widths.
func TestATerminalTooSmallSaysSo(t *testing.T) {
	s := open(t, 40, 10)
	if got := s.frame(); !strings.Contains(got, "the terminal is 40x10") {
		t.Errorf("a 40-column terminal drew a frame: %q", got)
	}
}

// TestTheHintSurvivesANarrowTerminal. Dropping it drops the only thing on
// screen saying that ? exists, and the reader who needed it is the one on the
// narrow terminal. It also left the last row short of the screen.
func TestTheHintSurvivesANarrowTerminal(t *testing.T) {
	for _, width := range []int{100, 72, 56} {
		s := open(t, width, 10)
		bar := s.lines()[9]

		if !strings.Contains(bar, "? help") {
			t.Errorf("at %d columns the status bar lost the hint: %q", width, bar)
		}
		if got := lipgloss.Width(bar); got != width {
			t.Errorf("at %d columns the status bar is %d wide: %q", width, got, bar)
		}
	}
}

// TestASmallTerminalStillFillsTheScreen. Every other path returns width by
// height, and a frame that stops short leaves whatever was under it on screen.
func TestASmallTerminalStillFillsTheScreen(t *testing.T) {
	for _, size := range []struct{ width, height int }{{54, 20}, {20, 4}, {80, 2}} {
		s := open(t, size.width, size.height)
		lines := s.lines()

		if len(lines) != size.height {
			t.Errorf("%dx%d drew %d lines", size.width, size.height, len(lines))
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != size.width {
				t.Errorf("%dx%d line %d is %d columns: %q", size.width, size.height, i, got, line)
			}
		}
	}
}
