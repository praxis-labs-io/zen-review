package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/golden"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
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

// TestPreviewReadsEachFileItIsTurnedOnOver. The mode lasts the run, so walking
// to the next file is a file whose text has not been read yet.
func TestPreviewReadsEachFileItIsTurnedOnOver(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.src.bodies["README.md"] = testchangeset.Body(12)

	s.press("n", "n", "p")
	s.press("tab")

	if got := s.src.read; len(got) != 2 {
		t.Errorf("the reader asked for %v, want the second file too", got)
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
