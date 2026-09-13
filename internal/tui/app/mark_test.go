package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
)

const read = "●"

func TestRMarksTheHunkTheCursorIsOn(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.press("r")

	want := []string{"MarkHunk a.go head:1 gen=2"}
	if got := s.calls(); !equal(got, want) {
		t.Errorf("the reader wrote %v, want %v", got, want)
	}
}

func TestAMarkedHunkWearsTheBadgeInTheDiffPane(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	if strings.Contains(s.frame(), read) {
		t.Fatal("a hunk reads as read before anything was marked")
	}

	s.wrote(testchangeset.Derive(t, ringPatch, testchangeset.Head("a.go", 1, 1)))
	s.press("r")

	if !strings.Contains(s.frame(), read) {
		t.Errorf("no heading wears the badge after r:\n%s", s.frame())
	}
}

func TestRAdvancesToTheNextUnreadHunk(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	s.wrote(testchangeset.Derive(t, ringPatch, testchangeset.Head("a.go", 1, 1)))
	s.press("r")

	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Errorf("the cursor is on %q, want a.go's second hunk", got)
	}
}

func TestTheMarkKeysAdvanceAndTheUnmarkKeysStay(t *testing.T) {
	tests := []struct {
		key     string
		read    []store.ReviewedRange
		file    string
		heading string
	}{
		{"r", []store.ReviewedRange{testchangeset.Head("a.go", 1, 1)}, "a.go", "@@ -10,0 +11,1 @@"},
		{"R", []store.ReviewedRange{testchangeset.Head("a.go", 1, 1), testchangeset.Head("a.go", 11, 11)}, "b.go", "@@ -1,0 +1,1 @@"},
		{"u", nil, "a.go", "@@ -1,0 +1,1 @@"},
		{"U", nil, "a.go", "@@ -1,0 +1,1 @@"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

			s.wrote(testchangeset.Derive(t, ringPatch, tt.read...))
			s.press(tt.key)

			if got := s.title(); !strings.Contains(got, tt.file) {
				t.Errorf("%s left the pane on %q, want %s", tt.key, got, tt.file)
			}
			if got := heading(t, s); !strings.Contains(got, tt.heading) {
				t.Errorf("%s left the cursor on %q, want %s", tt.key, got, tt.heading)
			}
		})
	}
}

func TestRStaysPutWhenNothingIsLeftUnread(t *testing.T) {
	whole := testchangeset.Derive(t, ringPatch,
		testchangeset.Head("a.go", 1, 1), testchangeset.Head("a.go", 11, 11),
		testchangeset.Head("b.go", 1, 1), testchangeset.Head("b.go", 11, 11),
	)
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	s.wrote(whole)
	s.press("r")

	if got := heading(t, s); !strings.Contains(got, "@@ -1,0 +1,1 @@") {
		t.Errorf("the cursor moved to %q, want a.go's first hunk", got)
	}
}

func TestTheMarkKeysReachTheirOwnCall(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"r", "MarkHunk a.go head:1 gen=2"},
		{"R", "MarkFile a.go gen=2"},
		{"u", "UnmarkHunk a.go head:1 gen=2"},
		{"U", "UnmarkFile a.go gen=2"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
			s.press(tt.key)

			if got := s.calls(); !equal(got, []string{tt.want}) {
				t.Errorf("%s wrote %v, want [%s]", tt.key, got, tt.want)
			}
		})
	}
}

func TestRMarksAFileWithNoHunksWhole(t *testing.T) {
	s := open(t, 100, 24)
	for range 20 {
		if strings.Contains(s.lines()[0], "assets/logo.png") {
			break
		}
		s.press("tab")
	}
	if !strings.Contains(s.lines()[0], "assets/logo.png") {
		t.Fatalf("the ring never reached the binary file: %q", s.lines()[0])
	}
	s.press("r")

	if got := s.calls(); len(got) != 1 || !strings.HasPrefix(got[0], "MarkFile assets/logo.png") {
		t.Errorf("the reader wrote %v, want one MarkFile against the binary file", got)
	}
}

func TestAStaleWriteWritesNothingAndSaysWhichKeyAnswersIt(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.src.wroteErr = &review.StaleGenerationError{Seq: 2, Current: 3}
	s.press("r")

	if got := s.calls(); len(got) != 0 {
		t.Errorf("the reader wrote %v, want nothing", got)
	}
	bar := s.bar()
	if !strings.Contains(bar, "generation 2 is not the current one, 3 is") {
		t.Errorf("the bar says %q, want the error's own sentence", bar)
	}
	if !strings.Contains(bar, "press s") {
		t.Errorf("the bar says %q, want the key that answers it", bar)
	}
	if strings.Contains(s.frame(), read) {
		t.Error("a refused write left a hunk reading as read")
	}
}

func TestAFailedWriteLeavesTheChangesetAlone(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	was := s.frame()

	s.src.wroteErr = errors.New("the database is locked")
	s.wrote(testchangeset.Derive(t, ringPatch, testchangeset.Head("a.go", 1, 1)))
	s.press("r")

	if got := heading(t, s); !strings.Contains(got, "@@ -1,0 +1,1 @@") {
		t.Errorf("the cursor moved to %q on a write that failed", got)
	}
	if strings.Contains(s.frame(), read) {
		t.Error("a failed write left a hunk reading as read")
	}
	if bar := s.bar(); !strings.Contains(bar, "the database is locked") {
		t.Errorf("the bar says %q, want the error", bar)
	}
	if lines(was)[1] != lines(s.frame())[1] {
		t.Error("a failed write moved the pane")
	}
}

func TestAPressDuringAWriteIsRefusedAndSaysSo(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	cmd := s.hold(keystroke("r"))
	if bar := s.bar(); !strings.Contains(bar, "marking") {
		t.Errorf("the bar says %q while a write is out, want it to say what is happening", bar)
	}

	s.press("r")
	s.drain(cmd)

	want := []string{"MarkHunk a.go head:1 gen=2"}
	if got := s.calls(); !equal(got, want) {
		t.Errorf("the reader wrote %v, want the press in flight alone", got)
	}
}

func TestTheAdvanceStopsAtTheEndRatherThanWrapping(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch,
		testchangeset.Head("a.go", 11, 11), testchangeset.Head("b.go", 1, 1),
	), 100, 16)

	s.press("n")
	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Fatalf("n landed on %q, want b.go's second hunk", got)
	}

	s.wrote(testchangeset.Derive(t, ringPatch,
		testchangeset.Head("a.go", 11, 11), testchangeset.Head("b.go", 1, 1),
		testchangeset.Head("b.go", 11, 11),
	))
	s.press("r")

	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Errorf("the cursor wrapped to %q, want it to stay at the end", got)
	}
}

func TestTheBarSaysHowFarDownTheReviewIs(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	s.wrote(testchangeset.Derive(t, ringPatch, testchangeset.Head("a.go", 1, 1)))
	s.press("r")

	if bar := s.bar(); !strings.Contains(bar, "1/4 read") {
		t.Errorf("the bar says %q, want the burn-down after the mark", bar)
	}
}

func TestJTakesTheRingIntoTheHunkTheCursorReaches(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	s.press("j", "j", "j")
	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Fatalf("the caret is on %q, want a.go's second hunk", got)
	}

	s.press("r")
	want := []string{"MarkHunk a.go head:11 gen=2"}
	if got := s.calls(); !equal(got, want) {
		t.Errorf("r wrote %v, want %v", got, want)
	}
}

func TestAMoveDuringAWriteCancelsTheAdvance(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	cmd := s.hold(keystroke("r"))
	s.press("j")
	s.drain(cmd)

	if got := heading(t, s); !strings.Contains(got, "@@ -1,0 +1,1 @@") {
		t.Errorf("the write advanced to %q, want the hunk the reader stayed in", got)
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func lines(frame string) []string { return strings.Split(frame, "\n") }

func TestAStaleWriteUnderANoteBoxNamesTheKeysPastIt(t *testing.T) {
	s := open(t, 100, 24)
	s.src.wroteErr = &review.StaleGenerationError{Seq: 2, Current: 3}

	marking := s.hold(keystroke("r"))
	s.press("C")
	s.drain(marking)

	if got := s.bar(); !strings.Contains(got, "esc, then s") {
		t.Errorf("the bar reads %q, want the keys that reach the reload past the box", got)
	}
}
