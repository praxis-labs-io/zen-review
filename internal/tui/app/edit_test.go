package app_test

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
)

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

func TestAnEmptyEditWritesNothingAndSaysSo(t *testing.T) {
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, "hi")

	s := commented(t, 100, 24, on)
	s.press("]", "e", "backspace", "backspace", "ctrl+s")

	if got := s.calls(); len(got) != 0 {
		t.Errorf("an empty save wrote %v", got)
	}
	if got := s.bar(); !strings.Contains(got, "cannot save an empty comment") {
		t.Errorf("the bar reads %q, want what the press could not do", got)
	}
	if got := s.frame(); !strings.Contains(got, "◇ editing") {
		t.Errorf("the box came down on a press that wrote nothing:\n%s", got)
	}

	s.press("h")
	if got := s.bar(); strings.Contains(got, "cannot save") {
		t.Errorf("the bar still reads %q a keystroke later", got)
	}
}

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

func TestNothingCarriesACursorWithNoBoxUp(t *testing.T) {
	s := over(t, testchangeset.Derive(t, mixedPatch), 100, 24)

	if c := s.m.View().Cursor; c != nil {
		t.Errorf("the frame carries a cursor at %d,%d with no box up", c.X, c.Y)
	}
}

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

func TestASecondDeleteTakesNothing(t *testing.T) {
	first := testchangeset.Comment("aaaaaaaaaaaa", "internal/review/state.go", 13, 13, "the first one")
	second := testchangeset.Comment("bbbbbbbbbbbb", "internal/review/state.go", 13, 13, "the second one")

	s := commented(t, 100, 24, first, second)
	s.resolving(second)
	s.press("]", "D", "D")

	want := "DeleteComment aaaaaaaaaaaa gen=2"
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want the one the reader aimed at", got)
	}
}
