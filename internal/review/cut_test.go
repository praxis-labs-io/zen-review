package review_test

import (
	"sort"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func (f *fixture) storedCuts(g review.Generation) []string {
	f.t.Helper()

	var out []string
	for path, row := range f.genFiles(g) {
		if row.Cut {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func TestARefreshRecordsWhatItTookOffAFile(t *testing.T) {
	tests := []struct {
		name string
		edit func(f *fixture, s *review.Session, g review.Generation)
		want []string
	}{
		{
			"a line inserted above moves the mark and takes nothing",
			func(f *fixture, _ *review.Session, _ review.Generation) {
				f.Write("code.txt", "inserted\n"+numbered(1, 20))
			},
			nil,
		},
		{
			"a line inserted inside splits the mark and takes nothing",
			func(f *fixture, _ *review.Session, _ review.Generation) {
				f.Write("code.txt", numbered(1, 7)+"inserted\n"+numbered(8, 20))
			},
			nil,
		},
		{
			"a reviewed line deleted takes that line",
			func(f *fixture, _ *review.Session, _ review.Generation) {
				f.Write("code.txt", numbered(1, 6)+numbered(8, 20))
			},
			[]string{"code.txt"},
		},
		{
			"a region rewritten wholesale takes all of it",
			func(f *fixture, _ *review.Session, _ review.Generation) {
				f.Write("code.txt", numbered(1, 4)+"alpha\nbeta\ngamma\ndelta\nepsilon\n"+numbered(10, 20))
			},
			[]string{"code.txt"},
		},
		{
			"an edit elsewhere in the file takes nothing",
			func(f *fixture, _ *review.Session, _ review.Generation) {
				f.Write("code.txt", numbered(1, 20)+"appended\n")
			},
			nil,
		},
		{
			"a rename takes nothing",
			func(f *fixture, _ *review.Session, _ review.Generation) {
				f.Git("mv", "code.txt", "moved.txt")
			},
			nil,
		},
		{
			"unmarking lowers the coverage and records nothing",
			func(f *fixture, s *review.Session, g review.Generation) {
				if err := s.Unmark(f.t.Context(), g, "code.txt", store.SideHead,
					[]review.Range{{Start: 7, End: 9}}); err != nil {
					f.t.Fatalf("unmarking: %v", err)
				}
				f.Write("code.txt", numbered(1, 20)+"appended\n")
			},
			nil,
		},
		{
			"marking part of a hunk on purpose records nothing",
			func(f *fixture, s *review.Session, g review.Generation) {
				f.mark(s, g, "code.txt", store.SideHead, review.Range{Start: 15, End: 15})
				f.Write("code.txt", numbered(1, 20)+"appended\n")
			},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s, first := marked(t)
			tt.edit(f, s, first)

			next := f.refresh(s)
			if next.ID == first.ID {
				t.Fatal("the edit built no new generation, so nothing was carried")
			}
			assertRanges(t, f.storedCuts(next), tt.want)
		})
	}
}

func TestAWholeFileMarkTheBytesMovedUnderIsRecorded(t *testing.T) {
	f := branched(t)
	f.Write("blob.bin", "\x00\x01binary\n")
	f.Commit("add the blob")

	s := f.mustOpen("")
	first := f.refresh(s)
	f.mark(s, first, "blob.bin", store.SideHead, review.Range{})
	assertRanges(t, f.storedRanges(first), []string{"blob.bin head 0:0"})

	f.Write("blob.bin", "\x00\x02different\n")
	next := f.refresh(s)

	assertRanges(t, f.storedRanges(next), nil)
	assertRanges(t, f.storedCuts(next), []string{"blob.bin"})
}

func TestAWholeFileMarkDroppedByABaseChangeIsNotRecorded(t *testing.T) {
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
	if err := near.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	far := f.mustOpen("origin/main")
	next := f.refresh(far)
	if next.ID == first.ID {
		t.Fatal("the base change built no new generation")
	}
	assertRanges(t, f.storedRanges(next), nil)
	assertRanges(t, f.storedCuts(next), nil)
}

func TestUpstreamRewritingTheLinesBehindABaseSideMarkIsRecorded(t *testing.T) {
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
	f.Write("code.txt", numbered(1, 9)+"alpha\nbeta\ngamma\n"+numbered(13, 20))
	f.Commit("upstream rewrites the middle")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("merge", "-q", "-X", "ours", "-m", "merge upstream", "main")

	next := f.refresh(s)
	if next.ID == first.ID {
		t.Fatal("the base move built no new generation")
	}

	assertRanges(t, f.storedRanges(next), nil)
	assertRanges(t, f.storedCuts(next), []string{"code.txt"})
}

func TestABaseSideCutOnARenamedFileIsRecordedUnderItsHeadName(t *testing.T) {
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
	f.Write("old.txt", numbered(1, 9)+"alpha\nbeta\ngamma\n"+numbered(13, 20))
	f.Commit("upstream rewrites the middle")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("merge", "-q", "-X", "ours", "-m", "merge upstream", "main")

	next := f.refresh(s)
	if next.ID == first.ID {
		t.Fatal("the base move built no new generation")
	}
	assertRanges(t, f.storedRanges(next), nil)
	assertRanges(t, f.storedCuts(next), []string{"new.txt"})
}

func TestAnUnmarkAnswersTheRecord(t *testing.T) {
	f, s, _ := marked(t)

	f.Write("code.txt", numbered(1, 4)+"alpha\nbeta\ngamma\ndelta\nepsilon\n"+numbered(10, 20))
	cut := f.refresh(s)
	assertRanges(t, f.storedCuts(cut), []string{"code.txt"})

	f.mark(s, cut, "code.txt", store.SideHead, review.Range{Start: 1, End: 20})
	if err := s.Unmark(t.Context(), cut, "code.txt", store.SideHead,
		[]review.Range{{Start: 3, End: 3}}); err != nil {
		t.Fatalf("unmarking: %v", err)
	}

	assertRanges(t, f.storedCuts(cut), nil)

	c, err := s.Changeset(t.Context(), cut)
	if err != nil {
		t.Fatalf("reading the changeset: %v", err)
	}
	for _, file := range c.Files {
		if file.Changed {
			t.Errorf("%s reads as changed after review, and the reader took the line back themselves",
				file.Diff.Path)
		}
	}
}

func TestTheRecordStandsUntilTheFileIsReadAgain(t *testing.T) {
	f, s, _ := marked(t)

	f.Write("code.txt", numbered(1, 4)+"alpha\nbeta\ngamma\ndelta\nepsilon\n"+numbered(10, 20))
	cut := f.refresh(s)
	assertRanges(t, f.storedCuts(cut), []string{"code.txt"})

	f.Write("a.txt", "an edit somewhere else\n")
	stands := f.refresh(s)
	if stands.ID == cut.ID {
		t.Fatal("the edit built no new generation")
	}
	assertRanges(t, f.storedCuts(stands), []string{"code.txt"})

	f.mark(s, stands, "code.txt", store.SideHead, review.Range{Start: 1, End: 20})
	f.Write("a.txt", "another edit somewhere else\n")
	cleared := f.refresh(s)

	assertRanges(t, f.storedCuts(cleared), nil)
}

func TestTheRecordFollowsARename(t *testing.T) {
	f, s, first := marked(t)

	f.Write("code.txt", numbered(1, 4)+"alpha\nbeta\ngamma\ndelta\nepsilon\n"+numbered(10, 20))
	cut := f.refresh(s)
	if cut.ID == first.ID {
		t.Fatal("the rewrite built no new generation")
	}
	assertRanges(t, f.storedCuts(cut), []string{"code.txt"})

	f.Git("mv", "code.txt", "moved.txt")
	renamed := f.refresh(s)

	assertRanges(t, f.storedCuts(renamed), []string{"moved.txt"})
}

func TestTheChangesetReadsBackWhatTheRefreshRecorded(t *testing.T) {
	f, s, _ := marked(t)

	f.Write("code.txt", numbered(1, 4)+"alpha\nbeta\ngamma\ndelta\nepsilon\n"+numbered(10, 20))
	g := f.refresh(s)

	c, err := s.Changeset(t.Context(), g)
	if err != nil {
		t.Fatalf("reading the changeset: %v", err)
	}

	for _, file := range c.Files {
		want := file.Diff.Path == "code.txt"
		if file.Changed != want {
			t.Errorf("%s changed = %v, want %v", file.Diff.Path, file.Changed, want)
		}
	}
}
