package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/testchangeset"
)

// TestTheCommentRingCrossesFiles. The comments are one list over the whole
// changeset, so ] walks off the end of a file the way n walks off a hunk.
func TestTheCommentRingCrossesFiles(t *testing.T) {
	s := commented(t, 100, 24, testchangeset.NestedComments()...)

	// The first is on README.md and the next two are on state.go.
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

// TestTheCommentRingSkipsAResolvedOne. A review is a burn-down and this ring is
// the comments' half of it, the way n is the hunks'.
func TestTheCommentRingSkipsAResolvedOne(t *testing.T) {
	settled := testchangeset.In(
		testchangeset.Comment("ffffffffffff", "README.md", 3, 3, "settled and gone"),
		store.CommentResolved)
	live := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "still wants an answer")

	s := commented(t, 100, 24, live, settled)
	for range 3 {
		s.press("]")
	}

	// The hints, not the body: both cards draw their body whether lit or not, so
	// only what a lit card names says which one the ring landed on.
	if got := s.frame(); !strings.Contains(got, "space fold") {
		t.Fatalf("the ring never landed on the open comment:\n%s", got)
	}

	// Only a lit card names its keys, and the folded one names the way out of
	// its fold. So the resolved card was never landed on.
	if got := s.frame(); strings.Contains(got, "space open") {
		t.Errorf("the ring landed on the resolved card:\n%s", got)
	}
}

// TestTheCommentRingWraps, so a reader holding ] walks the queue round rather
// than stopping on the last one with no way to say so.
func TestTheCommentRingWraps(t *testing.T) {
	first := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "the first one")
	second := testchangeset.Comment("bbbbbbbbbbbb", "internal/review/state.go", 13, 13, "the second one")

	s := commented(t, 100, 24, first, second)
	s.press("]", "]", "]")

	if got := s.title(); !strings.Contains(got, "README.md") {
		t.Errorf("three presses over two comments left the pane on %q", got)
	}
}

// TestTheRingFollowsACardIntoItsHunk, so r after ] marks the hunk the card is
// in rather than whichever one the reader was on before.
func TestTheRingFollowsACardIntoItsHunk(t *testing.T) {
	on := testchangeset.Comment("cccccccccccc", "internal/review/state.go", 124, 125, "the second hunk")

	s := commented(t, 100, 24, on)
	s.press("]", "r")

	want := "MarkHunk internal/review/state.go head:124 gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the marks were %v, want the hunk the card is in", got)
	}
}

// TestACardFoldsFromTheReader. space is the tree's fold key doing the tree's
// job on the one other thing on screen that can be folded away.
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

	// The box stays. Without one a folded card is a line of grey text in a
	// column of diff, which is what the diff's own notes look like.
	if !strings.Contains(got, "╭─ ◇ open") {
		t.Errorf("the folded card lost its box:\n%s", got)
	}
}

// TestTheFactsCountTheComments. Nothing else on screen says one exists before
// the reader scrolls into a card.
func TestTheFactsCountTheComments(t *testing.T) {
	s := commented(t, 100, 24, testchangeset.NestedComments()...)

	if got := s.treeRow(20); !strings.Contains(got, "Comments") || !strings.Contains(got, "1/6") {
		t.Errorf("the facts read %q, want one of six answered", got)
	}
}

// TestTheRingLeavesTheFileWithACardOutsideEveryHunk. A file comment and a stray
// sit outside every hunk, and a ring left where it was would mark a hunk in the
// file the reader just left.
func TestTheRingLeavesTheFileWithACardOutsideEveryHunk(t *testing.T) {
	whole := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 0, 0, "the file itself")

	// Off in state.go, so a ring left alone would still be pointed there.
	s := commented(t, 100, 24, whole)
	s.press("tab", "]", "r")

	want := "MarkHunk README.md head:3 gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the marks were %v, want %q", got, want)
	}
}

// TestTheCommentRingSkipsAFileTheChangesetLost. A comment whose file was
// reverted out has nowhere to draw, and in the ring it is a stop that lands on
// nothing and never lets the key move past it.
func TestTheCommentRingSkipsAFileTheChangesetLost(t *testing.T) {
	gone := testchangeset.Comment("aaaaaaaaaaaa", "reverted.go", 4, 4, "its file went away")
	live := testchangeset.Comment("bbbbbbbbbbbb", "README.md", 2, 2, "this one is still here")

	s := commented(t, 100, 24, gone, live)
	s.press("]")

	if got := s.frame(); !strings.Contains(got, "this one is still here") {
		t.Errorf("] never reached the comment it could show:\n%s", got)
	}
}

// mixedPatch is one hunk that both removes and adds, so a selection has a side
// to pick and a removal has one of its own.
const mixedPatch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 one
-two
+dos
 three
`

// wrote is the one comment a press of c wrote, and fails on anything else.
func wrote(t *testing.T, s *screen) string {
	t.Helper()

	got := s.calls()
	if len(got) != 1 {
		t.Fatalf("the writes were %v, want the one comment", got)
	}
	return got[0]
}

// TestCScopesToWhatIsUnderTheCursor. The scope is the whole of what the key
// decides, and the ladder is what tells a line from a hunk from a file.
func TestCScopesToWhatIsUnderTheCursor(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys []string
		want string
	}{
		{"a hunk heading", nil, `AddComment a.go head:2-2 range "x" gen=2`},
		{"a code row", []string{"j"}, `AddComment a.go head:1-1 line "x" gen=2`},
		{"a removal", []string{"j", "j"}, `AddComment a.go base:2-2 line "x" gen=2`},

		// The head wherever the lines have one, because that is the code the next
		// agent rewrites. A selection of removals has none.
		{"a selection", []string{"j", "v", "j", "j"}, `AddComment a.go head:1-2 range "x" gen=2`},
		{"a selection of removals", []string{"j", "j", "v"}, `AddComment a.go base:2-2 line "x" gen=2`},

		// The tree names the file, and a selection still open in the pane beside
		// it is what c comments on from either.
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

// TestCOnAFileWithNoHunksCommentsOnTheFile. A binary file is one thing to read
// and one thing to comment on, and it has no line to hang a card under.
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

// TestCOnADirectoryRowDoesNothing. There is no file under the cursor, so the
// press has nothing to act on rather than something to refuse.
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

// TestTheBoxHangsWhereTheCardWill, which is what says where the comment lands.
// A box under one line needs no number: the gutter beside it has one.
func TestTheBoxHangsWhereTheCardWill(t *testing.T) {
	for _, tt := range []struct {
		name  string
		keys  []string
		label string
		under string
	}{
		{"one line", []string{"j"}, "◇ new", "one"},

		// The label says the run, because a box covering four lines cannot be
		// read off the one row it hangs under.
		{"a range", []string{"j", "v", "j", "j"}, "◇ new · lines 1-2", "dos"},

		// Under the removal it is about, which is what says it is on the base.
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

// TestTheBarNamesTheBoxsKeysAndNothingElse. It takes q and ? too, so a bar
// still offering the way out would be naming two keystrokes.
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

// TestAnEmptyCommentWritesNothing and takes the box down. The engine refuses
// one, and nothing typed is nothing the key can cost.
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

// TestDiscardingACommentWritesNothing, and the next c comes up empty rather
// than holding what was thrown away.
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

// TestAFailedCommentKeepsTheWordsAndTheAim. The only thing a local transaction
// can cost is what was typed into it, and the lines it was pointed at.
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

	// And the retry writes them, against the lines the first press was aimed at.
	s.src.wroteErr = nil
	s.press("ctrl+s")

	want := `AddComment a.go head:1-2 range "hi" gen=2`
	if got := wrote(t, s); got != want {
		t.Errorf("the retry wrote %q, want %q", got, want)
	}
}

// TestASavedCommentReportsItselfAndComesBackAsACard, which is the write going
// through the same seam every other one does.
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

// TestCIsRefusedWhileAReloadIsOut. The generation would land under the open box
// and move the lines it was scoped to, and the box cannot be re-aimed.
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

	// And once it has landed the key works, so the refusal costs one press.
	s.drain(running)
	s.press("c")

	if got := s.frame(); !strings.Contains(got, "◇ new") {
		t.Errorf("c did nothing after the reload landed:\n%s", got)
	}
}

// TestCIsRefusedOnAFrameWithNoRoomForTheBox. It takes every key while it is up,
// so one nobody can see is a reader typing into nothing.
func TestCIsRefusedOnAFrameWithNoRoomForTheBox(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 50, 10)

	before := s.frame()
	s.press("c", "h", "i", "ctrl+s")

	if got := s.calls(); len(got) != 0 {
		t.Errorf("a box nobody could see wrote %v", got)
	}
	if got := s.frame(); got != before {
		t.Errorf("c opened something on a frame with no room:\n%s", got)
	}
}
