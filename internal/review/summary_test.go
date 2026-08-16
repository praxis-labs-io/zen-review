package review_test

import (
	"testing"

	"github.com/zen-review/zen-review/internal/review"
)

// summary is the note a session reads back, or the test that failed reading it.
func summary(t *testing.T, s *review.Session) string {
	t.Helper()

	note, err := s.Summary(t.Context())
	if err != nil {
		t.Fatalf("reading the summary: %v", err)
	}
	return note
}

// The note is what a review concluded, so it outlives the process that wrote it.
func TestTheSummaryOutlivesTheSession(t *testing.T) {
	f := branched(t)

	first := f.mustOpen("")
	if got := summary(t, first); got != "" {
		t.Errorf("summary = %q on a session nobody wrote one for, want empty", got)
	}
	if err := first.SetSummary(t.Context(), "held the store changes"); err != nil {
		t.Fatalf("writing the summary: %v", err)
	}
	if got := summary(t, first); got != "held the store changes" {
		t.Errorf("summary = %q, want the session to read back what it wrote", got)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first session: %v", err)
	}

	second := f.mustOpen("")

	if got := summary(t, second); got != "held the store changes" {
		t.Errorf("summary = %q on the resumed session, want the note that was written", got)
	}
}

// A reader open for an hour is not a snapshot. Without this the composer opens
// over what the session started with and the next save puts that back.
func TestASessionReadsANoteAnotherWrote(t *testing.T) {
	f := branched(t)

	reader := f.mustOpen("")
	defer func() { _ = reader.Close() }()

	writer := f.mustOpen("")
	if err := writer.SetSummary(t.Context(), "written from the other one"); err != nil {
		t.Fatalf("writing the summary: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the writing session: %v", err)
	}

	if got := summary(t, reader); got != "written from the other one" {
		t.Errorf("summary = %q on the session that was already open, want the newer note", got)
	}
}

// Empty clears it, which is the only way to take a note back.
func TestAnEmptySummaryClearsTheNote(t *testing.T) {
	f := branched(t)
	s := f.mustOpen("")

	if err := s.SetSummary(t.Context(), "wrote something"); err != nil {
		t.Fatalf("writing the summary: %v", err)
	}
	if err := s.SetSummary(t.Context(), ""); err != nil {
		t.Fatalf("clearing the summary: %v", err)
	}

	if got := summary(t, s); got != "" {
		t.Errorf("summary = %q, want it cleared", got)
	}
}

// Moving the base is a write of the same row and leaves the note where it was.
// The two have different writers so resuming cannot lose what it concluded.
func TestMovingTheBaseKeepsTheSummary(t *testing.T) {
	f := branched(t)
	f.Git("branch", "other", "main")

	first := f.mustOpen("")
	if err := first.SetSummary(t.Context(), "held the store changes"); err != nil {
		t.Fatalf("writing the summary: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first session: %v", err)
	}

	second := f.mustOpen("other")

	if second.Base().Ref != "other" {
		t.Errorf("base = %s, want the one just passed", second.Base().Ref)
	}
	if got := summary(t, second); got != "held the store changes" {
		t.Errorf("summary = %q, want the base move to have left it alone", got)
	}
}
