package cli_test

import (
	"strings"
	"testing"
)

// The body is the whole of an edit, and the answer is the comment as it now
// stands so a script does not need a second command to read it back.
func TestEditRewritesTheBodyAndLeavesTheAnchor(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "this reads backwards")

	before, _ := f.decodeComments("comments")
	was := only(t, before)

	w, _ := f.decodeComments("edit", id, "--body", "this reads forwards")

	got := only(t, w)
	if got.Body != "this reads forwards" {
		t.Errorf("body = %q, want what was passed", got.Body)
	}
	if got.Path != was.Path || got.Side != was.Side || got.Start != was.Start || got.End != was.End {
		t.Errorf("anchor = %s %s %d-%d, want it left at %s %s %d-%d",
			got.Path, got.Side, got.Start, got.End, was.Path, was.Side, was.Start, was.End)
	}
}

// A rewritten body arrives on stdin the way a new one does, so prose with
// newlines in it does not have to survive a shell.
func TestAnEditBodyCanArriveOnStdin(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "here")

	f.stdin = strings.NewReader("the first thing\n\nand the second\n")
	w, _ := f.decodeComments("edit", id, "--body", "-")

	want := "the first thing\n\nand the second"
	if got := only(t, w); got.Body != want {
		t.Errorf("body = %q, want %q", got.Body, want)
	}
}

// An edit with no --body is a comment about to be blanked, which the engine
// refuses anyway. The refusal names the flag and how to reach stdin with it.
func TestAnEditWithNoBodyIsRefused(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "here")

	err := f.failure("edit", id)

	for _, want := range []string{"--body", "stdin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %q", err, want)
		}
	}
}

// A delete prints the row that went, because nothing else can say what the
// comment said once it has gone, and the session is left holding none.
func TestDeleteHandsBackTheCommentThatWent(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "never mind")

	w, _ := f.decodeComments("delete", id)
	if got := only(t, w); got.ID != id || got.Body != "never mind" {
		t.Errorf("delete = %s %q, want %s and what it said", got.ID, got.Body, id)
	}

	left, _ := f.decodeComments("comments")
	if len(left.Comments) != 0 {
		t.Errorf("the session still holds %v", placed(left))
	}
}

// Both reach a comment in any state: a typo in a resolved comment is still a
// typo, and one nobody meant to write is a record of nothing.
func TestEditAndDeleteReachAResolvedComment(t *testing.T) {
	for _, verb := range []string{"edit", "delete"} {
		t.Run(verb, func(t *testing.T) {
			f := clean(t)
			id := f.comment("code.txt", "--lines", "3", "--body", "here")
			f.mustRun("resolve", id)

			args := []string{verb, id}
			if verb == "edit" {
				args = append(args, "--body", "still worth saying")
			}
			f.mustRun(args...)
		})
	}
}

// An id this session does not hold is a sentence, and the same one whichever
// verb was reaching for it.
func TestAnUnknownIdIsRefusedByEditAndDelete(t *testing.T) {
	for _, verb := range []string{"edit", "delete"} {
		t.Run(verb, func(t *testing.T) {
			f := clean(t)

			args := []string{verb, "0123456789ab"}
			if verb == "edit" {
				args = append(args, "--body", "hello")
			}
			err := f.failure(args...)

			for _, want := range []string{"no comment", "0123456789ab"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}
