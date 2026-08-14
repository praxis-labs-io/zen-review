package cli_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/cli"
)

// The listing is in the order a reader reads: by file, then down the file.
func TestCommentsComeBackInTheOrderTheyAreRead(t *testing.T) {
	f, _ := queue(t)

	w, _ := f.decodeComments("comments")

	want := []string{"code.txt 3", "code.txt 30", "gone.txt 0", "gone.txt 1"}
	if got := placed(w); !slices.Equal(got, want) {
		t.Errorf("listing = %v, want %v", got, want)
	}
}

// Every state a comment can be in, and the word for the ones still waiting on
// somebody. An orphaned comment is unresolved: the code moving under a comment
// is not an answer to it.
func TestTheStateFilterNarrowsTheListing(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  []string
	}{
		{state: "open", want: []string{"code.txt 30"}},
		{state: "orphaned", want: []string{"code.txt 3"}},
		{state: "addressed", want: []string{"gone.txt 1"}},
		{state: "resolved", want: []string{"gone.txt 0"}},
		{state: "unresolved", want: []string{"code.txt 3", "code.txt 30", "gone.txt 1"}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			f, _ := queue(t)

			w, _ := f.decodeComments("comments", "--state", tc.state)

			if got := placed(w); !slices.Equal(got, tc.want) {
				t.Errorf("--state %s = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// The totals are what a hook reads instead of counting the rows itself.
func TestTheTotalsCountEachStateAndTheQueue(t *testing.T) {
	f, _ := queue(t)

	w, _ := f.decodeComments("comments")

	got := w.Totals
	switch {
	case got.Comments != 4:
		t.Errorf("comments = %d, want 4", got.Comments)
	case got.Open != 1 || got.Addressed != 1 || got.Resolved != 1 || got.Orphaned != 1:
		t.Errorf("states = %+v, want one of each", got)
	case got.Unresolved != 3:
		t.Errorf("unresolved = %d, want 3: everything but the resolved one", got.Unresolved)
	}
}

// The path filter matches the name the comment is recorded under, which on the
// base side of a rename is the name the file has on the base.
func TestThePathFilterNarrowsTheListing(t *testing.T) {
	f, _ := queue(t)

	w, _ := f.decodeComments("comments", "--path", "gone.txt")

	if got := placed(w); !slices.Equal(got, []string{"gone.txt 0", "gone.txt 1"}) {
		t.Errorf("--path gone.txt = %v, want the two on it", got)
	}

	both, _ := f.decodeComments("comments", "--path", "gone.txt", "--state", "addressed")
	if got := placed(both); !slices.Equal(got, []string{"gone.txt 1"}) {
		t.Errorf("--path with --state = %v, want the one that is both", got)
	}
}

// --exit-code is the whole reason this surface exists: three lines of Stop hook
// and an agent that does not get to stop while a comment is open. A hook reading
// only "non-zero" cannot tell an open comment from a git failure, so the two
// statuses differ.
func TestTheExitCodeSaysWhetherTheFilterMatched(t *testing.T) {
	f, _ := queue(t)

	out, _, err := f.run("comments", "--state", "unresolved", "--exit-code")
	if got := cli.ExitCode(err); got != 1 {
		t.Errorf("exit = %d with three unresolved, want 1", got)
	}
	if !cli.Quiet(err) {
		t.Error("the matched status would be printed as an error, and it has nothing to say")
	}
	if !strings.Contains(out, "3 unresolved") {
		t.Errorf("the listing was withheld to report the status:\n%s", out)
	}

	_, _, none := f.run("comments", "--path", "elsewhere.txt", "--exit-code")
	if got := cli.ExitCode(none); got != 0 {
		t.Errorf("exit = %d with nothing matched, want 0", got)
	}

	_, _, broken := f.run("comments", "--state", "sideways", "--exit-code")
	if got := cli.ExitCode(broken); got != 2 {
		t.Errorf("exit = %d on a state that is not one, want 2", got)
	}
	if cli.Quiet(broken) {
		t.Error("a failure has something to say and would be swallowed")
	}
}

// Without --exit-code a listing is a listing, whatever it found.
func TestWithoutTheFlagAFullQueueIsStillASuccess(t *testing.T) {
	f, _ := queue(t)

	if _, _, err := f.run("comments", "--state", "unresolved"); err != nil {
		t.Errorf("comments = %v, want it to succeed and say so with its output", err)
	}
}

// Nothing matched is not the same answer as there being nothing, and a reader
// who mistyped a path would believe the wrong one.
func TestNothingMatchedRepeatsTheFilterBack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*testing.T) *fixture
		args  []string
		want  string
	}{
		{name: "no comments at all", build: clean, args: []string{"comments"},
			want: "no comments yet"},
		{name: "a state none is in", build: clean, args: []string{"comments", "--state", "resolved"},
			want: "no resolved comment"},
		{name: "a path none is on", build: commented, args: []string{"comments", "--path", "elsewhere.txt"},
			want: "no comment on elsewhere.txt"},
		{name: "both", build: commented, args: []string{"comments", "--state", "open", "--path", "elsewhere.txt"},
			want: "no open comment on elsewhere.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.build(t)

			out := f.mustRun(tc.args...)

			if !strings.Contains(out, tc.want) {
				t.Errorf("output does not say %q:\n%s", tc.want, out)
			}
		})
	}
}

// A state that is not one is a sentence rather than an empty list that looks
// like an answer.
func TestAStateThatIsNotOneIsRefused(t *testing.T) {
	f, _ := queue(t)

	err := f.failure("comments", "--state", "handled")

	for _, want := range []string{"handled", "open, addressed, resolved or orphaned", "unresolved"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

// The listing carries the body under the row naming it, and the count a reader
// is walking down.
func TestTheListingCarriesTheBodiesAndTheCount(t *testing.T) {
	f, _ := queue(t)

	out := f.mustRun("comments")

	for _, want := range []string{"still open", "4 comments, 3 unresolved"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not carry %q:\n%s", want, out)
		}
	}
}

// queue is a session holding one comment in each of the four states, so a filter
// has something to leave out.
//
// The orphan is made the only way one is made: a comment on a line, and then a
// refresh over that line rewritten. The line count does not move, so the comment
// further down the file stays exactly where it was.
func queue(t *testing.T) (*fixture, map[string]string) {
	t.Helper()

	f := spread(t)
	f.mustRun("refresh")

	ids := map[string]string{
		"open":      f.comment("code.txt", "--hunk", "30", "--body", "still open"),
		"orphaned":  f.comment("code.txt", "--lines", "3", "--body", "about the line that goes"),
		"addressed": f.comment("gone.txt", "--hunk", "1", "--side", "base", "--body", "why did this go"),
		"resolved":  f.comment("gone.txt", "--file", "--body", "the whole file"),
	}

	f.mustRun("address", ids["addressed"])
	f.mustRun("resolve", ids["resolved"])

	f.Write("code.txt", numbered(1, 2)+"line 3 replaced\n"+numbered(4, 29)+"line 30 changed\n"+numbered(31, 40))
	f.mustRun("refresh")

	return f, ids
}

// clean is a changeset nobody has said anything about.
func clean(t *testing.T) *fixture {
	t.Helper()

	f := spread(t)
	f.mustRun("refresh")
	return f
}

// commented is a changeset with one comment on it, which is what a filter
// matching none has to be told apart from.
func commented(t *testing.T) *fixture {
	t.Helper()

	f := clean(t)
	f.comment("code.txt", "--hunk", "3", "--body", "here")
	return f
}

// comment writes one and hands back the id, which is what address and resolve
// take.
func (f *fixture) comment(args ...string) string {
	f.t.Helper()

	w, _ := f.decodeComments(append([]string{"comment"}, args...)...)
	return only(f.t, w).ID
}

// placed is where each comment in a listing points, which is what an order or a
// filter is asserted on. The id and the body move between runs; the location
// does not.
func placed(w commentWire) []string {
	out := make([]string, 0, len(w.Comments))
	for _, c := range w.Comments {
		out = append(out, c.Path+" "+strconv.Itoa(c.Start))
	}
	return out
}
