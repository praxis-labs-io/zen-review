package cli_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/golden"
)

// The report is what somebody still has to answer, so a resolved comment is not
// in it and everything else is: an orphan included, because the code moving
// under a comment is not an answer to it.
func TestTheExportCarriesEverythingUnresolved(t *testing.T) {
	f, ids := queue(t)

	out := f.mustRun("export")

	for state, id := range ids {
		held := strings.Contains(out, id)
		if want := state != "resolved"; held != want {
			t.Errorf("the %s comment is in the report = %t, want %t:\n%s", state, held, want, out)
		}
	}
	for _, want := range []string{"still open", "about the line that goes", "why did this go"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not carry %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "the whole file") {
		t.Errorf("the report carries the resolved comment's body:\n%s", out)
	}
}

// One heading per file, and the reference repeated on every entry under it. The
// heading is not a location somebody can paste, and going to the code is what a
// reader does with a report.
func TestTheExportGroupsByFileAndKeepsTheReferences(t *testing.T) {
	f, _ := queue(t)

	out := f.mustRun("export")

	for _, want := range []string{"## code.txt\n", "## gone.txt\n"} {
		if strings.Count(out, want) != 1 {
			t.Errorf("%q appears %d times, want one heading per file:\n%s", want, strings.Count(out, want), out)
		}
	}
	for _, want := range []string{"`code.txt:3`", "`code.txt:30`", "`gone.txt:1`"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not carry %q:\n%s", want, out)
		}
	}
}

// The burn-down is what makes this a report rather than a comment dump, and it
// is the same count zen-review files prints.
func TestTheExportCountsWhatHasBeenRead(t *testing.T) {
	f := clean(t)
	f.mustRun("review", "code.txt", "--all")

	out := f.mustRun("export")

	state, _ := f.decodeState("files")
	want := "of " + strconv.Itoa(state.Totals.Items) + " reviewed"
	if !strings.Contains(out, want) {
		t.Errorf("the report does not say %q:\n%s", want, out)
	}
	if !strings.Contains(out, strconv.Itoa(state.Totals.Reviewed)+" of ") {
		t.Errorf("the report does not report %d read:\n%s", state.Totals.Reviewed, out)
	}
	if !strings.Contains(out, "nothing unresolved") {
		t.Errorf("the report does not say there is nothing outstanding:\n%s", out)
	}
}

// The note the review concluded with opens the report, verbatim. Markdown is
// laid out by whatever renders it, so nothing is folded on the way out.
func TestTheExportOpensOnTheSummary(t *testing.T) {
	f := clean(t)
	f.mustRun("summary", "--set", "held the store changes until the migration lands")

	out := f.mustRun("export")

	if !strings.Contains(out, "\nheld the store changes until the migration lands\n") {
		t.Errorf("the report does not carry the note on a line of its own:\n%s", out)
	}
}

// A paste lands in front of somebody who cannot see the repository, so what the
// lines below no longer describe has to travel with them.
func TestTheExportSaysWhenTheLinesHaveMoved(t *testing.T) {
	f := clean(t)
	f.comment("code.txt", "--hunk", "3", "--body", "here")
	f.Write("code.txt", numbered(1, 40))

	out := f.mustRun("export")

	if !strings.Contains(out, "The work tree has moved") {
		t.Errorf("the report does not say the lines may have moved:\n%s", out)
	}
}

// A session with nothing built yet has nothing to count and says so, rather than
// reporting zero of zero as though that were an answer.
func TestTheExportSaysWhenThereIsNoGeneration(t *testing.T) {
	f := edited(t)

	out := f.mustRun("export")

	for _, want := range []string{"No generation yet", "zen-review refresh"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "reviewed, ") {
		t.Errorf("the report wrote a burn-down for a generation that does not exist:\n%s", out)
	}
}

// --json is persistent and reaches every command. Honouring it here would be a
// second wire shape saying what comments --json already says.
func TestTheExportRefusesJSON(t *testing.T) {
	f := clean(t)

	err := f.failure("export", "--json")

	for _, want := range []string{"markdown", "comments --json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

// The report's shape is a contract with whatever it is pasted into, and it is
// the one output of this tool a person reads somewhere other than a terminal.
//
// It is a golden here rather than in golden_test.go, which locks the JSON
// schema with every value normalised out and says not to assert values in it.
// What this locks is the values: the prose, the ordering and the layout.
func TestTheExportedReportIsTheContract(t *testing.T) {
	f, _ := queue(t)
	f.mustRun("summary", "--set", "held the store changes until the migration lands")
	f.mustRun("review", "gone.txt", "--all")

	w, _ := f.decodeComments("comments")
	out := f.mustRun("export")

	subs := append(commentSubs(w), [2]string{w.Base.SHA[:7], "<sha>"})
	golden.Compare(t, "export", []byte(scrub(out, w.wireHeader, subs...)))
}

// A paste lands in front of somebody who cannot open the repository, so the
// words backing a claim have to travel with it.
func TestTheExportCarriesTheAnswer(t *testing.T) {
	f, _ := queue(t)

	out := f.mustRun("export")

	if !strings.Contains(out, "> it moved into the store package") {
		t.Errorf("the report drops the answer behind an addressed comment:\n%s", out)
	}
}
