package app_test

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/testchangeset"
)

// TestEditRewritesTheCardTheCursorIsOn. A golden cannot show that e named the
// right comment, and that is the whole of what the key does.
func TestEditRewritesTheCardTheCursorIsOn(t *testing.T) {
	first := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "the first one")
	second := testchangeset.Comment("bbbbbbbbbbbb", "internal/review/state.go", 13, 13, "hi")

	s := commented(t, 100, 24, first, second)
	s.press("]", "]", "e", "!", "ctrl+s")

	want := `EditComment bbbbbbbbbbbb "hi!" gen=2`
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want %q", got, want)
	}
}

// TestTheBoxOpensHoldingWhatTheCardSaid, so a typo is fixed rather than retyped.
func TestTheBoxOpensHoldingWhatTheCardSaid(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "unreviewd is the clearer word")

	s := commented(t, 100, 24, on)
	s.press("]", "e")

	got := s.frame()
	if !strings.Contains(got, "◇ editing") {
		t.Fatalf("the box does not say what it is doing:\n%s", got)
	}
	if !strings.Contains(got, "unreviewd is the clearer word") {
		t.Errorf("the box does not hold what the card said:\n%s", got)
	}
	for _, want := range []string{"ctrl+s save", "esc discard"} {
		if !strings.Contains(s.bar(), want) {
			t.Errorf("the bar reads %q, want it to name %q", s.bar(), want)
		}
	}
}

// TestEditOnNoCardOpensNothing. The reader opens on a hunk, so most presses of
// this key have nothing to act on rather than a refusal to report.
func TestEditOnNoCardOpensNothing(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "somewhere else")

	s := commented(t, 100, 24, on)
	s.press("e", "h", "i", "ctrl+s")

	if got := s.calls(); len(got) != 0 {
		t.Errorf("e on no card wrote %v", got)
	}
	if got := s.frame(); strings.Contains(got, "editing") {
		t.Errorf("a box opened over no card:\n%s", got)
	}
}

// TestAnEmptyEditGoesThroughToBeRefused. Blanking a comment that says something
// is a delete, and D is how that is spelled.
func TestAnEmptyEditGoesThroughToBeRefused(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "hi")

	s := commented(t, 100, 24, on)
	s.press("]", "e", "backspace", "backspace", "ctrl+s")

	want := `EditComment aaaaaaaaaaaa "" gen=2`
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want %q", got, want)
	}
}

// TestAFailedEditKeepsTheBoxAndTheWords. The only thing a local transaction can
// cost is what was typed into it.
func TestAFailedEditKeepsTheBoxAndTheWords(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "hi")

	s := commented(t, 100, 24, on)
	s.src.wroteErr = errors.New("the database is locked")
	s.press("]", "e", "!", "ctrl+s")

	got := s.frame()
	if !strings.Contains(got, "◇ editing") {
		t.Fatalf("the box came down on a write that failed:\n%s", got)
	}
	if !strings.Contains(got, "hi!") {
		t.Errorf("the words are gone:\n%s", got)
	}
}

// TestEditReachesAnOrphanedCard. A comment whose code went is still a comment
// somebody wrote, and a typo in it is still a typo.
func TestEditReachesAnOrphanedCard(t *testing.T) {
	lost := testchangeset.In(
		testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "hi"), store.CommentOrphaned)

	s := commented(t, 100, 24, lost)
	s.press("]", "e", "!", "ctrl+s")

	want := `EditComment aaaaaaaaaaaa "hi!" gen=2`
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want %q", got, want)
	}
}

// TestDeleteNamesTheCardTheCursorIsOn, and acts at once: the capital does the
// whole of the thing, the way R and U do.
func TestDeleteNamesTheCardTheCursorIsOn(t *testing.T) {
	first := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "the first one")
	second := testchangeset.Comment("bbbbbbbbbbbb", "internal/review/state.go", 13, 13, "the second one")

	s := commented(t, 100, 24, first, second)
	s.resolving(first)
	s.press("]", "]", "D")

	want := "DeleteComment bbbbbbbbbbbb gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Fatalf("the writes were %v, want %q", got, want)
	}

	got := s.frame()
	if strings.Contains(got, "the second one") {
		t.Errorf("the card is still drawn:\n%s", got)
	}
	if bar := s.bar(); !strings.Contains(bar, "comment deleted") {
		t.Errorf("the bar reads %q, want what the key did", bar)
	}
}

// TestDeleteOnNoCardWritesNothing, and says nothing: a press with nothing under
// it has nothing to do rather than something to refuse.
func TestDeleteOnNoCardWritesNothing(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "somewhere else")

	s := commented(t, 100, 24, on)
	s.press("D")

	if got := s.calls(); len(got) != 0 {
		t.Errorf("D on no card wrote %v", got)
	}
	if got := s.bar(); strings.Contains(got, "deleting") {
		t.Errorf("the bar reads %q, want a press that did nothing to say nothing", got)
	}
}

// TestACardNamesTheKeysThatChangeIt, rather than the status bar: they reach one
// row. A narrow card drops hints, so this is the width that proves it carries them.
func TestACardNamesTheKeysThatChangeIt(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "answer this one")

	s := commented(t, 200, 24, on)
	s.press("]")

	got := s.frame()
	for _, want := range []string{"e edit", "D delete"} {
		if !strings.Contains(got, want) {
			t.Errorf("the lit card does not name %q:\n%s", want, got)
		}
	}
	if bar := s.bar(); strings.Contains(bar, "e edit") || strings.Contains(bar, "D delete") {
		t.Errorf("the bar reads %q, want the card's own keys left to the card", bar)
	}
}

// TestEditFallsBackToTheBoxOverTheFrame. It holds every key while it is up, so a
// pane with no room to draw one beside the card is not a reason to have none.
func TestEditFallsBackToTheBoxOverTheFrame(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "hi")

	s := commented(t, 50, 10, on)
	s.press("]", "e")

	if got := s.frame(); !strings.Contains(got, "Edit comment on README.md:2") {
		t.Fatalf("e opened nothing on a frame with no room beside the code:\n%s", got)
	}

	s.press("!", "ctrl+s")
	want := `EditComment aaaaaaaaaaaa "hi!" gen=2`
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the box over the frame wrote %v, want %q", got, want)
	}
}

// TestAnEditCrossesToTheFrameWithItsWords. A terminal shrinking under a reader
// mid-sentence carries them across with it.
func TestAnEditCrossesToTheFrameWithItsWords(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "hi")

	s := commented(t, 100, 24, on)
	s.press("]", "e", "!")
	s.send(tea.WindowSizeMsg{Width: 50, Height: 10})

	got := s.frame()
	if !strings.Contains(got, "Edit comment on README.md:2") {
		t.Fatalf("the box went with the room for it:\n%s", got)
	}
	if !strings.Contains(got, "hi!") {
		t.Errorf("the words went with it:\n%s", got)
	}

	s.press("ctrl+s")
	want := `EditComment aaaaaaaaaaaa "hi!" gen=2`
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the box that moved wrote %v, want %q", got, want)
	}
}

// rowHolding is the frame row a string lands on, which is what a cursor position
// is checked against.
func rowHolding(t *testing.T, s *screen, want string) int {
	t.Helper()

	for i, line := range s.lines() {
		if strings.Contains(line, want) {
			return i
		}
	}
	t.Fatalf("no row holds %q:\n%s", want, s.frame())
	return -1
}

// TestTheCursorIsTheTerminalsOwn. Nothing is drawn into the body: the root hands
// the position up and the terminal draws the cursor the reader set up.
func TestTheCursorIsTheTerminalsOwn(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys []string
	}{
		{"the box beside the code", []string{"j", "c", "h", "i"}},
		{"the box over the frame", []string{"C", "h", "i"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press(tt.keys...)

			c := s.m.View().Cursor
			if c == nil {
				t.Fatal("the frame carries no cursor while a box is up")
			}

			// Cells and not bytes: a border rune is three of the second and one of
			// the first, and a cursor is placed in cells.
			row := rowHolding(t, s, "hi")
			line := s.lines()[row]
			want := lipgloss.Width(line[:strings.Index(line, "hi")]) + len("hi")

			if c.Y != row || c.X != want {
				t.Errorf("the cursor is at %d,%d, want %d,%d, just past what was typed",
					c.X, c.Y, want, row)
			}
		})
	}
}

// TestNothingCarriesACursorWithNoBoxUp, which is every other frame: a terminal
// cursor parked in a diff is one the reader reads as an edit point.
func TestNothingCarriesACursorWithNoBoxUp(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24)

	if c := s.m.View().Cursor; c != nil {
		t.Errorf("the frame carries a cursor at %d,%d with no box up", c.X, c.Y)
	}
}

// TestASaveDropsTheEnterItWasFinishedOn, the way a body off stdin loses the one
// a heredoc leaves. The card draws every break, so a stray one is a blank row.
func TestASaveDropsTheEnterItWasFinishedOn(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24)
	s.press("j", "c", "h", "i")
	s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.press("ctrl+s")

	want := `AddComment a.go head:1-1 line "hi" gen=2`
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want %q", got, want)
	}
}

// TestARefusalReadsFromTheRightOfTheBar, where every other notice is. One thrown
// to the left is a second place to look for what just happened.
func TestARefusalReadsFromTheRightOfTheBar(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24).press("j", "c", "h", "i")
	s.src.wroteErr = errors.New("the database is locked")
	s.press("ctrl+s")

	bar := strings.TrimRight(s.bar(), " ")
	if !strings.HasSuffix(bar, "the database is locked") {
		t.Errorf("the bar reads %q, want the refusal at its right", bar)
	}
	if !strings.Contains(bar, "ctrl+s save") {
		t.Errorf("the bar reads %q, want the box's keys still on it", bar)
	}
}
