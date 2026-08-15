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

	if got := s.frame(); !strings.Contains(got, "○ open") {
		t.Fatalf("the ring never landed on the open comment:\n%s", got)
	}
	if strings.Contains(s.frame(), "╭─ ● resolved") {
		t.Errorf("the ring opened the resolved card:\n%s", s.frame())
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

	if got := s.frame(); !strings.Contains(got, "╭─ ○ open") {
		t.Fatalf("the card did not open bordered:\n%s", got)
	}

	s.press("space")
	got := s.frame()
	if strings.Contains(got, "╭─ ○ open") {
		t.Errorf("space left the card bordered:\n%s", got)
	}
	if !strings.Contains(got, "○ open · worth folding away") {
		t.Errorf("the folded row does not say which comment it is:\n%s", got)
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
