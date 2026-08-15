package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/testchangeset"
)

// read is the badge a heading wears once the hunk under it has been read.
const read = "●"

// TestRMarksTheHunkTheCursorIsOn, naming it by side and line the way review
// does, and against the generation on screen.
func TestRMarksTheHunkTheCursorIsOn(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.press("r")

	want := []string{"MarkHunk a.go head:1 gen=2"}
	if got := s.calls(); !equal(got, want) {
		t.Errorf("the reader wrote %v, want %v", got, want)
	}
}

// The mark has to reach the pane, not just the tree. Both point into the
// changeset the write replaced, and only one of them is re-pointed by hand.
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

// r advances so r r r r walks a review down, and it advances against what the
// mark did rather than against the changeset that was there before it.
func TestRAdvancesToTheNextUnreadHunk(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	s.wrote(testchangeset.Derive(t, ringPatch, testchangeset.Head("a.go", 1, 1)))
	s.press("r")

	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Errorf("the cursor is on %q, want a.go's second hunk", got)
	}
}

// A press with nothing left unread leaves the cursor where it is. The write
// still happened; there is just nowhere to go.
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

// A file with no hunks is one stop and is marked whole, which is the only way a
// binary file can be read.
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

// A refresh landing between the press and the transaction is not a failure.
// Nothing was written, and the reader is told which key answers it.
func TestAStaleWriteWritesNothingAndSaysWhichKeyAnswersIt(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.src.wroteErr = &review.StaleGenerationError{Seq: 2, Current: 3}
	s.press("r")

	if got := s.calls(); len(got) != 0 {
		t.Errorf("the reader wrote %v, want nothing", got)
	}
	if bar := s.bar(); !strings.Contains(bar, "generation 3") || !strings.Contains(bar, "press s") {
		t.Errorf("the bar says %q, want the generation it moved to and the key to press", bar)
	}
	if strings.Contains(s.frame(), read) {
		t.Error("a refused write left a hunk reading as read")
	}
}

// A write that failed leaves the changeset alone. It is a local transaction
// that committed or did not, and there is no half-applied state to paint over.
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

// One write in flight at a time. The write itself is a local transaction, but
// the source it goes through can be held by a refresh already running.
func TestASecondWriteIsRefusedWhileOneIsInFlight(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	// hold applies the press without running the command it returned, which is
	// the frame the reader sees while the first write is still going.
	cmd := s.hold(keystroke("r"))
	s.press("r")

	if got := s.calls(); len(got) != 0 {
		t.Fatalf("a write landed before the first was drained: %v", got)
	}

	s.drain(cmd)
	if got := s.calls(); len(got) != 1 {
		t.Errorf("the reader wrote %v, want the first press alone", got)
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
