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

const twoHunks = "internal/review/state.go"

const previewLines = 130

func TestThePreviewGoldenFrame(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "}", "p")

	golden.Compare(t, "preview", []byte(s.frame()+"\n"))
}

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

func TestPreviewIsNotResurrected(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")
	s.press("tab", "shift+tab")

	if strings.Contains(s.frame(), "of the file") {
		t.Errorf("the whole file was waiting on the way back:\n%s", s.frame())
	}

	s.press("p")
	if !strings.Contains(s.frame(), "line 15 of the file") {
		t.Fatalf("the whole file did not come back:\n%s", s.frame())
	}
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader asked for %v, want the text it already had", got)
	}
}

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

func TestPreviewSaysSoWhenTheReadFails(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.src.bodyErr = errors.New("reading the blob: object missing")
	s.press("n", "n", "p")

	if !strings.Contains(s.bar(), "object missing") {
		t.Errorf("the bar says %q", s.bar())
	}

	s.src.bodyErr = nil
	s.press("p")
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader asked for %v after the failure cleared", got)
	}
}

func TestPreviewSurvivesAReload(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

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

func TestPreviewRereadsANewGeneration(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	s.src.at.Generation = review.Generation{ID: 3, Seq: 3}
	s.press("s")

	if got := s.src.read; len(got) != 2 {
		t.Errorf("the reader asked for %v, want the new generation's bytes too", got)
	}
}

func TestPreviewWalksOntoABinaryFile(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	s.press("h", "g", "l")
	if got := s.src.read; len(got) != 1 {
		t.Errorf("the reader read %v, want the binary file left alone", got)
	}
	if !strings.Contains(s.frame(), "binary") {
		t.Errorf("the pane did not say the file is binary:\n%s", s.frame())
	}
}

func TestTheBoxHangsUnderAFilledInLine(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p")

	for range 12 {
		s.press("j")
	}
	was := s.frame()
	if !strings.Contains(was, "line 20 of the file") {
		t.Fatalf("the cursor did not reach the run:\n%s", was)
	}

	s.press("c")
	got := s.frame()

	for _, want := range []string{"line 20 of the file", "ctrl+s save"} {
		if !strings.Contains(got, want) {
			t.Errorf("the box does not hang under the line it is about, %q is missing:\n%s", want, got)
		}
	}

	if strings.Contains(got, "was line") {
		t.Errorf("the box says the changeset has no line for it:\n%s", got)
	}
}

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

	s.press("R")
	if got := s.calls(); len(got) != 1 || !strings.Contains(got[0], "MarkFile") {
		t.Errorf("R wrote %v, want the whole file", got)
	}
}

func TestRStillMarksTheHunkTheCursorIsIn(t *testing.T) {
	s := previewing(t, twoHunks, previewLines, 100, 16)
	s.press("n", "n", "p", "j")

	s.press("r")
	if got := s.calls(); len(got) != 1 || !strings.Contains(got[0], "MarkHunk") {
		t.Errorf("r wrote %v, want the hunk the cursor is in", got)
	}
}

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
