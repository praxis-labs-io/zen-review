package cli_test

import (
	"strings"
	"testing"
)

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
			name: "lines reaching past every hunk",
			args: []string{"review", "code.txt", "--lines", "40-50"},
			want: []string{"code.txt", "between 40 and 50", "zen-review files"},
		},
		{
			name: "lines on the side the file has none on",
			args: []string{"review", "gone.txt", "--lines", "1"},
			want: []string{"gone.txt", "head side", "zen-review files"},
		},
		{
			name: "lines on a file no line names",
			args: []string{"review", "blob.bin", "--lines", "1"},
			want: []string{"blob.bin", "--all"},
		},
		{
			name: "lines given as nothing",
			args: []string{"review", "code.txt", "--lines="},
			want: []string{"A-B", `""`},
		},
		{
			name: "all turned off",
			args: []string{"review", "code.txt", "--all=false"},
			want: []string{"nothing to mark"},
		},
		{
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

func TestTheReadCommandsStillTakeTheBase(t *testing.T) {
	f := marking(t)

	f.mustRun("files", "--base", "main")
}

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

	f.mustRun("review", "code.txt", "--all", "--generation", "2")

	if err := f.failure("review", "code.txt", "--all", "--generation", "0"); !strings.Contains(err.Error(), "0") {
		t.Errorf("err = %v, want it to name the generation it was given", err)
	}
}

func TestMarkingBeforeAnythingIsBuiltIsRefused(t *testing.T) {
	f := marking(t)

	err := f.failure("review", "code.txt", "--all")

	if !strings.Contains(err.Error(), "zen-review refresh") {
		t.Errorf("err = %v, want it to name what to run", err)
	}
}

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

func TestASingleLineIsARangeOfItself(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	w, _ := f.decodeState("review", "code.txt", "--lines", "3")

	got := state(w.Files, "code.txt")
	if got.State != "reviewed" {
		t.Errorf("code.txt = %s after --lines 3, want reviewed", got.State)
	}
}

func TestAFileWithNoHunksIsMarkedWhole(t *testing.T) {
	f := marking(t)
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

func TestADeletionOnlyHunkIsMarkedOnTheBase(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	w, _ := f.decodeState("review", "gone.txt", "--hunk", "1", "--side", "base")

	if got := state(w.Files, "gone.txt"); got.State != "reviewed" {
		t.Errorf("gone.txt = %s, want reviewed", got.State)
	}
}

func TestAWriteReportsTheBurnDownItMoved(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	out := f.mustRun("review", "code.txt", "--all")

	if !strings.Contains(out, "1 of 4 reviewed") {
		t.Errorf("output does not carry the burn-down:\n%s", out)
	}
}

func TestLinesReachingPastTheHunksDoNotOutliveTheReview(t *testing.T) {
	f := wide(t)
	f.mustRun("refresh")

	f.mustRun("review", "code.txt", "--lines", "1-1000")
	f.mustRun("unreview", "code.txt", "--all")

	f.Write("code.txt", numbered(1, 2)+"line 3 changed\n"+numbered(4, 23)+
		"line 24 changed\n"+numbered(25, 27)+"line 28 changed\n"+numbered(29, 30))
	f.mustRun("refresh")

	w, _ := f.decodeState("files")
	got := state(w.Files, "code.txt")
	if got.State != "unreviewed" {
		t.Errorf("code.txt = %s, want unreviewed: the review was taken back and nobody read the new hunk", got.State)
	}
	if got.Reviewed != 0 {
		t.Errorf("reviewed = %d of %d, want 0", got.Reviewed, got.Items)
	}
}

func wide(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("code.txt", numbered(1, 30))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 2)+"line 3 changed\n"+numbered(4, 30))
	return f
}

func state(files []stateFile, path string) stateFile {
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	return stateFile{}
}

func marking(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("code.txt", "one\ntwo\nthree\nfour\nfive\n")
	f.Write("gone.txt", "doomed\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")

	f.Write("code.txt", "one\ntwo\ninserted\nthree\nfour\nfive\n")
	f.Git("rm", "-q", "gone.txt")
	f.Write("added.txt", "new\n")

	f.Write("blob.bin", "\x00\x01binary\n")
	return f
}
