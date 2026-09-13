package review_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func numbered(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func shown(rs []store.ReviewedRange) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, fmt.Sprintf("%s %s %d:%d", r.Path, r.Side, r.Start, r.End))
	}
	return out
}

func (f *fixture) storedRanges(g review.Generation) []string {
	f.t.Helper()

	rs, err := f.db().ReviewedRanges(f.t.Context(), g.ID)
	if err != nil {
		f.t.Fatalf("reading the reviewed ranges of generation %d: %v", g.Seq, err)
	}
	return shown(rs)
}

func (f *fixture) mark(s *review.Session, g review.Generation, path string, side store.Side, rs ...review.Range) {
	f.t.Helper()

	if err := s.Mark(f.t.Context(), g, path, side, rs); err != nil {
		f.t.Fatalf("marking %s: %v", path, err)
	}
}

func assertRanges(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("ranges = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("range %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func marked(t *testing.T) (*fixture, *review.Session, review.Generation) {
	t.Helper()

	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	f.mark(s, g, "code.txt", store.SideHead, review.Range{Start: 5, End: 9})
	return f, s, g
}

func TestAMarkLandsAgainstTheGenerationItNames(t *testing.T) {
	f, s, g := marked(t)

	assertRanges(t, f.storedRanges(g), []string{"code.txt head 5:9"})

	rs, err := s.Reviewed(t.Context(), g)
	if err != nil {
		t.Fatalf("reading the reviewed ranges: %v", err)
	}
	assertRanges(t, shown(rs), []string{"code.txt head 5:9"})
	if len(rs) > 0 && rs[0].CreatedAt.IsZero() {
		t.Error("the range came back without a time it was read")
	}
}

func TestAMarkFollowsItsLinesIntoTheNextGeneration(t *testing.T) {
	tests := []struct {
		name string
		edit func(f *fixture)
		want []string
	}{
		{
			"a line inserted above shifts it down",
			func(f *fixture) { f.Write("code.txt", "inserted\n"+numbered(1, 20)) },
			[]string{"code.txt head 6:10"},
		},
		{
			"a line inserted inside splits it, and both pieces survive",
			func(f *fixture) { f.Write("code.txt", numbered(1, 7)+"inserted\n"+numbered(8, 20)) },
			[]string{"code.txt head 5:7", "code.txt head 9:10"},
		},
		{
			"a reviewed line deleted closes the gap",
			func(f *fixture) { f.Write("code.txt", numbered(1, 6)+numbered(8, 20)) },
			[]string{"code.txt head 5:8"},
		},
		{
			"a region rewritten wholesale keeps nothing",
			func(f *fixture) {
				f.Write("code.txt", numbered(1, 4)+"alpha\nbeta\ngamma\ndelta\nepsilon\n"+numbered(10, 20))
			},
			nil,
		},
		{
			"an edit elsewhere in the file leaves it where it was",
			func(f *fixture) { f.Write("code.txt", numbered(1, 20)+"appended\n") },
			[]string{"code.txt head 5:9"},
		},
		{
			"a rename carries it to the new path",
			func(f *fixture) { f.Git("mv", "code.txt", "moved.txt") },
			[]string{"moved.txt head 5:9"},
		},
		{
			"a deleted file takes it with it",
			func(f *fixture) { f.Git("rm", "-q", "-f", "code.txt") },
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s, first := marked(t)
			tt.edit(f)

			next := f.refresh(s)
			if next.ID == first.ID {
				t.Fatal("the edit built no new generation, so nothing was carried")
			}
			assertRanges(t, f.storedRanges(next), tt.want)
		})
	}
}

func TestARebaseCarriesEveryMark(t *testing.T) {
	f, s, first := marked(t)

	f.Git("checkout", "-q", "main")
	f.Write("upstream.txt", "new upstream work\n")
	f.Commit("upstream")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("rebase", "-q", "main")

	next := f.refresh(s)
	if next.ID == first.ID {
		t.Fatal("the rebase built no new generation")
	}
	assertRanges(t, f.storedRanges(next), []string{"code.txt head 5:9"})
}

func TestABaseSideMarkTranslatesWhenTheBaseMoves(t *testing.T) {
	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 9)+numbered(13, 20))
	f.Commit("drop three lines")

	s := f.mustOpen("")
	first := f.refresh(s)
	f.mark(s, first, "code.txt", store.SideBase, review.Range{Start: 10, End: 12})

	f.Git("checkout", "-q", "main")
	f.Write("code.txt", "upstream\n"+numbered(1, 20))
	f.Commit("upstream")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("rebase", "-q", "main")

	next := f.refresh(s)
	if next.ID == first.ID {
		t.Fatal("the base move built no new generation")
	}
	assertRanges(t, f.storedRanges(next), []string{"code.txt base 11:13"})
}

func TestABaseSideMarkOnARenamedFileIsKeyedByTheBaseName(t *testing.T) {
	f := newFixture(t)
	f.Write("old.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Git("mv", "old.txt", "new.txt")
	f.Write("new.txt", numbered(1, 9)+numbered(13, 20))
	f.Commit("move it and drop three lines")

	s := f.mustOpen("")
	first := f.refresh(s)

	f.mark(s, first, "new.txt", store.SideBase, review.Range{Start: 10, End: 12})
	assertRanges(t, f.storedRanges(first), []string{"old.txt base 10:12"})

	f.Git("checkout", "-q", "main")
	f.Write("old.txt", "upstream\n"+numbered(1, 20))
	f.Commit("upstream")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("rebase", "-q", "main")

	next := f.refresh(s)
	if next.ID == first.ID {
		t.Fatal("the base move built no new generation")
	}
	assertRanges(t, f.storedRanges(next), []string{"old.txt base 11:13"})
}

func TestAWholeFileMarkGoesWhenTheFileGainsHunks(t *testing.T) {
	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 20)+"added by the branch\n")
	f.Commit("add a line")
	f.Git("mv", "code.txt", "moved.txt")
	f.Commit("move it")

	near := f.mustOpen("feature~1")
	first := f.refresh(near)
	f.mark(near, first, "moved.txt", store.SideHead, review.Range{})
	assertRanges(t, f.storedRanges(first), []string{"moved.txt head 0:0"})
	if err := near.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	far := f.mustOpen("origin/main")
	next := f.refresh(far)
	if next.ID == first.ID {
		t.Fatal("the base change built no new generation")
	}
	assertRanges(t, f.storedRanges(next), nil)
}

func TestABaseForcePushThatLosesTheForkPointKeepsTheMarks(t *testing.T) {
	f, s, _ := marked(t)

	f.Git("checkout", "-q", "--orphan", "unrelated")
	f.commit("nothing in common")
	orphan := f.Git("rev-parse", "HEAD")
	f.Git("checkout", "-q", "feature")
	f.Git("update-ref", "refs/remotes/origin/main", orphan)

	f.Write("code.txt", numbered(1, 21))

	next, err := s.Refresh(t.Context())
	if err != nil {
		t.Fatalf("refreshing past a lost fork point: %v", err)
	}
	if s.Base().Ref != "main" {
		t.Errorf("base = %s, want the local main it fell back to", s.Base().Ref)
	}
	if s.Base().Fallback != "not origin/main" {
		t.Errorf("fallback = %q, want it to say what it is not measured from", s.Base().Fallback)
	}
	assertRanges(t, f.storedRanges(next), []string{"code.txt head 5:9"})
}

func TestAMarkAgainstAnOldGenerationIsRefused(t *testing.T) {
	f, s, first := marked(t)

	f.Write("code.txt", numbered(1, 21))
	next := f.refresh(s)

	err := s.Mark(t.Context(), first, "code.txt", store.SideHead, []review.Range{{Start: 15, End: 16}})

	var stale *review.StaleGenerationError
	if !errors.As(err, &stale) {
		t.Fatalf("err = %v (%T), want *review.StaleGenerationError", err, err)
	}
	if stale.Seq != first.Seq || stale.Current != next.Seq {
		t.Errorf("err = %+v, want generation %d refused in favour of %d", stale, first.Seq, next.Seq)
	}
	assertRanges(t, f.storedRanges(first), []string{"code.txt head 5:9"})
}

func TestUnmarkingCutsOnlyTheLinesItNames(t *testing.T) {
	f, s, g := marked(t)

	f.mark(s, g, "code.txt", store.SideHead, review.Range{Start: 15, End: 18})
	assertRanges(t, f.storedRanges(g), []string{"code.txt head 5:9", "code.txt head 15:18"})

	if err := s.Unmark(t.Context(), g, "code.txt", store.SideHead, []review.Range{{Start: 7, End: 16}}); err != nil {
		t.Fatalf("unmarking: %v", err)
	}
	assertRanges(t, f.storedRanges(g), []string{"code.txt head 5:6", "code.txt head 17:18"})

	f.mark(s, g, "code.txt", store.SideHead, review.Range{Start: 7, End: 16})
	assertRanges(t, f.storedRanges(g), []string{"code.txt head 5:18"})
}

func TestAWholeFileMarkComesOffOnlyToAWholeFileUnmark(t *testing.T) {
	f, s, g := marked(t)

	f.mark(s, g, "code.txt", store.SideHead, review.Range{})
	assertRanges(t, f.storedRanges(g), []string{"code.txt head 0:0", "code.txt head 5:9"})

	if err := s.Unmark(t.Context(), g, "code.txt", store.SideHead, []review.Range{{Start: 1, End: 30}}); err != nil {
		t.Fatalf("unmarking every line: %v", err)
	}
	assertRanges(t, f.storedRanges(g), []string{"code.txt head 0:0"})

	if err := s.Unmark(t.Context(), g, "code.txt", store.SideHead, []review.Range{{}}); err != nil {
		t.Fatalf("unmarking the file as a whole: %v", err)
	}
	assertRanges(t, f.storedRanges(g), nil)
}

func TestAWholeFileMarkSurvivesARefreshThatLeavesTheFileAlone(t *testing.T) {
	f := branched(t)
	f.Write("blob.bin", "\x00\x01binary\n")
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add both")

	s := f.mustOpen("")
	first := f.refresh(s)
	f.mark(s, first, "blob.bin", store.SideHead, review.Range{})
	assertRanges(t, f.storedRanges(first), []string{"blob.bin head 0:0"})

	f.Write("code.txt", numbered(1, 21))
	next := f.refresh(s)

	assertRanges(t, f.storedRanges(next), []string{"blob.bin head 0:0"})
}
