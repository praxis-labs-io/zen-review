package cli_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/golden"
)

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

func TestTheJSONShapeIsTheContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(f *fixture) []string
	}{
		{
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

func TestTheChangesetJSONShapeIsTheContract(t *testing.T) {
	f := mixed(t)
	f.mustRun("refresh")
	f.mustRun("review", "modified.txt", "--all")
	f.mustRun("review", "deleted.txt", "--hunk", "1", "--side", "base")

	w, raw := f.decodeState("files")

	golden.Compare(t, "files", []byte(scrub(raw, w.wireHeader)))
}

func TestTheCommentsJSONShapeIsTheContract(t *testing.T) {
	f, _ := queue(t)

	w, raw := f.decodeComments("comments")

	golden.Compare(t, "comments", []byte(scrub(raw, w.wireHeader, commentSubs(w)...)))
}

func TestTheSummaryJSONShapeIsTheContract(t *testing.T) {
	f := clean(t)
	f.mustRun("summary", "--set", "held the store changes until the migration lands")

	w, raw := f.decodeSummary("summary")

	golden.Compare(t, "summary", []byte(scrub(raw, w.wireHeader)))
}

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

	slices.SortStableFunc(subs, func(a, b [2]string) int { return len(b[0]) - len(a[0]) })
	for _, sub := range subs {
		if sub[0] == "" {
			continue
		}
		raw = strings.ReplaceAll(raw, sub[0], sub[1])
	}
	return raw
}
