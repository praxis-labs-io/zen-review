package app_test

import (
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
