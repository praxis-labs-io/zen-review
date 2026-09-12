package cli_test

import (
	"strings"
	"testing"
)

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

func TestAddressCarriesTheResponse(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "why is this here")

	w, _ := f.decodeComments("address", id, "--body", "the retry loop needs it first")

	got := only(t, w)
	if got.Response != "the retry loop needs it first" {
		t.Errorf("response = %q, want what was passed", got.Response)
	}
	if got.Body != "why is this here" {
		t.Errorf("body = %q, want the reader's words left alone", got.Body)
	}
	if got.State != "addressed" {
		t.Errorf("state = %q, want addressed", got.State)
	}
}

func TestAddressStillTakesNoWordsAtAll(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "cap this")

	w, _ := f.decodeComments("address", id)

	got := only(t, w)
	if got.Response != "" {
		t.Errorf("response = %q, want none", got.Response)
	}
	if got.State != "addressed" {
		t.Errorf("state = %q, want addressed", got.State)
	}
}

func TestAResponseCanArriveOnStdin(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "why")

	f.stdin = strings.NewReader("the first reason\n\nand the second\n")
	w, _ := f.decodeComments("address", id, "--body", "-")

	want := "the first reason\n\nand the second"
	if got := only(t, w); got.Response != want {
		t.Errorf("response = %q, want %q", got.Response, want)
	}
}

func TestAResponseOpeningOnABlankLineKeepsItsElbow(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--lines", "3", "--body", "why")

	f.stdin = strings.NewReader("\nthe response starts here\n")
	f.mustRun("address", id, "--body", "-")

	out := f.mustRun("comments")
	if !strings.Contains(out, "╰─ the response starts here") {
		t.Errorf("the response printed with no elbow:\n%s", out)
	}
}
