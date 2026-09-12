package app_test

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/golden"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
)

var code = []string{"h", "G", "k", "k", "k"}

const mark = "\uf0da" // an escape, because a Nerd Font glyph does not survive every editor and pipe

func TestGoldenFrames(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		keys          []string
	}{
		{"open", 100, 16, nil},

		{"narrow", 56, 16, code},

		{"help", 100, 16, []string{"h", "?"}},
		{"help-diff", 100, 16, []string{"?"}},

		{"help-eighty", 80, 20, []string{"?"}},

		{"selecting", 100, 16, []string{"n", "n", "j", "v", "j", "j"}},

		{"commenting", 100, 16, []string{"n", "n", "j", "c", "n", "o"}},

		{"folded", 100, 16, []string{"h", "j", "space"}},
		{"deep", 100, 16, []string{"h", "G"}},

		{"ring", 100, 16, []string{"n", "n", "}"}},

		{"reloaded", 100, 16, []string{"s"}},

		{"short", 100, 8, []string{"h", "G"}},

		{"split", 120, 16, []string{"n", "n", "|"}},

		{"split-narrow", 100, 16, []string{"n", "n", "|"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := open(t, tt.width, tt.height).press(tt.keys...)
			golden.Compare(t, tt.name, []byte(s.frame()+"\n"))
		})
	}
}

const respondedCard = "dddddddddddd"

func TestTheRespondedCardGoldenFrame(t *testing.T) {
	s := commented(t, 100, 20, testchangeset.NestedComments()...)
	s.press("]", "]", "]", "]")

	golden.Compare(t, "responded", []byte(s.frame()+"\n"))
}

func TestTheReplacedBlockGoldenFrame(t *testing.T) {
	s := replacing(t, 100, 24,
		"\tfiles := d.Files()", "sort.Strings(files)", "return files", "// unreachable")
	s.press("]", "]", "]", "]")

	golden.Compare(t, "replaced", []byte(s.frame()+"\n"))
}

func TestTheExpandedBlockGoldenFrame(t *testing.T) {
	s := replacing(t, 100, 24,
		"\tfiles := d.Files()", "sort.Strings(files)", "return files", "// unreachable")
	s.press("]", "]", "]", "]", ">")

	golden.Compare(t, "expanded", []byte(s.frame()+"\n"))
}

func TestTheFrameIsExactlyTheTerminal(t *testing.T) {
	sizes := []struct{ width, height int }{
		{100, 16},
		{56, 16},
		{56, 6},
		{200, 40},

		{72, 10},
	}

	for _, keys := range [][]string{nil, code, {"?"}, {"s"}, {"C"}, {"c"}, {"v", "j", "j"}} {
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

func seam(treeFocused bool) string {
	lit := lipgloss.NewStyle().Foreground(testtheme.Dark.Accent)
	dim := lipgloss.NewStyle().Foreground(testtheme.Dark.BorderSubtleOrBorder())

	if treeFocused {
		return lit.Render("╮") + dim.Render("╭")
	}
	return dim.Render("╮") + lit.Render("╭")
}

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

func filledTreeRow(t *testing.T, s *screen) string {
	t.Helper()

	for _, fg := range []color.Color{testtheme.Dark.Text, testtheme.Dark.Subtle} {
		name := lipgloss.NewStyle().
			Background(testtheme.Dark.SelectedBackground).
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

func TestTheHalfPageKeysPageTheDiffFromTheTree(t *testing.T) {
	s := open(t, 100, 16).press(code...)
	columns := s.treeColumns()

	before := s.frame()
	s.press("ctrl+d", "ctrl+d")
	after := s.frame()

	if before == after {
		t.Fatalf("ctrl+d moved nothing:\n%s", after)
	}
	if got, want := column(after, columns), column(before, columns); got != want {
		t.Errorf("ctrl+d paged the tree as well:\n%s", got)
	}
}

func column(frame string, width int) string {
	var b strings.Builder
	for _, line := range strings.Split(frame, "\n") {
		runes := []rune(line)
		b.WriteString(string(runes[:min(width, len(runes))]) + "\n")
	}
	return b.String()
}

func TestTheOverlayStaysABoxOnTheSmallestFrame(t *testing.T) {
	for _, size := range []struct{ width, height int }{
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

func TestTheOverlaySaysWhichPaneAKeyMoves(t *testing.T) {
	tree := open(t, 100, 16).press("?").frame()
	if !strings.Contains(tree, "ctrl+d diff half page down") {
		t.Errorf("the tree's column claims ctrl+d pages the tree:\n%s", tree)
	}
}

func TestTheBasePickerFiltersAndSelectsAcrossGroups(t *testing.T) {
	s := open(t, 100, 20)
	s.src.candidates = review.BaseCandidates{
		Local: []review.Candidate{
			{Branch: "parent", Ahead: 1},
			{Branch: "older", Ahead: 3},
		},
		Remote: []review.Candidate{{Branch: "origin/main", Ahead: 4}},
	}

	s.press("b")
	frame := s.frame()
	for _, want := range []string{"Base", "Local", "Remote", "origin/main (current)"} {
		if !strings.Contains(frame, want) {
			t.Errorf("picker has no %q:\n%s", want, frame)
		}
	}
	var selected string
	for _, line := range strings.Split(s.raw(), "\n") {
		if strings.Contains(ansi.Strip(line), "> parent") {
			selected = line
			break
		}
	}
	if !strings.Contains(selected, styleParams(t, lipgloss.NewStyle().Background(testtheme.Dark.SelectedBackground))) ||
		!strings.Contains(selected, ";1m") {
		t.Errorf("selected base does not stand on its fill:\n%s", s.raw())
	}

	s.press("p", "a", "r")
	frame = s.frame()
	if !strings.Contains(frame, "parent") || strings.Contains(frame, "older") {
		t.Errorf("filter did not leave only parent:\n%s", frame)
	}
	s.press("enter")
	if got := s.src.wrote; len(got) != 1 || got[0] != "SetBase parent" {
		t.Errorf("writes = %v, want SetBase parent", got)
	}
	if strings.Contains(s.frame(), "↑/↓ move") {
		t.Errorf("picker stayed open after the base changed:\n%s", s.frame())
	}
}

func styleParams(t *testing.T, style lipgloss.Style) string {
	t.Helper()
	probe := style.Render("x")
	start, end := strings.Index(probe, "["), strings.Index(probe, "m")
	if start < 0 || end < start {
		t.Fatalf("style rendered no escape: %q", probe)
	}
	return probe[start+1 : end]
}

func TestTheSelectedBaseStandsWithoutAFill(t *testing.T) {
	s := themed(t, testtheme.Bare, 100, 20)
	s.src.candidates = review.BaseCandidates{
		Local: []review.Candidate{{Branch: "parent", Ahead: 1}, {Branch: "older", Ahead: 3}},
	}

	s.press("b")

	var selected, beside string
	for _, line := range strings.Split(s.raw(), "\n") {
		switch {
		case strings.Contains(ansi.Strip(line), "> parent"):
			selected = line
		case strings.Contains(ansi.Strip(line), "older"):
			beside = line
		}
	}

	if !strings.Contains(selected, ";1m") && !strings.Contains(selected, "[1m") {
		t.Errorf("the selected base is not bold, so with no fill nothing marks it:\n%s", s.raw())
	}
	if strings.Contains(beside, ";1m") || strings.Contains(beside, "[1m") {
		t.Errorf("a base the cursor is not on is bold:\n%s", s.raw())
	}
}

func TestTheBasePickerCanCancelAndChooseNothing(t *testing.T) {
	s := open(t, 100, 20)
	s.src.candidates = review.BaseCandidates{
		Remote: []review.Candidate{{Branch: "origin/main", Ahead: 1}},
	}

	s.press("b", "enter")
	if len(s.src.wrote) != 0 || strings.Contains(s.frame(), "↑/↓ move") {
		t.Errorf("choosing the current base did not close without writing")
	}

	s.press("b", "x", "esc")
	if len(s.src.wrote) != 0 {
		t.Errorf("writes = %v, want none", s.src.wrote)
	}
}

func TestTheBasePickerTakesARefNobodyListed(t *testing.T) {
	s := open(t, 100, 20)
	s.src.candidates = review.BaseCandidates{
		Local: []review.Candidate{{Branch: "parent", Ahead: 1}},
	}

	s.press("b", "H", "E", "A", "D", "~", "5")
	if frame := s.frame(); !strings.Contains(frame, "no branch matches, enter takes it as a ref") {
		t.Errorf("the box does not say what enter will do with it:\n%s", frame)
	}

	s.press("enter")
	if got := s.src.wrote; len(got) != 1 || got[0] != "SetBase HEAD~5" {
		t.Errorf("writes = %v, want SetBase HEAD~5", got)
	}
}

func TestTheBasePickerClearsALastRefsFailure(t *testing.T) {
	s := open(t, 100, 20)
	s.src.candidates = review.BaseCandidates{
		Local: []review.Candidate{{Branch: "parent", Ahead: 1}},
	}
	s.press("b")
	s.src.wroteErr = errors.New("the branch moved")
	s.press("enter")

	s.press("p")
	if frame := s.frame(); strings.Contains(frame, "the branch moved") {
		t.Errorf("the failed ref's sentence outlived it:\n%s", frame)
	}
}

func TestTheBasePickerKeepsFailuresInTheModal(t *testing.T) {
	s := open(t, 100, 20)
	s.src.candidates = review.BaseCandidates{
		Local: []review.Candidate{{Branch: "parent", Ahead: 1}},
	}
	s.press("b")
	s.src.wroteErr = errors.New("the branch moved")
	s.press("enter")

	frame := s.frame()
	if !strings.Contains(frame, "the branch moved") || !strings.Contains(frame, "↑/↓ move") {
		t.Errorf("failed picker lost its error or input:\n%s", frame)
	}
}

func TestTheBasePickerReportsALoadFailureWithoutOpening(t *testing.T) {
	s := open(t, 100, 20)
	s.src.err = errors.New("git stopped")
	s.press("b")

	frame := s.frame()
	if !strings.Contains(frame, "git stopped") || strings.Contains(frame, "↑/↓ move") {
		t.Errorf("load failure did not stay on the frame:\n%s", frame)
	}
}

func TestZBOwnsTheBAtTheRoot(t *testing.T) {
	s := open(t, 100, 16).press("z", "b")
	if s.src.baseReads != 0 {
		t.Errorf("base reads = %d, want zb routed to the diff pane", s.src.baseReads)
	}
	if strings.Contains(s.frame(), "branch or revision") {
		t.Errorf("zb opened the base picker:\n%s", s.frame())
	}
}

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

func TestTheFactsAreDrawnAndColoured(t *testing.T) {
	th := testtheme.Dark
	s := open(t, 100, 16)

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

func TestTheBurnDownWearsItsOwnState(t *testing.T) {
	th := testtheme.Dark

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

	want := "├" + strings.Repeat("─", s.treeColumns()-2) + "┤"
	if !strings.HasPrefix(lines[rule], want) {
		t.Errorf("the rule is %q, want it to start %q", lines[rule], want)
	}
}

func TestThePadsBelongToTheEndsOfTheList(t *testing.T) {
	const height, first, last = 15, 1, 6

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

func TestTheHintIsAgainstTheLeftEdge(t *testing.T) {
	for _, width := range []int{100, 72, 56} {
		bar := open(t, width, 16).lines()[15]

		if !strings.HasPrefix(bar, "j/k") {
			t.Errorf("at %d columns the bar starts %q", width, bar)
		}
		if got := lipgloss.Width(bar); got != width {
			t.Errorf("at %d columns the bar is %d wide: %q", width, got, bar)
		}

		if !strings.Contains(bar, "? help") || !strings.Contains(bar, "q quit") {
			t.Errorf("at %d columns the bar lost the way out: %q", width, bar)
		}
	}
}

func TestTheBarSaysWhatThePaneHoldingTheKeysCanDo(t *testing.T) {
	tree := open(t, 100, 16).press("h").lines()[15]
	for _, want := range []string{"j/k move", "enter open", "space fold"} {
		if !strings.Contains(tree, want) {
			t.Errorf("the tree holds the keys and the bar does not say %q: %q", want, tree)
		}
	}

	diff := open(t, 100, 16).lines()[15]
	if !strings.Contains(diff, "j/k move") {
		t.Errorf("the diff holds the keys and the bar reads %q", diff)
	}
	if strings.Contains(diff, "space fold") {
		t.Errorf("the bar still names a key of the pane that lost the keys: %q", diff)
	}

	for _, bar := range []string{tree, diff} {
		if !strings.Contains(bar, "ctrl+d/u page") {
			t.Errorf("the bar drops the key that crosses the panes: %q", bar)
		}
	}
}

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

func TestADirectoryLeavesTheDiffPaneAlone(t *testing.T) {
	s := open(t, 100, 16).press("h", "j")
	if got := s.lines()[0]; !strings.Contains(got, "assets/logo.png") {
		t.Errorf("stepping onto a directory changed the diff pane: %q", got)
	}
}

func TestHelpTakesTheKeys(t *testing.T) {
	s := open(t, 100, 16)
	before := s.frame()

	s.press("?", "j", "j", "G")
	s.press("esc")

	if got := s.frame(); got != before {
		t.Errorf("keys reached the panes under the overlay:\n%s\nwant\n%s", got, before)
	}
}

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

func TestATerminalTooSmallSaysSo(t *testing.T) {
	s := open(t, 40, 10)
	if got := s.frame(); !strings.Contains(got, "the terminal is 40x10") {
		t.Errorf("a 40-column terminal drew a frame: %q", got)
	}
}

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

func heading(t *testing.T, s *screen) string {
	t.Helper()

	for _, line := range strings.Split(s.frame(), "\n") {
		if strings.Contains(line, mark) {
			return line
		}
	}
	return ""
}

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

func TestTheRingWrapsPastTheLastUnread(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

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

func TestTheHunkKeyCrossesIntoTheNextFileAndTheTreeFollows(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	s.press("}", "}")

	if title := s.lines()[0]; !strings.Contains(title, "b.go") {
		t.Errorf("} did not cross the file boundary: %q", title)
	}
	if got := filledTreeRow(t, s); !strings.Contains(got, "b.go") {
		t.Errorf("the tree's cursor is on %q, want it to follow the diff pane", got)
	}
}

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

func TestAFileWithNoHunksIsAStopOnTheRing(t *testing.T) {
	s := open(t, 100, 16)

	if title := s.lines()[0]; !strings.Contains(title, "assets/logo.png") {
		t.Fatalf("the reader did not open on the binary file: %q", title)
	}

	s.press("n", "n", "n", "n", "n")
	if title := s.lines()[0]; !strings.Contains(title, "assets/logo.png") {
		t.Errorf("n never came back to the binary file: %q", title)
	}
}

func TestTheMarkedHeadingIsFilledAndOneCellWide(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	if got := lipgloss.Width(mark); got != 1 {
		t.Errorf("the mark is %d cells wide, want 1", got)
	}

	marked := lipgloss.NewStyle().
		Background(testtheme.Dark.SelectedBackground).
		Foreground(testtheme.Dark.Accent).Render(mark)
	if !strings.Contains(s.raw(), marked) {
		t.Error("the heading the ring is on carries no fill")
	}
}

func TestTheTreeFollowsTheRingOffADirectoryRow(t *testing.T) {
	s := open(t, 100, 16).press(code...).press("k")

	if got := filledTreeRow(t, s); strings.Contains(got, "state.go") {
		t.Fatalf("the tree's cursor is still on the file, so this proves nothing: %q", got)
	}
	if title := s.lines()[0]; !strings.Contains(title, "state.go") {
		t.Fatalf("the directory row took the file out of the pane: %q", title)
	}

	s.press("}")
	if got := filledTreeRow(t, s); !strings.Contains(got, "state.go") {
		t.Errorf("the tree's cursor is on %q, want the file the ring moved inside", got)
	}
}

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

const movedPatch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -5,0 +6,1 @@
+one
@@ -20,0 +21,1 @@
+ten
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,0 +1,1 @@
+uno
@@ -10,0 +11,1 @@
+diez
`

const gonePatch = `diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,0 +1,1 @@
+uno
@@ -10,0 +11,1 @@
+diez
`

const renamedPatch = `diff --git a/a.go b/c.go
rename from a.go
rename to c.go
--- a/a.go
+++ b/c.go
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

func TestAReloadWithNoEditsLeavesTheReaderWhereTheyWere(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")

	before := s.lines()
	s.press("s")
	after := s.lines()

	for i := range before[:len(before)-1] {
		if before[i] != after[i] {
			t.Fatalf("a reload that changed nothing moved row %d:\n%q\n%q", i, before[i], after[i])
		}
	}
	if got := s.bar(); !strings.Contains(got, "up to date") {
		t.Errorf("the bar reads %q, want it to say the work tree had not moved", got)
	}
}

func TestAReloadWithNoEditsKeepsTheReaderWhereTheyScrolledTo(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 4).press("j", "j")

	before := s.lines()[1]
	s.press("s")

	if after := s.lines()[1]; after != before {
		t.Errorf("a reload that changed nothing scrolled the pane:\n%q\n%q", before, after)
	}
}

func TestAReloadLandsOnTheHunkThatTookThePlaceOfTheOldOne(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")

	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Fatalf("the reader is on %q, want a.go's second hunk", got)
	}

	s.reloading(testchangeset.Derive(t, movedPatch)).press("s")

	if got := heading(t, s); !strings.Contains(got, "@@ -20,0 +21,1 @@") {
		t.Errorf("the cursor landed on %q, want a.go's second hunk where it moved to", got)
	}
	if got := s.bar(); !strings.Contains(got, "a.go moved") {
		t.Errorf("the bar reads %q, want it to name the file that changed under the reader", got)
	}
}

func TestAReloadFollowsARename(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")
	s.reloading(testchangeset.Derive(t, renamedPatch)).press("s")

	if title := s.lines()[0]; !strings.Contains(title, "c.go") {
		t.Errorf("the pane shows %q, want the renamed file", title)
	}
	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Errorf("the cursor landed on %q, want the same hunk under the new name", got)
	}
}

func TestAReloadThatLostTheFileLandsWhereItWas(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")
	s.reloading(testchangeset.Derive(t, gonePatch)).press("s")

	if title := s.lines()[0]; !strings.Contains(title, "b.go") {
		t.Errorf("the pane shows %q, want the file that took a.go's place", title)
	}
	if heading(t, s) == "" {
		t.Error("the cursor landed nowhere, and every ring key from here would jump")
	}
	if got := s.bar(); !strings.Contains(got, "a.go is gone") {
		t.Errorf("the bar reads %q, want it to name the file that left", got)
	}
}

func TestAFailedReloadLeavesTheChangesetAlone(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")

	before := s.lines()
	s.src.err = errors.New("another zen-review refreshed this session first")
	s.press("s")

	after := s.lines()
	for i := range before[:len(before)-1] {
		if before[i] != after[i] {
			t.Fatalf("a failed reload moved row %d:\n%q\n%q", i, before[i], after[i])
		}
	}
	if got := s.bar(); !strings.Contains(got, "another zen-review refreshed this session first") {
		t.Errorf("the bar reads %q, want the reason the reload failed", got)
	}
}

func TestTheErrorIsSaidInTheThemesErrorColour(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.src.err = errors.New("no")
	s.press("s")

	want := lipgloss.NewStyle().Foreground(testtheme.Dark.Error).Render("no")
	if !strings.Contains(s.raw(), want) {
		t.Errorf("the failed reload does not wear the error colour:\n%q", s.raw())
	}
}

func TestASecondReloadWhileOneIsRunningDoesNothing(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	running := s.hold(keystroke("s"))
	if got := s.bar(); !strings.Contains(got, "reloading") {
		t.Errorf("the bar reads %q while a reload is in git", got)
	}

	if second := s.hold(keystroke("s")); second != nil {
		t.Error("a second s started a second reload")
	}
	if got := s.bar(); !strings.Contains(got, "reloading") {
		t.Errorf("the guarded press blanked the bar: %q", got)
	}

	s.drain(running)
	if s.src.calls != 1 {
		t.Errorf("the source was asked %d times, want once", s.src.calls)
	}
}

func TestTheNoticeClearsOnTheNextKey(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("s")

	if got := s.bar(); !strings.Contains(got, "up to date") {
		t.Fatalf("the bar reads %q after a reload", got)
	}
	if got := s.press("j").bar(); strings.Contains(got, "up to date") {
		t.Errorf("the notice outlived the press after it: %q", got)
	}
}

func TestTheFactsMoveWithTheGeneration(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.reloading(testchangeset.Derive(t, gonePatch)).press("s")

	if got := s.treeRow(10); !strings.Contains(got, "3") {
		t.Errorf("the generation reads %q after a reload that built one", got)
	}
	if got := s.treeRow(11); !strings.Contains(got, "0/2") {
		t.Errorf("the burn-down reads %q over a changeset of two hunks", got)
	}
}

func TestTheTreeKeepsAFoldedDirectoryAcrossAReload(t *testing.T) {
	s := open(t, 100, 16).press("h", "j", "space")

	if got := s.treeRow(4); !strings.Contains(got, "docs/superpowers/specs") {
		t.Fatalf("the cursor is not on the collapsed chain: %q", got)
	}
	if got := s.treeRow(5); strings.Contains(got, "design.md") {
		t.Fatalf("the chain did not fold: %q", got)
	}

	s.press("s")
	if got := s.treeRow(5); strings.Contains(got, "design.md") {
		t.Errorf("a reload unfolded what the reader had folded: %q", got)
	}
}

const twiceRenamedPatch = `diff --git a/a.go b/d.go
rename from a.go
rename to d.go
--- a/a.go
+++ b/d.go
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

func TestAReloadFollowsAFileRenamedTwice(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")
	s.reloading(testchangeset.Derive(t, renamedPatch)).press("s")

	if title := s.lines()[0]; !strings.Contains(title, "c.go") {
		t.Fatalf("the first rename put %q in the pane", title)
	}

	s.reloading(testchangeset.Derive(t, twiceRenamedPatch)).press("s")
	if title := s.lines()[0]; !strings.Contains(title, "d.go") {
		t.Errorf("the pane shows %q, want the file under its second new name", title)
	}
	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Errorf("the cursor landed on %q, want the same hunk it was on", got)
	}
	if got := s.bar(); strings.Contains(got, "is gone") {
		t.Errorf("the bar reads %q about a file that was renamed, not lost", got)
	}
}

const recreatedPatch = `diff --git a/a.go b/a.go
--- /dev/null
+++ b/a.go
@@ -0,0 +1,1 @@
+brand new
diff --git a/a.go b/c.go
rename from a.go
rename to c.go
--- a/a.go
+++ b/c.go
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

func TestARenameBeatsAFileWrittenBackUnderTheOldName(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16).press("}")
	s.reloading(testchangeset.Derive(t, recreatedPatch)).press("s")

	if title := s.lines()[0]; !strings.Contains(title, "c.go") {
		t.Errorf("the pane shows %q, want the file the reader's content moved to", title)
	}
}

func TestTheBarKeepsSayingAReloadIsRunning(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	running := s.hold(keystroke("s"))
	s.hold(keystroke("j"))

	if got := s.bar(); !strings.Contains(got, "reloading") {
		t.Errorf("a press while the reload was in git blanked the bar: %q", got)
	}
	s.drain(running)
}

func TestAFailedReloadStillFillsTheBar(t *testing.T) {
	long := "another zen-review refreshed this session first, so nothing was built: run it again"

	for _, width := range []int{200, 100, 72, 56} {
		s := over(t, testchangeset.Derive(t, ringPatch), width, 16)
		s.src.err = errors.New(long)
		s.press("s")

		if got := lipgloss.Width(s.bar()); got != width {
			t.Errorf("at %d columns the error bar is %d wide: %q", width, got, s.bar())
		}
	}
}

func TestAnEmptyChangesetSaysSoInBothPanes(t *testing.T) {
	base := review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"}

	frame := measured(t, base, review.Changeset{}, 100, 16).frame()

	for _, want := range []string{"no files changed", "nothing to review"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the frame is missing %q:\n%s", want, frame)
		}
	}
}

func TestAFallbackBaseReadsBesideTheBase(t *testing.T) {
	base := review.Base{Ref: "HEAD", SHA: "a1b2c3d4e5f67890", Fallback: "uncommitted"}

	s := measured(t, base, testchangeset.Derive(t, ringPatch), 100, 16)

	if want := "HEAD (uncommitted)"; !strings.Contains(s.frame(), want) {
		t.Errorf("the frame is missing %q:\n%s", want, s.frame())
	}
	if strings.Contains(s.bar(), "uncommitted") {
		t.Errorf("bar = %q, want the bar left to the keys", s.bar())
	}

	if !strings.Contains(s.press("j").frame(), "HEAD (uncommitted)") {
		t.Error("a press cleared the fallback, which is not a notice")
	}
}

func TestANarrowPaneClipsTheReasonAndKeepsTheRef(t *testing.T) {
	base := review.Base{
		Ref:      "feature",
		SHA:      "a1b2c3d4e5f67890",
		Fallback: "not origin/a-long-branch-name",
	}

	frame := measured(t, base, testchangeset.Derive(t, ringPatch), 56, 16).frame()

	if !strings.Contains(frame, "feature (") {
		t.Errorf("the frame lost the ref:\n%s", frame)
	}
	if strings.Contains(frame, "a-long-branch-name") {
		t.Errorf("the reason was not clipped at 56 columns:\n%s", frame)
	}
}

func TestTheEmptyTreeBaseIsNamedInTheFacts(t *testing.T) {
	base := review.Base{SHA: "4b825dc642cb6eb9a060e54bf8d69288fbee4904"}

	frame := measured(t, base, review.Changeset{}, 100, 16).frame()

	if !strings.Contains(frame, "empty tree") {
		t.Errorf("the frame does not name the base:\n%s", frame)
	}
}

func TestTheFallbackTagDoesNotReadAsAFactOnTheBar(t *testing.T) {
	base := review.Base{Ref: "HEAD", SHA: "a1b2c3d4e5f67890", Fallback: "uncommitted"}

	bar := measured(t, base, testchangeset.Derive(t, ringPatch), 200, 6).bar()

	if !strings.Contains(bar, "HEAD (uncommitted)") {
		t.Errorf("bar = %q, want the tag bracketed onto the ref", bar)
	}
	if strings.Contains(bar, "HEAD · uncommitted") {
		t.Errorf("bar = %q, want the tag not to read as its own fact", bar)
	}
}

func TestHStepsTheColumnBeforeItLeavesTheDiffPane(t *testing.T) {
	s := open(t, 120, 16).press("n", "n", "|")
	if !strings.Contains(s.raw(), seam(false)) {
		t.Fatal("the diff pane does not have the focus to begin with")
	}

	s.press("h")
	if !strings.Contains(s.raw(), seam(false)) {
		t.Error("h left the diff pane instead of stepping into the base column")
	}

	s.press("h")
	if !strings.Contains(s.raw(), seam(true)) {
		t.Error("a second h did not hand the focus to the tree")
	}

	s.press("l")
	if !strings.Contains(s.raw(), seam(false)) {
		t.Fatal("l did not come back to the diff pane")
	}
	s.press("l")
	if !strings.Contains(s.raw(), seam(false)) {
		t.Error("l stepping to the head column gave the focus away")
	}
}

func TestTheBadgeIsAJumpNotAStep(t *testing.T) {
	s := open(t, 120, 16).press("n", "n", "|")

	s.press("1")
	if !strings.Contains(s.raw(), seam(true)) {
		t.Error("1 stepped a column instead of jumping to the tree")
	}

	s.press("2")
	if !strings.Contains(s.raw(), seam(false)) {
		t.Error("2 did not jump to the diff pane")
	}
}

func TestAUnifiedPaneStillGivesTheFocusUpOnTheFirstH(t *testing.T) {
	s := open(t, 120, 16).press("n", "n")

	s.press("h")
	if !strings.Contains(s.raw(), seam(true)) {
		t.Error("h did not hand the focus to the tree in a unified pane")
	}
}

func TestTheToggleLeavesTheCursorWhereTheCaretIs(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 120, 16)

	s.press("}")
	if got := barred(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Fatalf("the cursor is on %q, want a.go's second hunk heading", got)
	}

	s.press("|")
	if got := barred(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Errorf("the toggle took the cursor to %q, want a.go's second hunk", got)
	}

	s.press("r")
	want := []string{"MarkHunk a.go head:11 gen=2"}
	if got := s.calls(); !equal(got, want) {
		t.Errorf("r wrote %v, want %v", got, want)
	}
}

func barred(t *testing.T, s *screen) string {
	t.Helper()

	for _, line := range strings.Split(s.frame(), "\n") {
		if strings.Contains(line, "▌") {
			return line
		}
	}
	return ""
}
