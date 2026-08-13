package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
// which pane has the keys is said in the border colour and a stripped golden
// cannot show it.
//
// The seam is where the two panes meet, so it carries both answers at once: a
// frame with the wrong pane lit fails on the same string a frame with both lit
// does.
func TestFocusMovesBetweenThePanes(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		tree bool
	}{
		{"the tree opens with the keys", nil, true},
		{"l moves them right", []string{"l"}, false},
		{"h moves them back", []string{"l", "h"}, true},
		{"2 moves them right", []string{"2"}, false},
		{"1 moves them back", []string{"2", "1"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := open(t, 100, 16).press(tt.keys...)
			if got := s.raw(); !strings.Contains(got, seam(tt.tree)) {
				t.Errorf("the border does not say the keys are on the tree = %v", tt.tree)
			}
		})
	}
}

// seam is the two corners where the panes meet, the tree's right and the diff
// pane's left, coloured for whichever holds the keys.
func seam(treeFocused bool) string {
	lit := lipgloss.NewStyle().Foreground(theme.RosePineMoon.Accent)
	dim := lipgloss.NewStyle().Foreground(theme.RosePineMoon.BorderSubtleOrBorder())

	if treeFocused {
		return lit.Render("╮") + dim.Render("╭")
	}
	return dim.Render("╮") + lit.Render("╭")
}

// TestTheCursorIsOnTheRowTheKeysMoved. The tree marks its cursor with a filled
// background and nothing else, so a stripped golden cannot see it and j could
// stop moving without a single frame changing.
func TestTheCursorIsOnTheRowTheKeysMoved(t *testing.T) {
	fill := lipgloss.NewStyle().Background(theme.RosePineMoon.SelectedBackground).Render("x")
	sgr := fill[len("\x1b["):strings.Index(fill, "m")]

	for _, tt := range []struct {
		keys []string
		want string
	}{
		{nil, "README.md"},
		{[]string{"j", "j"}, "logo.png"},
		{[]string{"G"}, "painting_the_unif"},
	} {
		s := open(t, 100, 16).press(tt.keys...)

		var found string
		for _, line := range strings.Split(s.raw(), "\n") {
			if strings.Contains(line, sgr) {
				found = ansi.Strip(line)
				break
			}
		}
		switch {
		case found == "":
			t.Errorf("after %v no row carries the cursor", tt.keys)
		case !strings.Contains(found, tt.want):
			t.Errorf("after %v the cursor is on %q, want %q", tt.keys, found, tt.want)
		}
	}
}

// TestTheTreeIsHeadedByTheRepository, so a reader with two of these open knows
// which one they are looking at. What is in it is said in the footer.
//
// The directory name reads as a name rather than as a path segment, and only
// its first letters are touched: lowercasing the rest renames a repository its
// owner did not.
func TestTheTreeIsHeadedByTheRepository(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"zen-review", "─[1]─Zen Review─"},
		{"my_side_project", "─[1]─My Side Project─"},
		{"zenOcto", "─[1]─ZenOcto─"},
		{"CLAUDE", "─[1]─CLAUDE─"},
		{"dotfiles", "─[1]─Dotfiles─"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			head := named(t, tt.repo, 100, 16).lines()[0]
			if !strings.Contains(head, tt.want) {
				t.Errorf("the tree is headed %q, want it to hold %q", head, tt.want)
			}
		})
	}
}

// TestTheTreesFooterSaysHowMuchThereIs. One grey across the whole line says how
// much changed and not which way it went, which is the half a reader is
// scanning for.
func TestTheTreesFooterSaysHowMuchThereIs(t *testing.T) {
	th := theme.RosePineMoon
	foot := open(t, 100, 16).rawLine(14)

	want := map[string]string{
		"the file count": lipgloss.NewStyle().Foreground(th.Accent).Render("6 files"),
		"the additions":  lipgloss.NewStyle().Foreground(th.Success).Render("+10"),
		"the deletions":  lipgloss.NewStyle().Foreground(th.Error).Render("-3"),
	}

	for part, style := range want {
		if !strings.Contains(foot, style) {
			t.Errorf("%s is not in its own colour: %q", part, ansi.Strip(foot))
		}
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
	s := open(t, 100, 16).press("j", "j", "j", "j")
	if title := s.lines()[0]; !strings.Contains(title, "docs/superpowers/specs/design.md") {
		t.Errorf("walking onto a file did not open it in the diff pane: %q", title)
	}
	if !strings.Contains(s.raw(), seam(true)) {
		t.Errorf("walking the tree gave the focus away")
	}

	s.press("enter")
	if !strings.Contains(s.raw(), seam(false)) {
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
