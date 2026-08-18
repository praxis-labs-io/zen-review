package cli_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/golden"
)

// mixed is a changeset holding one of everything the summary rows can say.
func mixed(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("modified.txt", "before\n")
	f.Write("deleted.txt", "doomed\n")
	f.Write("renamed-from.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("modified.txt", "after\n")
	f.Git("rm", "-q", "deleted.txt")
	f.Git("mv", "renamed-from.txt", "renamed-to.txt")
	f.Write("added.txt", "never added to the index\n")
	return f
}

// The golden files lock the schema and not the values. Everything that moves
// between machines and runs is normalised out first, and what is left is the key
// set, the nesting, the enum spellings and which optional fields appear.
//
// That is what every strings.Contains assertion in this package misses: a
// renamed key sails through all of them and breaks whatever is parsing the
// output. What the normaliser destroys is asserted in ordinary tests instead,
// which is where base.sha being the merge base and the two bases diverging on a
// rebase get checked.
//
// So do not add value assertions here. Add them next to the behaviour they
// belong to and leave this locking the contract.
func TestTheJSONShapeIsTheContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(f *fixture) []string
	}{
		{
			// No generation at all, which is what the first run in a repository
			// reports and the one case where generation is null.
			name:  "no-generation",
			build: func(*fixture) []string { return []string{"status"} },
		},
		{
			name:  "refreshed",
			build: func(f *fixture) []string { f.mustRun("refresh"); return []string{"status"} },
		},
		{
			name: "stale",
			build: func(f *fixture) []string {
				f.mustRun("refresh")
				f.Write("modified.txt", "after the generation was built\n")
				return []string{"status"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := mixed(t)
			args := tc.build(f)

			w, raw := f.decode(args...)

			golden.Compare(t, tc.name, []byte(scrub(raw, w.wireHeader, candidateSubs(w)...)))
		})
	}
}

func candidateSubs(w wire) [][2]string {
	if w.Candidates == nil {
		return nil
	}
	var subs [][2]string
	for _, candidate := range append(w.Candidates.Local, w.Candidates.Remote...) {
		subs = append(subs, [2]string{candidate.SHA, "<sha>"})
	}
	return subs
}

// The changeset payload is the other contract, and the one a script marking
// hunks reads: it names each hunk by the side and line the write commands take.
// It locks the schema for the same reason and under the same rule.
func TestTheChangesetJSONShapeIsTheContract(t *testing.T) {
	f := mixed(t)
	f.mustRun("refresh")
	f.mustRun("review", "modified.txt", "--all")
	f.mustRun("review", "deleted.txt", "--hunk", "1", "--side", "base")

	w, raw := f.decodeState("files")

	golden.Compare(t, "files", []byte(scrub(raw, w.wireHeader)))
}

// The comment payload is the third contract, and the one a hook parses. It
// holds one of everything the surface can say: a line comment, a range, a file
// comment, both sides, and each of the four states.
func TestTheCommentsJSONShapeIsTheContract(t *testing.T) {
	f, _ := queue(t)

	w, raw := f.decodeComments("comments")

	golden.Compare(t, "comments", []byte(scrub(raw, w.wireHeader, commentSubs(w)...)))
}

// The summary payload is the fourth contract: the session keys every command
// promises, and the note. It locks the schema under the same rule.
func TestTheSummaryJSONShapeIsTheContract(t *testing.T) {
	f := clean(t)
	f.mustRun("summary", "--set", "held the store changes until the migration lands")

	w, raw := f.decodeSummary("summary")

	golden.Compare(t, "summary", []byte(scrub(raw, w.wireHeader)))
}

// commentSubs is what a comment carries that cannot be the same twice: the id,
// which is random, and the two timestamps.
func commentSubs(w commentWire) [][2]string {
	var subs [][2]string
	for _, c := range w.Comments {
		subs = append(subs,
			[2]string{c.ID, "<id>"},
			[2]string{c.CreatedAt, "<time>"},
			[2]string{c.UpdatedAt, "<time>"},
		)
	}
	return subs
}

// scrub replaces every value that cannot be the same twice: the session id,
// which hashes a temporary path, the shas, and the timestamp the engine stamps
// from the clock. more is whatever else the payload under test carries.
//
// The substitutions are the values the payload actually reported, read back off
// the parse, rather than patterns. A pattern for a 16-hex session id also
// matches inside a 40-hex sha and inside any path that happens to look like
// one, and getting two patterns to agree on which ran first is a bug waiting for
// a fixture that spells a word in hex.
func scrub(raw string, w wireHeader, more ...[2]string) string {
	subs := append([][2]string{
		{w.Session, "<session>"},
		{w.Base.SHA, "<sha>"},
	}, more...)
	if w.Generation != nil {
		subs = append(subs,
			[2]string{w.Generation.Commit, "<sha>"},
			[2]string{w.Generation.BaseSha, "<sha>"},
			[2]string{w.Generation.HeadSha, "<sha>"},
			[2]string{w.Generation.CreatedAt, "<time>"},
		)
	}

	// Longest first, so a value holding another inside it is replaced whole.
	slices.SortStableFunc(subs, func(a, b [2]string) int { return len(b[0]) - len(a[0]) })
	for _, sub := range subs {
		if sub[0] == "" {
			continue
		}
		raw = strings.ReplaceAll(raw, sub[0], sub[1])
	}
	return raw
}
