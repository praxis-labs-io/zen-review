package cli_test

import (
	"strings"
	"testing"
)

// A hunk is commented on as a region even where it holds one line. A selection
// of one is still a selection, and the hunk is what was selected.
func TestACommentOnAHunkTakesTheHunksLines(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	w, _ := f.decodeComments("comment", "code.txt", "--hunk", "3", "--body", "why is this here")

	got := only(t, w)
	switch {
	case got.Path != "code.txt":
		t.Errorf("path = %q, want code.txt", got.Path)
	case got.Side != "head":
		t.Errorf("side = %q, want head", got.Side)
	case got.Scope != "range":
		t.Errorf("scope = %q, want range: a hunk is a region", got.Scope)
	case got.Start != 3 || got.End != 3:
		t.Errorf("lines = %d:%d, want 3:3", got.Start, got.End)
	case got.State != "open":
		t.Errorf("state = %q, want open", got.State)
	case got.Body != "why is this here":
		t.Errorf("body = %q, want what was passed", got.Body)
	case got.ID == "":
		t.Error("the comment came back with no id, which is what address and resolve take")
	}
}

// The scope falls out of the lines rather than out of how they were typed, so
// one line is a line comment and more than one is a range.
func TestTheScopeFollowsTheLines(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lines      string
		scope      string
		start, end int
	}{
		{name: "one line", lines: "3", scope: "line", start: 3, end: 3},
		{name: "the same line spelled as a range", lines: "3-3", scope: "line", start: 3, end: 3},
		{name: "two hunks and everything between", lines: "3-30", scope: "range", start: 3, end: 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := spread(t)
			f.mustRun("refresh")

			w, _ := f.decodeComments("comment", "code.txt", "--lines", tc.lines, "--body", "here")

			got := only(t, w)
			if got.Scope != tc.scope {
				t.Errorf("scope = %q, want %q", got.Scope, tc.scope)
			}
			if got.Start != tc.start || got.End != tc.end {
				t.Errorf("lines = %d:%d, want %d:%d", got.Start, got.End, tc.start, tc.end)
			}
		})
	}
}

// Lines are stored as they were given. Clipping a mark narrows a claim about how
// much was read; clipping a comment moves what somebody said onto lines they did
// not pick, so a comment about two hunks stays one comment about both.
func TestACommentSpanningTwoHunksIsKeptAsTyped(t *testing.T) {
	f := spread(t)
	f.mustRun("refresh")

	w, _ := f.decodeComments("comment", "code.txt", "--lines", "3-30", "--body", "these two belong together")

	if got := only(t, w); got.Start != 3 || got.End != 30 {
		t.Errorf("lines = %d:%d, want the 3:30 that was typed", got.Start, got.End)
	}
}

// A file comment names the file rather than any line in it, and takes the side
// the file has bytes on: a deleted file has none on the head, and an anchor
// there would survive every rewrite of the bytes it actually removed.
func TestAFileCommentNamesTheFileOnTheSideItHasBytesOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		side string
	}{
		{name: "a file with no lines to name", path: "blob.bin", side: "head"},
		{name: "a file that was deleted", path: "gone.txt", side: "base"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := marking(t)
			f.mustRun("refresh")

			w, _ := f.decodeComments("comment", tc.path, "--file", "--body", "about the whole thing")

			got := only(t, w)
			if got.Scope != "file" {
				t.Errorf("scope = %q, want file", got.Scope)
			}
			if got.Side != tc.side {
				t.Errorf("side = %q, want %s", got.Side, tc.side)
			}
			if got.Start != 0 || got.End != 0 {
				t.Errorf("lines = %d:%d, want 0:0: a file comment names no line", got.Start, got.End)
			}
		})
	}
}

// A deletion-only hunk has no head-side lines, so --side base is the only way to
// name it, exactly as it is for a mark.
func TestADeletionOnlyHunkIsCommentedOnTheBase(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	w, _ := f.decodeComments("comment", "gone.txt", "--hunk", "1", "--side", "base", "--body", "why did this go")

	if got := only(t, w); got.Side != "base" || got.Start != 1 {
		t.Errorf("comment = %s %d:%d, want base 1", got.Side, got.Start, got.End)
	}
}

// A base-side comment is recorded under the name the file has on the base, which
// a rename makes a different one. The diff a refresh translates base-side
// anchors through knows only that name.
func TestABaseSideCommentIsRecordedUnderTheBaseName(t *testing.T) {
	f := renamed(t)
	f.mustRun("refresh")

	w, _ := f.decodeComments("comment", "new.txt", "--lines", "5", "--side", "base", "--body", "this line went")

	if got := only(t, w); got.Path != "old.txt" {
		t.Errorf("path = %q, want old.txt: a base-side anchor is stored under the base name", got.Path)
	}
}

// A body arrives on stdin so one with newlines does not have to survive a shell.
func TestABodyCanArriveOnStdin(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	f.stdin = strings.NewReader("the first thing\n\nand the second\n")
	w, _ := f.decodeComments("comment", "code.txt", "--hunk", "3", "--body", "-")

	want := "the first thing\n\nand the second"
	if got := only(t, w); got.Body != want {
		t.Errorf("body = %q, want %q: the trailing newline a heredoc leaves is not part of it", got.Body, want)
	}
}

// A body of several lines reads as one comment under the row naming it, and no
// line of the output ends in whitespace.
func TestAMultiLineBodyIsIndentedUnderItsRow(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	f.stdin = strings.NewReader("the first thing\n\nand the second\n")
	out := f.mustRun("comment", "code.txt", "--hunk", "3", "--body", "-")

	for _, want := range []string{"    the first thing\n", "    and the second\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not carry %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("a line ends in whitespace: %q", line)
		}
	}
}

// Every way of getting the flags wrong, and what the reader is told to do about
// it. None of these messages opens on a flag name or a path: the first letter is
// capitalised on the way out, which would print a --Hunk that does not exist.
func TestTheCommentFlagsRefuseWhatTheyCannotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "nothing named",
			args: []string{"comment", "code.txt", "--body", "here"},
			want: []string{"--hunk", "--lines", "--file"},
		},
		{
			name: "two things named",
			args: []string{"comment", "code.txt", "--file", "--hunk", "3", "--body", "here"},
			want: []string{"pass one"},
		},
		{
			// The side is the one the file has bytes on, which a file comment does
			// not get to choose any more than a whole-file mark does.
			name: "a side under --file",
			args: []string{"comment", "code.txt", "--file", "--side", "base", "--body", "here"},
			want: []string{"side is not a choice"},
		},
		{
			name: "no body at all",
			args: []string{"comment", "code.txt", "--hunk", "3"},
			want: []string{"--body", "stdin"},
		},
		{
			name: "a body of nothing but space",
			args: []string{"comment", "code.txt", "--hunk", "3", "--body", "   "},
			want: []string{"says nothing"},
		},
		{
			name: "a file the changeset does not hold",
			args: []string{"comment", "elsewhere.txt", "--file", "--body", "here"},
			want: []string{"elsewhere.txt", "zen-review files"},
		},
		{
			name: "a hunk no line names",
			args: []string{"comment", "code.txt", "--hunk", "99", "--body", "here"},
			want: []string{"code.txt", "head 99", "zen-review files"},
		},
		{
			name: "lines on the side the file has none on",
			args: []string{"comment", "gone.txt", "--lines", "1", "--body", "here"},
			want: []string{"gone.txt", "head side"},
		},
		{
			name: "lines on a file no line names",
			args: []string{"comment", "blob.bin", "--lines", "1", "--body", "here"},
			want: []string{"blob.bin", "--file"},
		},
		{
			// Line 0 is the file as a whole rather than a line in it. parseLines is
			// shared with the mark commands, so the refusal has to point at this
			// command's way of naming a file and not at their --all.
			name: "a line 0 that means the whole file",
			args: []string{"comment", "code.txt", "--lines", "0-3", "--body", "here"},
			want: []string{"start at 1", "--file"},
		},
		{
			// A flag given an empty value is a flag that was passed, so this is the
			// --lines branch being refused rather than the --hunk one.
			name: "lines given as nothing",
			args: []string{"comment", "code.txt", "--lines=", "--body", "here"},
			want: []string{"A-B", `""`},
		},
		{
			// The other spelling of the same trap. A bool flag is set either way,
			// and off is not a way of naming something to comment on.
			name: "file turned off",
			args: []string{"comment", "code.txt", "--file=false", "--body", "here"},
			want: []string{"nothing to comment on"},
		},
		{
			name: "no path at all",
			args: []string{"comment", "--file", "--body", "here"},
			want: []string{"arg"},
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

// Lines no hunk holds are refused rather than clipped, and the refusal names the
// lines the file does hold so the next attempt is a correction rather than a
// guess. A comment anchored outside every hunk is on nothing a reader can be
// shown, and it would carry into each new generation drifting as it goes.
func TestLinesNoHunkHoldsAreRefusedAndTheHunksAreNamed(t *testing.T) {
	f := spread(t)
	f.mustRun("refresh")

	err := f.failure("comment", "code.txt", "--lines", "15-20", "--body", "here")

	for _, want := range []string{"code.txt", "between 15 and 20", "it holds 3, 30"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

// None of the three writes takes --base. Open writes it back, so it stays
// moved: closing a comment is not where a reader decides what the changeset is
// measured from, and on the write itself the move recomputes the changeset the
// comment then anchors into.
func TestTheCommentWritesRefuseToMoveTheBase(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")
	id := f.comment("code.txt", "--file", "--body", "here")

	for _, args := range [][]string{
		{"comment", "code.txt", "--hunk", "3", "--body", "here", "--base", "main"},
		{"address", id, "--base", "main"},
		{"resolve", id, "--base", "main"},
	} {
		t.Run(args[0], func(t *testing.T) {
			err := f.failure(args...)

			for _, want := range []string{"--base", "zen-review status"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

// There is nothing for a comment to anchor to, so this refuses where files
// reports.
func TestCommentingBeforeAnythingIsBuiltIsRefused(t *testing.T) {
	f := marking(t)

	err := f.failure("comment", "code.txt", "--file", "--body", "here")

	if !strings.Contains(err.Error(), "zen-review refresh") {
		t.Errorf("err = %v, want it to name what to run", err)
	}
}

// A comment anchored to a generation that is no longer the latest is inert: the
// carry runs from the latest, so nothing would ever pick it up and it would
// never move again.
func TestACommentNamingAnOlderGenerationIsRefused(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	f.Write("code.txt", "one\ntwo\ninserted\nthree\nfour\nFIVE\n")
	f.mustRun("refresh")

	err := f.failure("comment", "code.txt", "--file", "--body", "here", "--generation", "1")

	for _, want := range []string{"1", "2", "refresh"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}

	f.mustRun("comment", "code.txt", "--file", "--body", "here", "--generation", "2")
}

// renamed is a file moved and edited in one branch, which is the case where the
// name a base-side anchor is stored under is not the file's own.
func renamed(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("old.txt", numbered(1, 10))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Git("mv", "old.txt", "new.txt")
	f.Write("new.txt", numbered(1, 4)+"line 5 changed\n"+numbered(6, 10))
	return f
}

// only is the one comment a write answers with.
func only(t *testing.T, w commentWire) commentEntry {
	t.Helper()

	if len(w.Comments) != 1 {
		t.Fatalf("the write answered with %d comments, want 1", len(w.Comments))
	}
	return w.Comments[0]
}
