package cli_test

import (
	"strings"
	"testing"
)

// address is the agent's verb and resolve is the reader's, and each answers with
// the comment it moved so a script does not need a second command to see it.
func TestAddressThenResolveWalksAComment(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--hunk", "3", "--body", "here")

	claimed, _ := f.decodeComments("address", id)
	if got := only(t, claimed); got.ID != id || got.State != "addressed" {
		t.Errorf("address = %s %s, want %s addressed", got.ID, got.State, id)
	}

	closed, _ := f.decodeComments("resolve", id)
	if got := only(t, closed); got.State != "resolved" {
		t.Errorf("resolve = %s, want resolved", got.State)
	}
}

// The claim and the confirmation are different facts, so nothing an agent runs
// reaches resolved and nothing re-opens a comment that has stopped.
func TestAnAgentCannotReachResolved(t *testing.T) {
	f := clean(t)
	id := f.comment("code.txt", "--hunk", "3", "--body", "here")
	f.mustRun("resolve", id)

	err := f.failure("address", id)

	for _, want := range []string{id, "resolved", "addressed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %q", err, want)
		}
	}
}

// Resolve reaches everything not already closed, an orphan included: the code it
// was about is gone, and saying that settles it is the reader's call.
func TestResolvingClosesAnOrphan(t *testing.T) {
	f, ids := queue(t)

	w, _ := f.decodeComments("resolve", ids["orphaned"])

	if got := only(t, w); got.State != "resolved" {
		t.Errorf("state = %s, want resolved", got.State)
	}
}

// An id this session does not hold is a sentence, and the same one whichever
// verb was reaching for it. A comment belonging to another session is not found
// rather than refused: the database is shared by every session in the repository
// and one session's ids are not another's business.
func TestAnUnknownIdIsRefusedByBothVerbs(t *testing.T) {
	for _, verb := range []string{"address", "resolve"} {
		t.Run(verb, func(t *testing.T) {
			f := clean(t)

			err := f.failure(verb, "0123456789ab")

			for _, want := range []string{"no comment", "0123456789ab"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}
