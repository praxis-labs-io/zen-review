package app_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
)

func TestTheCommentRingCrossesFiles(t *testing.T) {
	s := commented(t, 100, 24, testchangeset.NestedComments()...)

	s.press("]")
	if got := s.title(); !strings.Contains(got, "README.md") {
		t.Fatalf("the first ] opened %q", got)
	}

	s.press("]")
	if got := s.title(); !strings.Contains(got, "state.go") {
		t.Errorf("the second ] opened %q, want the next file", got)
	}
	if got := s.frame(); !strings.Contains(got, "unreviewed is the longer word") {
		t.Errorf("the card it landed on is not on screen:\n%s", got)
	}
}

func TestTheCommentRingSkipsAResolvedOne(t *testing.T) {
	settled := testchangeset.In(
		testchangeset.Comment("ffffffffffff", "README.md", 3, 3, "settled and gone"),
		store.CommentResolved)
	live := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "still wants an answer")

	s := commented(t, 100, 24, live, settled)
	for range 3 {
		s.press("]")
	}

	if got := s.frame(); !strings.Contains(got, "space fold") {
		t.Fatalf("the ring never landed on the open comment:\n%s", got)
	}

	if got := s.frame(); strings.Contains(got, "space open") {
		t.Errorf("the ring landed on the resolved card:\n%s", got)
	}
}

func TestTheCommentRingWraps(t *testing.T) {
	first := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "the first one")
	second := testchangeset.Comment("bbbbbbbbbbbb", "internal/review/state.go", 13, 13, "the second one")

	s := commented(t, 100, 24, first, second)
	s.press("]", "]", "]")

	if got := s.title(); !strings.Contains(got, "README.md") {
		t.Errorf("three presses over two comments left the pane on %q", got)
	}
}

func TestTheRingFollowsACardIntoItsHunk(t *testing.T) {
	on := testchangeset.Comment("cccccccccccc", "internal/review/state.go", 124, 125, "the second hunk")

	s := commented(t, 100, 24, on)
	s.press("]", "r")

	want := "MarkHunk internal/review/state.go head:124 gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the marks were %v, want the hunk the card is in", got)
	}
}

func TestACardFoldsFromTheReader(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "worth folding away")

	s := commented(t, 100, 24, on)
	s.press("]")

	if got := s.frame(); !strings.Contains(got, "╭─ ◇ open") {
		t.Fatalf("the card did not open bordered:\n%s", got)
	}

	s.press("space")
	got := s.frame()
	if !strings.Contains(got, "▸ worth folding away") {
		t.Errorf("space did not fold the card to its one row:\n%s", got)
	}
	if !strings.Contains(got, "space open") {
		t.Errorf("the folded card names the fold rather than the way out of it:\n%s", got)
	}

	if !strings.Contains(got, "╭─ ◇ open") {
		t.Errorf("the folded card lost its box:\n%s", got)
	}
}

func TestTheFactsCountTheComments(t *testing.T) {
	s := commented(t, 100, 24, testchangeset.NestedComments()...)

	if got := s.treeRow(20); !strings.Contains(got, "Comments") || !strings.Contains(got, "1/6") {
		t.Errorf("the facts read %q, want one of six answered", got)
	}
}

func TestTheRingLeavesTheFileWithACardOutsideEveryHunk(t *testing.T) {
	whole := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 0, 0, "the file itself")

	s := commented(t, 100, 24, whole)
	s.press("tab", "]", "r")

	want := "MarkHunk README.md head:3 gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the marks were %v, want %q", got, want)
	}
}

func TestTheCommentRingSkipsAFileTheChangesetLost(t *testing.T) {
	gone := testchangeset.Comment("aaaaaaaaaaaa", "reverted.go", 4, 4, "its file went away")
	live := testchangeset.Comment("bbbbbbbbbbbb", "README.md", 2, 2, "this one is still here")

	s := commented(t, 100, 24, gone, live)
	s.press("]")

	if got := s.frame(); !strings.Contains(got, "this one is still here") {
		t.Errorf("] never reached the comment it could show:\n%s", got)
	}
}

const mixedPatch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 one
-two
+dos
 three
`

func wrote(t *testing.T, s *screen) string {
	t.Helper()

	got := s.calls()
	if len(got) != 1 {
		t.Fatalf("the writes were %v, want the one comment", got)
	}
	return got[0]
}

func TestCScopesToWhatIsUnderTheCursor(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys []string
		want string
	}{
		{"a hunk heading", nil, `AddComment a.go head:2-2 range "x" gen=2`},
		{"a code row", []string{"j"}, `AddComment a.go head:1-1 line "x" gen=2`},
		{"a removal", []string{"j", "j"}, `AddComment a.go base:2-2 line "x" gen=2`},

		{"a selection", []string{"j", "v", "j", "j"}, `AddComment a.go head:1-2 range "x" gen=2`},
		{"a selection of removals", []string{"j", "j", "v"}, `AddComment a.go base:2-2 line "x" gen=2`},

		{"the tree", []string{"h"}, `AddComment a.go file "x" gen=2`},
		{"the tree over a selection", []string{"j", "v", "j", "j", "h"},
			`AddComment a.go head:1-2 range "x" gen=2`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press(tt.keys...)
			s.press("c", "x", "ctrl+s")

			if got := wrote(t, s); got != tt.want {
				t.Errorf("c wrote %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCOnAFileWithNoHunksCommentsOnTheFile(t *testing.T) {
	s := open(t, 100, 24)
	if title := s.title(); !strings.Contains(title, "assets/logo.png") {
		t.Fatalf("the reader did not open on the binary file: %q", title)
	}

	s.press("c", "x", "ctrl+s")
	want := `AddComment assets/logo.png file "x" gen=2`
	if got := wrote(t, s); got != want {
		t.Errorf("c wrote %q, want %q", got, want)
	}
}

func TestCOnADirectoryRowDoesNothing(t *testing.T) {
	s := open(t, 100, 24).press("h", "j")

	before := s.frame()
	s.press("c")

	if got := s.frame(); got != before {
		t.Errorf("c on a directory row opened something:\n%s", got)
	}
	if got := s.calls(); len(got) != 0 {
		t.Errorf("c on a directory row wrote %v", got)
	}
}

func TestTheBoxHangsWhereTheCardWill(t *testing.T) {
	for _, tt := range []struct {
		name  string
		keys  []string
		label string
		under string
	}{
		{"one line", []string{"j"}, "◇ new", "one"},

		{"a range", []string{"j", "v", "j", "j"}, "◇ new · lines 1-2", "dos"},

		{"a removal", []string{"j", "j"}, "◇ new", "two"},
		{"the file", []string{"h"}, "◇ new · file", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press(tt.keys...)
			s.press("c")

			lines := s.lines()
			at := -1
			for i, line := range lines {
				if strings.Contains(line, tt.label) {
					at = i
					break
				}
			}
			if at < 0 {
				t.Fatalf("no row carries the box labelled %q:\n%s", tt.label, s.frame())
			}
			if !strings.Contains(lines[at], "╭─") {
				t.Errorf("the label is not a box's top border: %q", lines[at])
			}
			if tt.under != "" && !strings.Contains(lines[at-1], tt.under) {
				t.Errorf("the box hangs under %q, want the line holding %q", lines[at-1], tt.under)
			}
		})
	}
}

func TestTheBarNamesTheBoxsKeysAndNothingElse(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j", "c")

	got := s.bar()
	for _, want := range []string{"ctrl+s save", "esc discard"} {
		if !strings.Contains(got, want) {
			t.Errorf("the bar reads %q, want it to name %q", got, want)
		}
	}
	for _, gone := range []string{"q quit", "? help", "j/k move"} {
		if strings.Contains(got, gone) {
			t.Errorf("the bar still offers %q while the box has the keys: %q", gone, got)
		}
	}
}

func TestAnEmptyCommentWritesNothing(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24)
	s.press("c", " ", "ctrl+s")

	if got := s.calls(); len(got) != 0 {
		t.Fatalf("an empty comment wrote %v", got)
	}
	if got := s.frame(); strings.Contains(got, "◇ new") {
		t.Errorf("the box stayed up over nothing to save:\n%s", got)
	}
}

func TestDiscardingACommentWritesNothing(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24)
	s.press("c", "n", "o", "esc")

	if got := s.calls(); len(got) != 0 {
		t.Fatalf("esc wrote %v", got)
	}

	s.press("c")
	if got := s.frame(); strings.Contains(got, "no") {
		t.Errorf("the discarded words came back:\n%s", got)
	}
}

func TestAFailedCommentKeepsTheWordsAndTheAim(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j", "v", "j", "j")
	s.src.wroteErr = errors.New("the database is locked")
	s.press("c", "h", "i", "ctrl+s")

	got := s.frame()
	if !strings.Contains(got, "◇ new · lines 1-2") {
		t.Fatalf("the box came down on a write that did not land:\n%s", got)
	}
	if !strings.Contains(got, "hi") {
		t.Errorf("the words went with it:\n%s", got)
	}

	s.src.wroteErr = nil
	s.press("ctrl+s")

	want := `AddComment a.go head:1-2 range "hi" gen=2`
	if got := wrote(t, s); got != want {
		t.Errorf("the retry wrote %q, want %q", got, want)
	}
}

func TestASavedCommentReportsItselfAndComesBackAsACard(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j")
	s.resolving(testchangeset.Comment("aaaaaaaaaaaa", "a.go", 1, 1, "why one"))
	s.press("c", "x", "ctrl+s")

	got := s.frame()
	if strings.Contains(got, "◇ new") {
		t.Fatalf("the box stayed up after the write:\n%s", got)
	}
	if !strings.Contains(got, "why one") {
		t.Errorf("the card the write left is not on screen:\n%s", got)
	}
	if bar := s.bar(); !strings.Contains(bar, "comment saved") {
		t.Errorf("the bar reads %q, want the write reported", bar)
	}
}

func TestCIsRefusedWhileAReloadIsOut(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24)

	running := s.hold(keystroke("s"))
	s.press("c")

	if got := s.frame(); strings.Contains(got, "◇ new") {
		t.Errorf("the box came up over a reload still in git:\n%s", got)
	}
	if got := s.bar(); !strings.Contains(got, "reloading") {
		t.Errorf("the bar reads %q, want it still saying what is happening", got)
	}

	s.drain(running)
	s.press("c")

	if got := s.frame(); !strings.Contains(got, "◇ new") {
		t.Errorf("c did nothing after the reload landed:\n%s", got)
	}
}

func TestCFallsBackToTheBoxOverTheFrame(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 50, 10)
	s.press("c")

	if got := s.frame(); !strings.Contains(got, "Comment on a.go") {
		t.Fatalf("c opened nothing on a frame with no room beside the code:\n%s", got)
	}

	s.press("h", "i", "ctrl+s")
	want := `AddComment a.go head:2-2 range "hi" gen=2`
	if got := wrote(t, s); got != want {
		t.Errorf("the box over the frame wrote %q, want %q", got, want)
	}
}

func TestAFrameTooSmallForTheBoxTakesItOverTheFrame(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j", "c", "h", "i")

	if got := s.frame(); !strings.Contains(got, "◇ new") {
		t.Fatalf("the box did not open beside the code:\n%s", got)
	}

	s.send(tea.WindowSizeMsg{Width: 50, Height: 10})

	got := s.frame()
	if !strings.Contains(got, "Comment on a.go:1") {
		t.Fatalf("the box went with the room for it:\n%s", got)
	}
	if !strings.Contains(got, "hi") {
		t.Errorf("the words went with it:\n%s", got)
	}

	s.press("ctrl+s")
	want := `AddComment a.go head:1-1 line "hi" gen=2`
	if got := wrote(t, s); got != want {
		t.Errorf("the box that moved wrote %q, want %q", got, want)
	}
}

func TestASaveThatLandedTakesTheBoxDown(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j", "c", "h", "i")

	saving := s.hold(keystroke("ctrl+s"))
	s.press("!")
	s.drain(saving)

	if got := s.frame(); strings.Contains(got, "◇ new") {
		t.Fatalf("the box outlived the write:\n%s", got)
	}

	s.press("ctrl+s")
	if got := s.calls(); len(got) != 1 {
		t.Errorf("the writes were %v, want the one comment", got)
	}
}

func TestAWriteSavedAndNotReadBackTakesTheBoxDown(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j", "c", "h", "i")
	s.src.wroteErr = fmt.Errorf("%w: the database is locked", app.ErrSaved)
	s.press("ctrl+s")

	if got := s.frame(); strings.Contains(got, "◇ new") {
		t.Errorf("the box stayed up over a write that landed:\n%s", got)
	}
	if got := s.bar(); !strings.Contains(got, "the screen is behind it") {
		t.Errorf("the bar reads %q, want it to say the write was saved", got)
	}
}

func TestARefusedWriteNamesAKeyTheBoxWouldEat(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j", "c", "h", "i")
	s.src.wroteErr = &review.StaleGenerationError{Seq: 2, Current: 3}
	s.press("ctrl+s")

	if got := s.bar(); !strings.Contains(got, "esc, then s") {
		t.Errorf("the bar reads %q, want the keys that reach the reload", got)
	}
}

func tallPatch() string {
	var b strings.Builder
	b.WriteString("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,40 +1,40 @@\n")
	for i := 1; i <= 40; i++ {
		if i%5 == 0 {
			fmt.Fprintf(&b, "-old line %d\n+new line %d\n", i, i)
			continue
		}
		fmt.Fprintf(&b, " line %d\n", i)
	}
	return b.String()
}

func TestTheBoxOnATallHunkIsOnScreen(t *testing.T) {
	s := over(t, testchangeset.Derive(t, tallPatch()), 100, 24)
	s.press("c")

	got := s.frame()
	if !strings.Contains(got, "◇ new") {
		t.Fatalf("the box is off the window:\n%s", got)
	}
	if !strings.Contains(got, "ctrl+s save") {
		t.Errorf("the box is cut off at the bottom:\n%s", got)
	}
}

func TestTheRingLandsOnACardBelowATallHunk(t *testing.T) {
	deep := testchangeset.Comment("aaaaaaaaaaaa", "a.go", 5, 40, "this hunk is the whole file")

	s := with(t, "zen-review", testchangeset.Derive(t, tallPatch()),
		[]store.Comment{deep}, "", 100, 24)
	s.press("]")

	if got := s.frame(); !strings.Contains(got, "this hunk is the whole file") {
		t.Errorf("the card the ring landed on is off the window:\n%s", got)
	}
}

func TestExpandReachesTheCardFromTheTree(t *testing.T) {
	s := replacing(t, 100, 24, "one", "two", "three", "four")
	s.press("]", "]", "]", "]")

	if got := s.frame(); !strings.Contains(got, "… 1 more") {
		t.Fatalf("the block is not truncated, so this proves nothing:\n%s", got)
	}

	s.press("h", ">")
	if got := s.frame(); !strings.Contains(got, "four") {
		t.Errorf("> did not reach the card from the tree pane:\n%s", got)
	}
}
