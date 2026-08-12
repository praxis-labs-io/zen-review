package review_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// numbered is a file whose every line names itself, so a diff of two of them
// says exactly which lines moved and an assertion can name them back.
func numbered(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// shown is what the tables below read: one string per range, path first.
func shown(rs []store.ReviewedRange) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, fmt.Sprintf("%s %s %d:%d", r.Path, r.Side, r.Start, r.End))
	}
	return out
}

// storedRanges reads through a second handle on the database rather than through
// the session that wrote, so a method returning the right value while writing
// nothing cannot pass.
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

// marked is the state every carry case starts from: a committed file of twenty
// numbered lines in the changeset, generation one built, and lines 5 to 9 read.
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

// The whole feature: a mark is line ranges, and a new generation is where those
// lines went. What is never true is a line the edit introduced sitting inside a
// range that survived.
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

// A rebase replays the same lines onto a newer upstream. Nothing about the file
// changed, so the review of it comes through whole.
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

// A deletion-only hunk has no head-side lines and anchors to the base blob, so
// the base moving is what moves the mark. It is the only reason the base diff
// runs at all.
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

	// Upstream inserts a line at the top of the same file and the branch replays
	// onto it, so every base-side line the mark names moved down one.
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

// The fork point going away has no answer, so the refresh refuses. What it must
// not do is leave the review in a state that reads as unreviewed.
func TestABaseForcePushThatLosesTheForkPointKeepsTheMarks(t *testing.T) {
	f, s, first := marked(t)

	f.Git("checkout", "-q", "--orphan", "unrelated")
	f.commit("nothing in common")
	orphan := f.Git("rev-parse", "HEAD")
	f.Git("checkout", "-q", "feature")
	f.Git("update-ref", "refs/remotes/origin/main", orphan)

	f.Write("code.txt", numbered(1, 21))

	_, err := s.Refresh(t.Context())
	var gone *review.NoMergeBaseError
	if !errors.As(err, &gone) {
		t.Fatalf("err = %v (%T), want *review.NoMergeBaseError", err, err)
	}
	assertRanges(t, f.storedRanges(first), []string{"code.txt head 5:9"})
}

// A mark against an old generation is not merely stale, it is inert: the carry
// runs from the latest, so nothing would ever pick the row up.
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

	// Marking the same lines back joins all three into one, because the pieces
	// either side of the gap now touch it.
	f.mark(s, g, "code.txt", store.SideHead, review.Range{Start: 7, End: 16})
	assertRanges(t, f.storedRanges(g), []string{"code.txt head 5:18"})
}

// A whole-file mark names the file rather than any line in it, so a line range
// neither clips it nor is clipped by it. Unmarking a file's lines and finding it
// no longer marked as a whole would be the mark disappearing for a reason nobody
// asked for.
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

// A file with no hunks is marked as a whole, and a whole-file mark is not lines,
// so it comes through on the file's content rather than on any arithmetic.
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
