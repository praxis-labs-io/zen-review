package cli_test

import (
	"strings"
	"testing"
)

// summaryWire is what the summary command answers with: the same session, and
// the note written against it.
type summaryWire struct {
	wireHeader

	Summary string `json:"summary"`
}

func (f *fixture) decodeSummary(args ...string) (summaryWire, string) {
	f.t.Helper()

	var w summaryWire
	raw := f.jsonFrom(f.Dir(), &w, args...)
	return w, raw
}

// The note is written, answered with, and read back by the next invocation,
// which is a different process against the same database.
func TestTheSummaryIsWrittenAndReadBack(t *testing.T) {
	f := clean(t)

	written, _ := f.decodeSummary("summary", "--set", "held the store changes")
	if written.Summary != "held the store changes" {
		t.Errorf("the write answered with %q, want the note it wrote", written.Summary)
	}

	read, _ := f.decodeSummary("summary")
	if read.Summary != "held the store changes" {
		t.Errorf("summary = %q, want the note the last call wrote", read.Summary)
	}
}

// A note with newlines in it does not have to survive a shell.
func TestTheSummaryCanArriveOnStdin(t *testing.T) {
	f := clean(t)
	f.stdin = strings.NewReader("two things:\n\n- the first\n- the second\n")

	f.mustRun("summary", "--set", "-")

	w, _ := f.decodeSummary("summary")
	if !strings.Contains(w.Summary, "\n- the first\n") {
		t.Errorf("summary = %q, want the lines it was given", w.Summary)
	}
	// A heredoc ends in a newline and a note does not.
	if strings.HasSuffix(w.Summary, "\n") {
		t.Errorf("summary = %q, want the trailing newline gone", w.Summary)
	}
}

// Empty clears it, which is the only way to take a note back.
func TestAnEmptySummaryClearsTheNote(t *testing.T) {
	f := clean(t)
	f.mustRun("summary", "--set", "wrote something")

	f.mustRun("summary", "--set", "")

	w, _ := f.decodeSummary("summary")
	if w.Summary != "" {
		t.Errorf("summary = %q, want it cleared", w.Summary)
	}
}

// No note yet is a sentence naming the flag, rather than a blank where an answer
// goes.
func TestNoSummaryYetNamesTheFlag(t *testing.T) {
	f := clean(t)

	out := f.mustRun("summary")

	for _, want := range []string{"no summary yet", "--set"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
}

// A write does not move the base. The move sticks, and this call was not about
// it. A read takes the flag the way every other read does.
func TestOnlyTheWriteRefusesToMoveTheBase(t *testing.T) {
	f := clean(t)

	err := f.failure("summary", "--base", "main", "--set", "a note")

	for _, want := range []string{"--base", "summary"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}

	if _, _, err := f.run("summary", "--base", "main"); err != nil {
		t.Errorf("reading the summary with --base = %v, want it accepted the way every read is", err)
	}
}

// A note wider than the page is folded under the same indent a comment body
// takes, so the two read as one surface.
func TestALongSummaryIsFoldedToThePage(t *testing.T) {
	f := clean(t)
	f.mustRun("summary", "--set", strings.Repeat("wordy ", 40))

	out := f.mustRun("summary")

	folded := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "    ") {
			continue
		}
		folded++
		if len(line) > 80 {
			t.Errorf("a note line is %d wide:\n%s", len(line), line)
		}
	}
	if folded < 3 {
		t.Errorf("the note came back on %d lines, so nothing was folded:\n%s", folded, out)
	}
}
