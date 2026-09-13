package cli_test

import (
	"strings"
	"testing"
)

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
	case got.Scope != "hunk":
		t.Errorf("scope = %q, want hunk: the reader named the hunk, not its lines", got.Scope)
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

func TestACommentSpanningTwoHunksIsKeptAsTyped(t *testing.T) {
	f := spread(t)
	f.mustRun("refresh")

	w, _ := f.decodeComments("comment", "code.txt", "--lines", "3-30", "--body", "these two belong together")

	if got := only(t, w); got.Start != 3 || got.End != 30 {
		t.Errorf("lines = %d:%d, want the 3:30 that was typed", got.Start, got.End)
	}
}

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

func TestADeletionOnlyHunkIsCommentedOnTheBase(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	w, _ := f.decodeComments("comment", "gone.txt", "--hunk", "1", "--side", "base", "--body", "why did this go")

	if got := only(t, w); got.Side != "base" || got.Start != 1 {
		t.Errorf("comment = %s %d:%d, want base 1", got.Side, got.Start, got.End)
	}
}

func TestABaseSideCommentIsRecordedUnderTheBaseName(t *testing.T) {
	f := renamed(t)
	f.mustRun("refresh")

	w, _ := f.decodeComments("comment", "new.txt", "--lines", "5", "--side", "base", "--body", "this line went")

	if got := only(t, w); got.Path != "old.txt" {
		t.Errorf("path = %q, want old.txt: a base-side anchor is stored under the base name", got.Path)
	}
}

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

func TestABodyKeepsTheIndentItArrivedWith(t *testing.T) {
	f := marking(t)
	f.mustRun("refresh")

	f.stdin = strings.NewReader("    a line somebody indented\n\n")
	w, _ := f.decodeComments("comment", "code.txt", "--hunk", "3", "--body", "-")

	want := "    a line somebody indented"
	if got := only(t, w); got.Body != want {
		t.Errorf("body = %q, want %q", got.Body, want)
	}
}

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
			name: "a line 0 that means the whole file",
			args: []string{"comment", "code.txt", "--lines", "0-3", "--body", "here"},
			want: []string{"start at 1", "--file"},
		},
		{
			name: "lines given as nothing",
			args: []string{"comment", "code.txt", "--lines=", "--body", "here"},
			want: []string{"A-B", `""`},
		},
		{
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

func TestCommentingBeforeAnythingIsBuiltIsRefused(t *testing.T) {
	f := marking(t)

	err := f.failure("comment", "code.txt", "--file", "--body", "here")

	if !strings.Contains(err.Error(), "zen-review refresh") {
		t.Errorf("err = %v, want it to name what to run", err)
	}
}

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

func only(t *testing.T, w commentWire) commentEntry {
	t.Helper()

	if len(w.Comments) != 1 {
		t.Fatalf("the write answered with %d comments, want 1", len(w.Comments))
	}
	return w.Comments[0]
}
