package app_test

import (
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/testchangeset"
)

// answered is the same comment settled, which is what the engine hands back
// after x and what the source has to answer the next reload with.
func answered(c store.Comment) store.Comment {
	return testchangeset.In(c, store.CommentResolved)
}

// TestResolveNamesTheCardTheCursorIsOn. A golden cannot show that x named the
// right comment, and that is the whole of what the key does.
func TestResolveNamesTheCardTheCursorIsOn(t *testing.T) {
	first := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "the first one")
	second := testchangeset.Comment("bbbbbbbbbbbb", "internal/review/state.go", 13, 13, "the second one")

	s := commented(t, 100, 24, first, second)
	s.press("]", "]", "x")

	want := "ResolveComment bbbbbbbbbbbb gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want %q", got, want)
	}
}

// TestResolveOnNoCardWritesNothing. The reader opens on a hunk, so most presses
// of this key have nothing to act on rather than a refusal to report.
func TestResolveOnNoCardWritesNothing(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "somewhere else")

	s := commented(t, 100, 24, on)
	s.press("x")

	if got := s.calls(); len(got) != 0 {
		t.Errorf("x on no card wrote %v", got)
	}
	if got := s.bar(); strings.Contains(got, "resolving") {
		t.Errorf("the bar reads %q, want a press that did nothing to say nothing", got)
	}
}

// TestTheCursorStaysOnTheCardItSettled. A card that folded is not the height it
// was, so a cursor put back by row would land on the code under it.
func TestTheCursorStaysOnTheCardItSettled(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "answer this one")

	s := commented(t, 100, 24, on)
	s.resolving(answered(on))
	s.press("]", "x")

	got := s.frame()
	if !strings.Contains(got, "◆ resolved") {
		t.Fatalf("the card did not come back settled:\n%s", got)
	}

	// Only a lit card names its keys, and a folded one names the way out of the
	// fold. So this is the cursor still on the card it acted on.
	if !strings.Contains(got, "space open") {
		t.Errorf("the cursor left the card it settled:\n%s", got)
	}
	if strings.Contains(got, "x resolve") {
		t.Errorf("a settled card still offers the key that settled it:\n%s", got)
	}
	if bar := s.bar(); !strings.Contains(bar, "1/1 settled") {
		t.Errorf("the bar reads %q, want how far down the comments the write left", bar)
	}
}

// TestResolveIsNotRefusedByTheReader. The engine owns the state ladder, and a
// second copy of it here is a second thing to keep true.
func TestResolveIsNotRefusedByTheReader(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "answer this one")

	s := commented(t, 100, 24, on)
	s.resolving(answered(on))
	s.press("]", "x", "x")

	if got := s.calls(); len(got) != 2 {
		t.Errorf("the writes were %v, want the second press through to the engine", got)
	}
}

// TestAnOrphanIsStillTheReadersToSettle. The code it was about is gone, and
// saying so is the only thing left that anyone can do with it.
func TestAnOrphanIsStillTheReadersToSettle(t *testing.T) {
	lost := testchangeset.In(
		testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "the code went away"),
		store.CommentOrphaned)

	s := commented(t, 100, 24, lost)
	s.press("]")

	if got := s.frame(); !strings.Contains(got, "x resolve") {
		t.Fatalf("an orphaned card does not offer the key:\n%s", got)
	}

	s.press("x")
	want := "ResolveComment aaaaaaaaaaaa gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want %q", got, want)
	}
}
