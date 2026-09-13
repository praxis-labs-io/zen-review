package app_test

import (
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
)

func answered(c store.Comment) store.Comment {
	return testchangeset.In(c, store.CommentResolved)
}

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

func TestTheCursorStaysOnTheCardItSettled(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "answer this one")

	s := commented(t, 100, 24, on)
	s.resolving(answered(on))
	s.press("]", "x")

	got := s.frame()
	if !strings.Contains(got, "◆ resolved") {
		t.Fatalf("the card did not come back settled:\n%s", got)
	}

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

func TestASettledCardTakesNoSecondPress(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "answer this one")

	s := commented(t, 100, 24, on)
	s.resolving(answered(on))
	s.press("]", "x", "x")

	if got := s.calls(); len(got) != 1 {
		t.Fatalf("the writes were %v, want the second press to have nothing to do", got)
	}
	if got := s.bar(); strings.Contains(got, "cannot be marked") {
		t.Errorf("the bar reads %q, want a press with nothing to do to say nothing", got)
	}
}

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
