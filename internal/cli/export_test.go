package cli_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/golden"
)

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

func TestTheExportOpensOnTheSummary(t *testing.T) {
	f := clean(t)
	f.mustRun("summary", "--set", "held the store changes until the migration lands")

	out := f.mustRun("export")

	if !strings.Contains(out, "\nheld the store changes until the migration lands\n") {
		t.Errorf("the report does not carry the note on a line of its own:\n%s", out)
	}
}

func TestTheExportSaysWhenTheLinesHaveMoved(t *testing.T) {
	f := clean(t)
	f.comment("code.txt", "--hunk", "3", "--body", "here")
	f.Write("code.txt", numbered(1, 40))

	out := f.mustRun("export")

	if !strings.Contains(out, "The work tree has moved") {
		t.Errorf("the report does not say the lines may have moved:\n%s", out)
	}
}

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

func TestTheExportRefusesJSON(t *testing.T) {
	f := clean(t)

	err := f.failure("export", "--json")

	for _, want := range []string{"markdown", "comments --json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

func TestTheExportedReportIsTheContract(t *testing.T) {
	f, _ := queue(t)
	f.mustRun("summary", "--set", "held the store changes until the migration lands")
	f.mustRun("review", "gone.txt", "--all")

	w, _ := f.decodeComments("comments")
	out := f.mustRun("export")

	subs := append(commentSubs(w), [2]string{w.Base.SHA[:7], "<sha>"})
	golden.Compare(t, "export", []byte(scrub(out, w.wireHeader, subs...)))
}

func TestTheExportCarriesTheResponse(t *testing.T) {
	f, _ := queue(t)

	out := f.mustRun("export")

	if !strings.Contains(out, "> it moved into the store package") {
		t.Errorf("the report drops the response behind an addressed comment:\n%s", out)
	}
}
