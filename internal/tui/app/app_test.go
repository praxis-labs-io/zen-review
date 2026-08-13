package app_test

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/golden"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/testchangeset"
)

// code takes the keys to the tree and walks it to the fixture's two-hunk Go
// file, counting back from the last row rather than down from the first: the
// reader opens on a binary file, and the rows between the two are directories a
// change to the fixture would move.
//
// The h is the reader moving into the tree. The diff pane holds the keys on the
// frame the reader opens on.
var code = []string{"h", "G", "k", "k", "k"}

// mark is the caret the diff pane puts on the heading the ring is on, written
// as an escape: a Nerd Font glyph does not survive every editor and pipe, and an
// empty string is contained in every row there is.
const mark = "\uf0da"

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

		// Narrow enough that a path outruns the tree and a line of code outruns
		// the diff pane, which is the only width that proves the panes clip
		// before they draw. The keys walk to a file that has code in it; the
		// tree opens on a binary one, which has none.
		{"narrow", 56, 16, code},

		// Both panes, because the overlay reads its keys off whichever one has
		// them and only one of the two can be wrong at a time.
		{"help", 100, 16, []string{"h", "?"}},
		{"help-diff", 100, 16, []string{"?"}},

		{"folded", 100, 16, []string{"h", "j", "space"}},
		{"deep", 100, 16, []string{"h", "G"}},

		// The ring on the fixture's two-hunk file, landed on the second: the
		// mark is on its heading, the tree followed, and the window has scrolled
		// as far as it goes without running off the end of the file.
		{"ring", 100, 16, []string{"n", "n", "}"}},

		// Two rows shorter than the tree has rows, so the pane has to scroll
		// rather than run off the status bar.
		{"short", 100, 8, []string{"h", "G"}},
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

	// Each size three ways: as it opens, on a binary file the painter draws no
	// rows for; walked to the file with the longest lines in the fixture; and
	// under the overlay, which is composited rather than drawn and comes back
	// with every line's trailing spaces trimmed unless they are put back.
	for _, keys := range [][]string{nil, code, {"?"}} {
		for _, size := range sizes {
			s := open(t, size.width, size.height).press(keys...)
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
		{"the diff pane opens with the keys", nil, false},
		{"h moves them left", []string{"h"}, true},
		{"l moves them back", []string{"h", "l"}, false},
		{"1 moves them left", []string{"1"}, true},
		{"2 moves them back", []string{"1", "2"}, false},
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
//
// The diff pane's heading wears the same fill, and one line of the frame holds
// both panes, so the fill alone no longer names a row. The bold on the name is
// what the tree's cursor row has and nothing else on screen does.
func TestTheCursorIsOnTheRowTheKeysMoved(t *testing.T) {
	for _, tt := range []struct {
		keys []string
		want string
	}{
		{[]string{"h"}, "logo.png"},
		{[]string{"h", "j", "j"}, "design.md"},
		{[]string{"h", "G"}, "README.md"},
	} {
		s := open(t, 100, 16).press(tt.keys...)

		found := filledTreeRow(t, s)
		switch {
		case found == "":
			t.Errorf("after %v no row of the tree carries the cursor", tt.keys)
		case !strings.Contains(found, tt.want):
			t.Errorf("after %v the cursor is on %q, want %q", tt.keys, found, tt.want)
		}
	}
}

// filledTreeRow is the tree's cursor row, stripped, found by the bold name over
// the fill that only that row carries. It is empty when no row carries one.
//
// Two foregrounds, because the name dims when the pane does not hold the keys
// and the ring moves the tree's cursor from the diff pane.
func filledTreeRow(t *testing.T, s *screen) string {
	t.Helper()

	for _, fg := range []color.Color{theme.RosePineMoon.Text, theme.RosePineMoon.Subtle} {
		name := lipgloss.NewStyle().
			Background(theme.RosePineMoon.SelectedBackground).
			Foreground(fg).Bold(true).Render("x")
		sgr := name[:strings.Index(name, "m")+1]

		for _, line := range strings.Split(s.raw(), "\n") {
			if strings.Contains(line, sgr) {
				return ansi.Strip(line)
			}
		}
	}
	return ""
}

// TestTheHalfPageKeysPageTheDiffFromTheTree. Walking the tree is how the reader
// gets to a file and reading it is what they came for, so ctrl+d belongs to the
// pane they are reading whichever one has the keys.
func TestTheHalfPageKeysPageTheDiffFromTheTree(t *testing.T) {
	// On the fixture's two-hunk file, with the tree still holding the keys.
	s := open(t, 100, 16).press(code...)
	columns := s.treeColumns()

	before := s.frame()
	s.press("ctrl+d")
	after := s.frame()

	if before == after {
		t.Fatalf("ctrl+d moved nothing:\n%s", after)
	}
	if got, want := column(after, columns), column(before, columns); got != want {
		t.Errorf("ctrl+d paged the tree as well:\n%s", got)
	}
}

// column is the left pane of a frame, for an assertion about one pane that has
// to ignore what the other did.
func column(frame string, width int) string {
	var b strings.Builder
	for _, line := range strings.Split(frame, "\n") {
		runes := []rune(line)
		b.WriteString(string(runes[:min(width, len(runes))]) + "\n")
	}
	return b.String()
}

// TestTheOverlayStaysABoxOnTheSmallestFrame. The compositor clips what does not
// fit, and a clipped box loses the border off two of its sides: the reader sees
// an unclosed rectangle and no bottom row. The modal is sized into the frame so
// the pane clips its content instead.
func TestTheOverlayStaysABoxOnTheSmallestFrame(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		// The smallest frame that draws panes at all, and one row more.
		{56, 4},
		{56, 6},
		{72, 10},
		{100, 16},
	} {
		frame := open(t, size.width, size.height).press("?").frame()

		for _, corner := range []string{"╭", "╮", "╰", "╯"} {
			if !strings.Contains(frame, corner) {
				t.Errorf("at %dx%d the box has no %s:\n%s", size.width, size.height, corner, frame)
			}
		}
	}
}

// TestTheOverlaySaysWhichPaneAKeyMoves. The overlay lists a pane's keys in one
// column, so a key that moves the other pane has to say so or the column reads
// as one more way to move this one.
func TestTheOverlaySaysWhichPaneAKeyMoves(t *testing.T) {
	tree := open(t, 100, 16).press("?").frame()
	if !strings.Contains(tree, "ctrl+d diff half page down") {
		t.Errorf("the tree's column claims ctrl+d pages the tree:\n%s", tree)
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

// TestTheFactsAreDrawnAndColoured, out of the goldens, where a
// layout change would hide their loss.
//
// The counts carry their own colours: one grey across the line says how much
// changed and not which way it went, which is the half a reader scans for.
func TestTheFactsAreDrawnAndColoured(t *testing.T) {
	th := theme.RosePineMoon
	s := open(t, 100, 16)

	// Label against the left edge of the pane, value against the right.
	for _, want := range []struct{ label, value string }{
		{"origin/main", "a1b2c3d"},
		{"Generation", "2"},
		{"Reviewed", "2/7"},
		{"Changes", "-3"},
	} {
		row := ""
		for i := range s.lines() {
			if r := s.treeRow(i); strings.HasPrefix(r, want.label) {
				row = r
				break
			}
		}
		switch {
		case row == "":
			t.Errorf("no row is labelled %q:\n%s", want.label, s.frame())
		case !strings.HasSuffix(row, want.value):
			t.Errorf("the %q row is %q, want it to end %q", want.label, row, want.value)
		}
	}

	// Every label reads at one weight. Only the churn and the burn-down carry a
	// colour, and they carry different ones.
	for _, label := range []string{"origin/main", "Generation", "Reviewed", "Changes"} {
		if want := lipgloss.NewStyle().Foreground(th.Muted).Render(label); !strings.Contains(s.raw(), want) {
			t.Errorf("the %q label is not muted", label)
		}
	}
	coloured := map[string]string{
		"the additions": lipgloss.NewStyle().Foreground(th.Success).Render("+10"),
		"the deletions": lipgloss.NewStyle().Foreground(th.Error).Render("-3"),
	}
	for part, style := range coloured {
		if !strings.Contains(s.raw(), style) {
			t.Errorf("%s is not in its own colour", part)
		}
	}
}

// TestTheBurnDownWearsItsOwnState. It is the same ladder as the glyphs beside
// the filenames, so the one number and the whole column agree at a glance.
func TestTheBurnDownWearsItsOwnState(t *testing.T) {
	th := theme.RosePineMoon

	// Two hunks in one file, so there is a half-way to be at. They only add, so
	// each has one anchor and one range covers it: a hunk that also removes has
	// a second anchor on the base side and takes two.
	const patch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,0 +1,1 @@
+one
@@ -10,0 +11,1 @@
+ten
`
	first := testchangeset.Head("a.go", 1, 1)
	second := testchangeset.Head("a.go", 11, 11)

	tests := []struct {
		name     string
		reviewed []store.ReviewedRange
		want     string
		colour   color.Color
	}{
		{"nothing read", nil, "0/2", th.Subtle},
		{"part read", []store.ReviewedRange{first}, "1/2", th.Warning},
		{"all read", []store.ReviewedRange{first, second}, "2/2", th.Accent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := over(t, testchangeset.Derive(t, patch, tt.reviewed...), 100, 16)

			want := lipgloss.NewStyle().Foreground(tt.colour).Render(tt.want)
			if !strings.Contains(s.raw(), want) {
				t.Errorf("the burn-down does not read %q in its own colour:\n%s", tt.want, s.frame())
			}
		})
	}
}

// TestTheFactsSitAtTheFootOfTheTree, ruled off from the rows rather than boxed
// beside them. A box of their own reads as a third pane to move into, and
// nothing there takes a key.
func TestTheFactsSitAtTheFootOfTheTree(t *testing.T) {
	s := open(t, 100, 16)
	lines := s.lines()

	rule, first := -1, -1
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "├"):
			rule = i
		case strings.Contains(line, "origin/main") && first < 0:
			first = i
		}
	}

	switch {
	case rule < 0:
		t.Fatalf("nothing rules the facts off from the rows:\n%s", strings.Join(lines, "\n"))
	case first != rule+1:
		t.Errorf("the facts start on line %d and the rule is on %d", first, rule)
	}

	// The rule joins the side borders rather than floating between them, and
	// the tree pane is the only thing it crosses.
	want := "├" + strings.Repeat("─", s.treeColumns()-2) + "┤"
	if !strings.HasPrefix(lines[rule], want) {
		t.Errorf("the rule is %q, want it to start %q", lines[rule], want)
	}
}

// TestThePadsBelongToTheEndsOfTheList. They are content rather than chrome, so
// a reader partway down a long list gets rows against both edges and does not
// pay two lines for a margin they cannot see the point of.
func TestThePadsBelongToTheEndsOfTheList(t *testing.T) {
	// At fifteen high the tree's window is seven rows over a list of twelve, so
	// there is a top, a middle and a bottom to be in. first and last are the
	// screen lines that window starts and ends on.
	const height, first, last = 15, 1, 7

	tests := []struct {
		name           string
		keys           []string
		topPad, botPad bool
	}{
		{"at the top", []string{"h"}, true, false},
		{"partway down", []string{"h", "j", "j", "j", "j", "j", "j", "j"}, false, false},
		{"at the bottom", []string{"h", "G"}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := open(t, 100, height).press(tt.keys...)
			lines := s.lines()

			blank := func(i int) bool { return s.treeRow(i) == "" }
			if got := blank(first); got != tt.topPad {
				t.Errorf("the pad above the list is %v, want %v: %q", got, tt.topPad, lines[first])
			}
			if got := blank(last); got != tt.botPad {
				t.Errorf("the pad below the list is %v, want %v: %q", got, tt.botPad, lines[last])
			}
		})
	}
}

// TestTheBarCarriesTheFactsWhenTheTreeCannot. The facts have to be somewhere,
// and a frame too short for the box is still a frame someone is reading.
func TestTheBarCarriesTheFactsWhenTheTreeCannot(t *testing.T) {
	s := open(t, 100, 7)
	lines := s.lines()
	bar := lines[len(lines)-1]

	for _, line := range lines[:len(lines)-1] {
		if strings.Contains(line, "generation 2") {
			t.Fatalf("the facts drew on a frame with no room for them:\n%s", s.frame())
		}
	}
	for _, want := range []string{"? help", "origin/main a1b2c3d", "Generation 2", "Reviewed 2/7"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar does not say %q: %q", want, bar)
		}
	}
}

// TestTheHintIsAgainstTheLeftEdge, where the eye starts.
func TestTheHintIsAgainstTheLeftEdge(t *testing.T) {
	for _, width := range []int{100, 72, 56} {
		bar := open(t, width, 16).lines()[15]

		if !strings.HasPrefix(bar, "j/k") {
			t.Errorf("at %d columns the bar starts %q", width, bar)
		}
		if got := lipgloss.Width(bar); got != width {
			t.Errorf("at %d columns the bar is %d wide: %q", width, got, bar)
		}

		// The bar cuts from the right, so a pane hint that does not fit would
		// take these with it and leave nothing on screen saying the overlay is
		// there. The reader who needs that is the one on the narrow terminal.
		if !strings.Contains(bar, "? help") || !strings.Contains(bar, "q quit") {
			t.Errorf("at %d columns the bar lost the way out: %q", width, bar)
		}
	}
}

// TestTheBarSaysWhatThePaneHoldingTheKeysCanDo. The overlay is a keypress away
// and nothing on screen said the keypress existed.
func TestTheBarSaysWhatThePaneHoldingTheKeysCanDo(t *testing.T) {
	tree := open(t, 100, 16).press("h").lines()[15]
	for _, want := range []string{"j/k move", "enter open", "space fold"} {
		if !strings.Contains(tree, want) {
			t.Errorf("the tree holds the keys and the bar does not say %q: %q", want, tree)
		}
	}

	diff := open(t, 100, 16).lines()[15]
	if !strings.Contains(diff, "j/k scroll") {
		t.Errorf("the diff holds the keys and the bar reads %q", diff)
	}
	if strings.Contains(diff, "space fold") {
		t.Errorf("the bar still names a key of the pane that lost the keys: %q", diff)
	}

	// The one that answers from both, said the same way from either.
	for _, bar := range []string{tree, diff} {
		if !strings.Contains(bar, "ctrl+d/u page") {
			t.Errorf("the bar drops the key that crosses the panes: %q", bar)
		}
	}
}

// TestOpeningAFileMovesTheReaderToIt separates the two things enter does from
// what walking the tree does.
func TestOpeningAFileMovesTheReaderToIt(t *testing.T) {
	s := open(t, 100, 16).press("h", "j", "j")
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
	s := open(t, 100, 16).press("h", "j")
	if got := s.lines()[0]; !strings.Contains(got, "assets/logo.png") {
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

// TestTheHintSurvivesTheFactsBesideIt, at the one height where the two share
// the bar. Dropping it drops the only thing on screen saying that ? exists, and
// the reader who needed it is the one on the small terminal. It also left the
// last row short of the screen.
func TestTheHintSurvivesTheFactsBesideIt(t *testing.T) {
	for _, width := range []int{100, 72, 56} {
		s := open(t, width, 7)
		bar := s.lines()[6]

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

// ringPatch is two files with two hunks each, so there is a hunk to cross into
// and a file boundary to cross over.
const ringPatch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,0 +1,1 @@
+one
@@ -10,0 +11,1 @@
+ten
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,0 +1,1 @@
+uno
@@ -10,0 +11,1 @@
+diez
`

// heading is the diff pane's cursor row, stripped, found by the mark only that
// row carries. It is empty when the pane marks nothing.
func heading(t *testing.T, s *screen) string {
	t.Helper()

	for _, line := range strings.Split(s.frame(), "\n") {
		if strings.Contains(line, mark) {
			return line
		}
	}
	return ""
}

// TestTheReaderOpensOnTheFirstHunkTheyHaveNotRead, which is not the first hunk
// of the changeset whenever the top of it has already been read.
func TestTheReaderOpensOnTheFirstHunkTheyHaveNotRead(t *testing.T) {
	c := testchangeset.Derive(t, ringPatch,
		testchangeset.Head("a.go", 1, 1),
		testchangeset.Head("a.go", 11, 11),
	)
	s := over(t, c, 100, 16)

	if title := s.lines()[0]; !strings.Contains(title, "b.go") {
		t.Errorf("the reader opened on %q, want the first file holding an unread hunk", title)
	}
	if got := heading(t, s); !strings.Contains(got, "@@ -1,0 +1,1 @@") {
		t.Errorf("the mark is on %q, want b.go's first hunk", got)
	}
	if !strings.Contains(s.raw(), seam(false)) {
		t.Error("the reader opened with the keys on the tree, and came here to read")
	}
}

// TestTheRingWrapsPastTheLastUnread, so n held down keeps going rather than
// stopping on the last one and reporting nothing.
func TestTheRingWrapsPastTheLastUnread(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	// Four hunks, so the fourth n is the one that comes back round.
	first := heading(t, s)
	s.press("n", "n", "n")
	if last := heading(t, s); last == first {
		t.Fatalf("three presses of n came back to where they started: %q", last)
	}

	s.press("n")
	if got := heading(t, s); got != first {
		t.Errorf("n off the last unread landed on %q, want the first, %q", got, first)
	}
}

// TestAFullyReadChangesetLeavesTheRingWhereItIs. n has done its job when there
// is nothing left to find, and the burn-down in the facts is what says so.
func TestAFullyReadChangesetLeavesTheRingWhereItIs(t *testing.T) {
	c := testchangeset.Derive(t, ringPatch,
		testchangeset.Head("a.go", 1, 1), testchangeset.Head("a.go", 11, 11),
		testchangeset.Head("b.go", 1, 1), testchangeset.Head("b.go", 11, 11),
	)
	s := over(t, c, 100, 16)

	before := s.frame()
	s.press("n", "n", "N")
	if after := s.frame(); after != before {
		t.Errorf("n moved on a changeset with nothing left to read:\n%s", after)
	}
}

// TestTheHunkKeyCrossesIntoTheNextFileAndTheTreeFollows, so the two panes never
// disagree about what is open.
func TestTheHunkKeyCrossesIntoTheNextFileAndTheTreeFollows(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	// Off a.go's second hunk and into b.go.
	s.press("}", "}")

	if title := s.lines()[0]; !strings.Contains(title, "b.go") {
		t.Errorf("} did not cross the file boundary: %q", title)
	}
	if got := filledTreeRow(t, s); !strings.Contains(got, "b.go") {
		t.Errorf("the tree's cursor is on %q, want it to follow the diff pane", got)
	}
}

// TestTheFileKeyLandsOnTheFilesFirstHunk, going either way. Stepping back to a
// file's last hunk would open it at the bottom and read as a different key.
func TestTheFileKeyLandsOnTheFilesFirstHunk(t *testing.T) {
	for _, tt := range []struct {
		keys []string
		want string
	}{
		{[]string{"tab"}, "b.go"},
		{[]string{"tab", "shift+tab"}, "a.go"},
	} {
		s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press(tt.keys...)

		if title := s.lines()[0]; !strings.Contains(title, tt.want) {
			t.Errorf("%v opened %q, want %s", tt.keys, title, tt.want)
		}
		if got := heading(t, s); !strings.Contains(got, "@@ -1,0 +1,1 @@") {
			t.Errorf("%v landed on %q, want the file's first hunk", tt.keys, got)
		}
	}
}

// TestAFileWithNoHunksIsAStopOnTheRing. A binary file is one thing to read, and
// the burn-down counts it, so a ring that stepped over it would leave n unable
// to walk the count to zero.
func TestAFileWithNoHunksIsAStopOnTheRing(t *testing.T) {
	s := open(t, 100, 16)

	// The fixture's binary file is where the reader opens, so the ring has to
	// come all the way back round to it.
	if title := s.lines()[0]; !strings.Contains(title, "assets/logo.png") {
		t.Fatalf("the reader did not open on the binary file: %q", title)
	}

	s.press("n", "n", "n", "n", "n")
	if title := s.lines()[0]; !strings.Contains(title, "assets/logo.png") {
		t.Errorf("n never came back to the binary file: %q", title)
	}
}

// TestTheMarkedHeadingIsFilledAndOneCellWide. The fill is the signal a stripped
// golden cannot see, and a two-cell glyph would put every row under the heading
// out of step with the code above it.
func TestTheMarkedHeadingIsFilledAndOneCellWide(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	if got := lipgloss.Width(mark); got != 1 {
		t.Errorf("the mark is %d cells wide, want 1", got)
	}

	marked := lipgloss.NewStyle().
		Background(theme.RosePineMoon.SelectedBackground).
		Foreground(theme.RosePineMoon.Accent).Render(mark)
	if !strings.Contains(s.raw(), marked) {
		t.Error("the heading the ring is on carries no fill")
	}
}

// TestTheTreeFollowsTheRingOffADirectoryRow. A directory row leaves the diff
// pane on the file before it, so the tree's cursor can be somewhere the pane is
// not, and a landing that only moves the tree when the file changes leaves the
// two disagreeing.
func TestTheTreeFollowsTheRingOffADirectoryRow(t *testing.T) {
	// To the two-hunk file, then one row up onto the directory holding it. The
	// pane stays on the file, so the two panes are now on different things.
	s := open(t, 100, 16).press(code...).press("k")

	if got := filledTreeRow(t, s); strings.Contains(got, "state.go") {
		t.Fatalf("the tree's cursor is still on the file, so this proves nothing: %q", got)
	}
	if title := s.lines()[0]; !strings.Contains(title, "state.go") {
		t.Fatalf("the directory row took the file out of the pane: %q", title)
	}

	// Inside the file the pane is already on, so nothing about the file changes
	// and only the tree has anywhere to move.
	s.press("}")
	if got := filledTreeRow(t, s); !strings.Contains(got, "state.go") {
		t.Errorf("the tree's cursor is on %q, want the file the ring moved inside", got)
	}
}

// TestOpeningTheFileAlreadyOpenLeavesTheRingWhereItIs. Pressing enter on the
// file being read is not a move onto it, and putting the ring back at the top
// would throw away where the reader had got to.
func TestOpeningTheFileAlreadyOpenLeavesTheRingWhereItIs(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")

	was := heading(t, s)
	if !strings.Contains(was, "@@ -10,0 +11,1 @@") {
		t.Fatalf("the ring is on %q, want a.go's second hunk", was)
	}

	s.press("h", "enter")
	if got := heading(t, s); got != was {
		t.Errorf("enter on the open file moved the ring to %q", got)
	}
}
