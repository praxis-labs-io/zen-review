package cli_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestFilesBuildsNothing(t *testing.T) {
	f := edited(t)

	out := f.mustRun("files")

	if !strings.Contains(out, "no generation yet, so run zen-review refresh") {
		t.Errorf("output does not say there is nothing to report:\n%s", out)
	}
	if refs := f.sessionRefs(); refs != nil {
		t.Errorf("refs = %v, want files to have written none", refs)
	}
}

func TestEveryHunkFilesNamesCanBeMarkedByThatName(t *testing.T) {
	f := spread(t)
	f.mustRun("refresh")

	w, _ := f.decodeState("files")
	if len(w.Files) == 0 {
		t.Fatal("the changeset holds no files")
	}

	spaced := false
	marked := 0
	for _, file := range w.Files {
		for i, h := range file.Hunks {
			f.mustRun("review", file.Path, "--hunk", strconv.Itoa(h.Line), "--side", h.Side)
			spaced = spaced || h.Line != i+1
			marked++
		}
	}
	if marked == 0 {
		t.Fatal("no file in the changeset has a hunk, so nothing was named")
	}
	if !spaced {
		t.Fatal("every hunk is named by its own position, so this proves nothing about the name")
	}

	after, _ := f.decodeState("files")
	for _, file := range after.Files {
		if len(file.Hunks) > 0 && file.State != "reviewed" {
			t.Errorf("%s = %s after every hunk of it was marked by name, want reviewed", file.Path, file.State)
		}
	}
}

func TestAPartlyReadHunkReadsPartial(t *testing.T) {
	f := lined(t)
	f.mustRun("refresh")

	f.mustRun("review", "code.txt", "--lines", "3-4")

	w, _ := f.decodeState("files")
	file := w.Files[0]
	if file.State != "partial" {
		t.Errorf("state = %s, want partial", file.State)
	}
	if file.Reviewed != 0 || file.Items != 1 {
		t.Errorf("reviewed = %d of %d, want 0 of 1: a hunk read in part is not read",
			file.Reviewed, file.Items)
	}
	if w.Totals.Reviewed != 0 || w.Totals.Items != 1 {
		t.Errorf("totals = %d of %d, want 0 of 1", w.Totals.Reviewed, w.Totals.Items)
	}
}

func TestFilesReportsWhatTheRefreshCutFromAFile(t *testing.T) {
	f := lined(t)
	f.mustRun("refresh")
	f.mustRun("review", "code.txt", "--all")

	f.Write("code.txt", "one\nrewritten\nwholesale\nand again\nfive\n")
	f.mustRun("refresh")

	w, _ := f.decodeState("files")
	if !w.Files[0].Changed {
		t.Error("changed = false, want the refresh to have recorded what it took")
	}

	out := f.mustRun("files")
	if !strings.Contains(out, "changed after review") {
		t.Errorf("the prose does not say the file moved under the mark:\n%s", out)
	}
}

func spread(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("code.txt", numbered(1, 40))
	f.Write("gone.txt", "doomed\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 2)+"line 3 changed\n"+numbered(4, 29)+"line 30 changed\n"+numbered(31, 40))
	f.Git("rm", "-q", "gone.txt")
	return f
}

func numbered(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func lined(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("code.txt", "one\ntwo\nthree\nfour\nfive\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", "one\ntwo\nTHREE\nFOUR\nfive\n")
	return f
}
