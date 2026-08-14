package review_test

import (
	"testing"
)

// The note is what a review concluded, so it outlives the process that wrote it.
func TestTheSummaryOutlivesTheSession(t *testing.T) {
	f := branched(t)

	first := f.mustOpen("")
	if first.Summary() != "" {
		t.Errorf("summary = %q on a session nobody wrote one for, want empty", first.Summary())
	}
	if err := first.SetSummary(t.Context(), "held the store changes"); err != nil {
		t.Fatalf("writing the summary: %v", err)
	}
	if first.Summary() != "held the store changes" {
		t.Errorf("summary = %q, want the session to read back what it wrote", first.Summary())
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first session: %v", err)
	}

	second := f.mustOpen("")

	if second.Summary() != "held the store changes" {
		t.Errorf("summary = %q on the resumed session, want the note that was written", second.Summary())
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

	if s.Summary() != "" {
		t.Errorf("summary = %q, want it cleared", s.Summary())
	}
}

// Moving the base is a write of the same row, and it leaves the note where it
// was. The two have different writers so that resuming a session cannot lose
// what it concluded.
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
	if second.Summary() != "held the store changes" {
		t.Errorf("summary = %q, want the base move to have left it alone", second.Summary())
	}
}
