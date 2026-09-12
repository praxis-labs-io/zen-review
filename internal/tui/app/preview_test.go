package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/golden"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
)

// twoHunks is the fixture's Go file, the one with code in it and two hunks.
const twoHunks = "internal/review/state.go"

// previewLines is how long that file is past its last hunk, which ends at 126.
const previewLines = 130

// TestThePreviewGoldenFrame. The whole file around the hunks, at the width the
// rest of the frames are locked at.
//
// The ring is walked to the second hunk first, so the frame holds the boundary
// the mode exists for: the lines the diff never showed, then the heading of the
// hunk they run into.
func TestThePreviewGoldenFrame(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "}", "p")

	golden.Compare(t, "preview", []byte(s.frame()+"\n"))
}

// TestPreviewReadsOncePerFile. The text is the bytes of one generation, so the
// key that turns the mode off and on again is not two more git calls.
func TestPreviewReadsOncePerFile(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	if got := s.src.read; len(got) != 1 || got[0] != twoHunks {
		t.Fatalf("the reader asked for %v, want %s once", got, twoHunks)
	}

	s.press("p", "p")
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader asked again: %v", got)
	}
}

// TestPreviewIsOneFileAtATime. The key is asked on the hunk the reader cannot
// judge, not as a taste in diffs, so the next file comes back as its hunks. Left
// on it would put a few hundred unchanged rows between the hunks of every file
// after it, which is the burn-down this tool is.
func TestPreviewIsOneFileAtATime(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.src.bodies["README.md"] = testchangeset.Body(12)

	s.press("n", "n", "p")
	if !strings.Contains(s.frame(), "line 15 of the file") {
		t.Fatalf("the whole file is not on:\n%s", s.frame())
	}

	s.press("tab")
	if strings.Contains(s.frame(), "of the file") {
		t.Errorf("the next file came up with the whole of it showing:\n%s", s.frame())
	}
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader asked for %v, want the next file left alone", got)
	}
}

// TestPreviewIsNotResurrected. Coming back to the file is the hunks, because the
// mode went when the reader left rather than waiting there for them.
func TestPreviewIsNotResurrected(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")
	s.press("tab", "shift+tab")

	if strings.Contains(s.frame(), "of the file") {
		t.Errorf("the whole file was waiting on the way back:\n%s", s.frame())
	}

	// And the text is still in hand, so turning it on again is no second read.
	s.press("p")
	if !strings.Contains(s.frame(), "line 15 of the file") {
		t.Fatalf("the whole file did not come back:\n%s", s.frame())
	}
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader asked for %v, want the text it already had", got)
	}
}

// TestPreviewSaysSoWhenAFileHasNoLines. A binary file has nothing to fill in, and
// a key that appeared to do nothing is worse than one that says why.
func TestPreviewSaysSoWhenAFileHasNoLines(t *testing.T) {
	s := open(t, 100, 16)
	s.press("p")

	if !strings.Contains(s.bar(), "no lines to show") {
		t.Errorf("the bar says %q", s.bar())
	}
	if got := s.src.read; len(got) != 0 {
		t.Errorf("the reader asked for %v anyway", got)
	}
}

// TestPreviewSaysSoWhenTheReadFails. The mode stands down rather than sitting on
// a file whose text never arrives, and the bar carries the reason.
func TestPreviewSaysSoWhenTheReadFails(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.src.bodyErr = errors.New("reading the blob: object missing")
	s.press("n", "n", "p")

	if !strings.Contains(s.bar(), "object missing") {
		t.Errorf("the bar says %q", s.bar())
	}

	// And the next press asks again: nothing was cached, because nothing came back.
	s.src.bodyErr = nil
	s.press("p")
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader asked for %v after the failure cleared", got)
	}
}

// TestPreviewSurvivesAReload. A generation the refresh did not move is the same
// bytes under the same path, and a reader mid-file keeps what they were reading.
func TestPreviewSurvivesAReload(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	// The panes and not the status bar, which gains the notice the reload left.
	panes := func() string {
		rows := s.lines()
		return strings.Join(rows[:len(rows)-1], "\n")
	}
	was := panes()

	s.press("s")
	if got := panes(); got != was {
		t.Errorf("the panes moved for a reload that changed nothing:\n%s", got)
	}
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader read the file again for a reload that moved nothing: %v", got)
	}
}

// TestPreviewRereadsANewGeneration. A body is the bytes of one generation, so a
// refresh that built one is a file whose text has to be read again.
func TestPreviewRereadsANewGeneration(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	s.src.at.Generation = review.Generation{ID: 3, Seq: 3}
	s.press("s")

	if got := s.src.read; len(got) != 2 {
		t.Errorf("the reader asked for %v, want the new generation's bytes too", got)
	}
}

// TestPreviewWalksOntoABinaryFile. The mode lasts the run, so the next file is
// one it was never turned on over, and a blob that is not lines is not read at
// all rather than read and drawn.
func TestPreviewWalksOntoABinaryFile(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	// Back round the ring to the binary file, which the tree opens on.
	s.press("h", "g", "l")
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader read %v, want the binary file left alone", got)
	}
	if !strings.Contains(s.frame(), "binary") {
		t.Errorf("the pane did not say the file is binary:\n%s", s.frame())
	}
}

// TestTheBoxHangsUnderAFilledInLine. c on a line outside every hunk used to send
// the box to the foot of the file and take the reader with it, because the card
// pass only ran over a hunk's lines.
func TestTheBoxHangsUnderAFilledInLine(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	// Down off the hunk and into the run below it.
	for range 12 {
		s.press("j")
	}
	was := s.frame()
	if !strings.Contains(was, "line 20 of the file") {
		t.Fatalf("the cursor did not reach the run:\n%s", was)
	}

	s.press("c")
	got := s.frame()

	// The code it is about is still above it, and the whole file is still on.
	for _, want := range []string{"line 20 of the file", "ctrl+s save"} {
		if !strings.Contains(got, want) {
			t.Errorf("the box does not hang under the line it is about, %q is missing:\n%s", want, got)
		}
	}

	// And the label says nothing about a line the changeset cannot show, because
	// it is showing it.
	if strings.Contains(got, "was line") {
		t.Errorf("the box says the changeset has no line for it:\n%s", got)
	}
}

// TestRMarksNothingOnAFilledInLine. The ring stop still names the hunk the reader
// arrived on, so a mark path reading it would mark a hunk a hundred lines from
// the cursor, which is the thing the whole file was turned on to avoid.
func TestRMarksNothingOnAFilledInLine(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "}", "p", "g")

	s.press("r")
	if got := s.calls(); len(got) != 0 {
		t.Errorf("r wrote %v from a row in no hunk", got)
	}

	s.press("u")
	if got := s.calls(); len(got) != 0 {
		t.Errorf("u wrote %v from a row in no hunk", got)
	}

	// R still reaches the file, which is a thing that row is part of.
	s.press("R")
	if got := s.calls(); len(got) != 1 || !strings.Contains(got[0], "MarkFile") {
		t.Errorf("R wrote %v, want the whole file", got)
	}
}

// TestRStillMarksTheHunkTheCursorIsIn, so the guard above did not take the key
// away from the rows it belongs to.
func TestRStillMarksTheHunkTheCursorIsIn(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p", "j")

	s.press("r")
	if got := s.calls(); len(got) != 1 || !strings.Contains(got[0], "MarkHunk") {
		t.Errorf("r wrote %v, want the hunk the cursor is in", got)
	}
}

// TestAFailedReadLeavesAnotherFilesPreviewAlone. Two reads can be out at once,
// and the one that failed says nothing about the file on screen.
func TestAFailedReadLeavesAnotherFilesPreviewAlone(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	s.send(app.BodyFailed("internal/cli/render.go", 2, errors.New("object missing")))

	if !strings.Contains(s.frame(), "line 15 of the file") {
		t.Errorf("a failure on another file stood this one's preview down:\n%s", s.frame())
	}
	if strings.Contains(s.bar(), "object missing") {
		t.Errorf("it reported that failure against this file: %q", s.bar())
	}
}
