package cli_test

import (
	"strings"
	"testing"
)

// Every way of getting the flags wrong, and what the reader is told to do about
// it. None of these messages opens on a flag name or a path: the first letter is
// capitalised on the way out, which would print a --Hunk that does not exist.
func TestTheWriteFlagsRefuseWhatTheyCannotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "nothing named",
			args: []string{"review", "code.txt"},
			want: []string{"--hunk", "--lines", "--all"},
		},
		{
			name: "two things named",
			args: []string{"review", "code.txt", "--all", "--hunk", "3"},
			want: []string{"pass one"},
		},
		{
			// --all covers every side a file has, so a side is not a choice under it.
			name: "a side under --all",
			args: []string{"review", "code.txt", "--all", "--side", "base"},
			want: []string{"side is not a choice"},
		},
		{
			name: "a side that is neither",
			args: []string{"review", "code.txt", "--lines", "3", "--side", "sideways"},
			want: []string{"head or base", "sideways"},
		},
		{
			name: "lines that are not numbers",
			args: []string{"review", "code.txt", "--lines", "three"},
			want: []string{"A-B", "three"},
		},
		{
			name: "lines that end before they start",
			args: []string{"review", "code.txt", "--lines", "9-3"},
			want: []string{"ends before it starts", "9-3"},
		},
		{
			// Line 0 is the file as a whole rather than a line in it, and --all is
			// the way to say that.
			name: "a line 0 that means the whole file",
			args: []string{"review", "code.txt", "--lines", "0-3"},
			want: []string{"start at 1", "--all"},
		},
		{
			name: "a file the changeset does not hold",
			args: []string{"review", "elsewhere.txt", "--all"},
			want: []string{"elsewhere.txt", "zen-review files"},
		},
		{
			name: "a hunk no line names",
			args: []string{"review", "code.txt", "--hunk", "99"},
			want: []string{"code.txt", "head 99", "zen-review files"},
		},
		{
			// The whole reason --side exists. The deletion-only hunk is on the base,
			// so naming its line on the head names nothing.
			name: "a base-side hunk named on the head",
			args: []string{"review", "gone.txt", "--hunk", "1"},
			want: []string{"head 1"},
		},
		{
			name: "no path at all",
			args: []string{"review", "--all"},
			want: []string{"arg"},
		},
		{
			name: "unreview gets the same refusals",
			args: []string{"unreview", "code.txt"},
			want: []string{"--hunk", "--lines", "--all"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := marking(t)
			f.mustRun("refresh")

			err := f.failure(tc.args...)

			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

// A write does not take --base. The base belongs to the session and outlives the
// call, so moving it here would recompute the changeset and then record a mark
// against the one it replaced.
func TestAWriteRefusesToMoveTheBase(t *testing.T) {
	for _, args := range [][]string{
		{"review", "code.txt", "--all", "--base", "main"},
		{"unreview", "code.txt", "--all", "--base", "main"},
	} {
		t.Run(args[0], func(t *testing.T) {
			f := marking(t)
			f.mustRun("refresh")

			err := f.failure(args...)

			for _, want := range []string{"--base", "zen-review status"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

// The read commands still take it, which is the difference the refusal above is
// drawing.
func TestTheReadCommandsStillTakeTheBase(t *testing.T) {
	f := marking(t)

	f.mustRun("files", "--base", "main")
}

// A mark aimed at a generation that is no longer the latest is inert rather than
// merely old: the carry runs from the latest, so nothing would ever pick the row
// up. The flag is how a script that read the listing a moment ago finds out the
// ground moved under it.
func TestAMarkNamingAnOlderGenerationIsRefused(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	f.Write("code.txt", "one\ntwo\nTHREE\nfour\nFIVE\n")
	f.mustRun("refresh")

	err := f.failure("review", "code.txt", "--all", "--generation", "1")

	for _, want := range []string{"1", "2", "refresh"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}

	// Naming the generation that is there goes through.
	f.mustRun("review", "code.txt", "--all", "--generation", "2")
}

// There is nothing for a mark to anchor to, so this refuses where files reports.
func TestMarkingBeforeAnythingIsBuiltIsRefused(t *testing.T) {
	f := marking(t)

	err := f.failure("review", "code.txt", "--all")

	if !strings.Contains(err.Error(), "zen-review refresh") {
		t.Errorf("err = %v, want it to name what to run", err)
	}
}

// --all reaches both sides of a hunk that adds and removes, and unreview takes
// back exactly what it recorded.
func TestMarkingAWholeFileAndTakingItBack(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	w, _ := f.decodeState("review", "code.txt", "--all")
	if state(w.Files, "code.txt").State != "reviewed" {
		t.Errorf("code.txt = %s after --all, want reviewed", state(w.Files, "code.txt").State)
	}

	back, _ := f.decodeState("unreview", "code.txt", "--all")
	if got := state(back.Files, "code.txt"); got.State != "unreviewed" {
		t.Errorf("code.txt = %s after unreview --all, want unreviewed", got.State)
	}
}

// A selection of one line is still a selection, and nobody types 3-3.
func TestASingleLineIsARangeOfItself(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	// code.txt has one hunk and it is the one line the branch changed, so marking
	// that line alone reads the hunk end to end.
	w, _ := f.decodeState("review", "code.txt", "--lines", "3")

	got := state(w.Files, "code.txt")
	if got.State != "reviewed" {
		t.Errorf("code.txt = %s after --lines 3, want reviewed", got.State)
	}
}

// A file with no lines to name is marked as itself. Nothing else can reach it,
// so a --all that only walked hunks would leave it unreviewed for good.
func TestAFileWithNoHunksIsMarkedWhole(t *testing.T) {
	f := marking(t)
	f.Write("blob.bin", "\x00\x01binary\n")
	f.mustRun("refresh")

	w, _ := f.decodeState("review", "blob.bin", "--all")

	got := state(w.Files, "blob.bin")
	if got.State != "reviewed" {
		t.Errorf("blob.bin = %s, want reviewed", got.State)
	}
	if len(got.Hunks) != 0 {
		t.Fatalf("blob.bin has %d hunks, so this is not the case under test", len(got.Hunks))
	}
}

// A deletion-only hunk has no head-side lines and anchors to the base blob, so
// --side base is the only way to name it.
func TestADeletionOnlyHunkIsMarkedOnTheBase(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	w, _ := f.decodeState("review", "gone.txt", "--hunk", "1", "--side", "base")

	if got := state(w.Files, "gone.txt"); got.State != "reviewed" {
		t.Errorf("gone.txt = %s, want reviewed", got.State)
	}
}

// The burn-down is the number the review is walking to zero, and a write reports
// it back so a script marking in a loop does not need a second command to see
// where it is.
func TestAWriteReportsTheBurnDownItMoved(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	out := f.mustRun("review", "code.txt", "--all")

	if !strings.Contains(out, "1 of 3 reviewed") {
		t.Errorf("output does not carry the burn-down:\n%s", out)
	}
}

// state is one file of the payload, so an assertion does not depend on the order
// the files came back in.
func state(files []stateFile, path string) stateFile {
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	return stateFile{}
}

// marking is a changeset with one of each thing a write command has to be able
// to name: a hunk with lines on both sides, a hunk with lines on the base only,
// and a file added whole.
func marking(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("code.txt", "one\ntwo\nthree\nfour\nfive\n")
	f.Write("gone.txt", "doomed\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")

	// An insertion rather than a change, so the hunk has head-side lines and no
	// base-side ones. A changed line anchors on both sides, and a mark that only
	// reached one of them would still read as partial.
	f.Write("code.txt", "one\ntwo\ninserted\nthree\nfour\nfive\n")
	f.Git("rm", "-q", "gone.txt")
	f.Write("added.txt", "new\n")
	return f
}
