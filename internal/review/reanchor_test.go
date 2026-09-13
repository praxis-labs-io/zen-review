package review_test

import (
	"fmt"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func reanchored(t *testing.T, s *review.Session, n review.Note, from, to review.Generation) string {
	t.Helper()

	got, held, err := s.Reanchor(t.Context(), n, from, to)
	if err != nil {
		t.Fatalf("reanchoring: %v", err)
	}
	if !held {
		return "gone"
	}
	return fmt.Sprintf("%s %s %s %d:%d %q", got.Path, got.Side, got.Scope, got.Range.Start, got.Range.End, got.Body)
}

func TestAReanchoredNoteLandsWhereARefreshWouldCarryIt(t *testing.T) {
	line := review.NoteOnLines("code.txt", store.SideHead, review.Range{Start: 10, End: 10}, "kept")
	block := review.NoteOnLines("code.txt", store.SideHead, review.Range{Start: 5, End: 9}, "kept")
	file := review.Note{Path: "code.txt", Side: store.SideHead, Scope: store.ScopeFile, Body: "kept"}

	for _, tc := range []struct {
		name   string
		note   review.Note
		change func(f *fixture)
		want   string
	}{
		{
			name:   "a line inserted above",
			note:   line,
			change: func(f *fixture) { f.Write("code.txt", numbered(101, 105)+numbered(1, 20)) },
			want:   `code.txt head line 15:15 "kept"`,
		},
		{
			name:   "its line rewritten",
			note:   line,
			change: func(f *fixture) { f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20)) },
			want:   "gone",
		},
		{
			name:   "the top of its range rewritten",
			note:   block,
			change: func(f *fixture) { f.Write("code.txt", numbered(1, 3)+"four\nfive\nsix\n"+numbered(7, 20)) },
			want:   `code.txt head range 7:9 "kept"`,
		},
		{
			name:   "a rename",
			note:   line,
			change: func(f *fixture) { f.Git("mv", "code.txt", "moved.txt") },
			want:   `moved.txt head line 10:10 "kept"`,
		},
		{
			name:   "the file deleted",
			note:   line,
			change: func(f *fixture) { f.Git("rm", "-q", "-f", "code.txt") },
			want:   "gone",
		},
		{
			name:   "a file note on a file that changed",
			note:   file,
			change: func(f *fixture) { f.Write("code.txt", numbered(1, 21)) },
			want:   `code.txt head file 0:0 "kept"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := branched(t)
			f.Write("code.txt", numbered(1, 20))
			f.Commit("add code")

			s := f.mustOpen("")
			from := f.refresh(s)

			tc.change(f)
			to := f.refresh(s)
			if to.ID == from.ID {
				t.Fatal("the change built no new generation")
			}

			if got := reanchored(t, s, tc.note, from, to); got != tc.want {
				t.Errorf("reanchored = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestAReanchoredNoteOnTheSameGenerationIsUntouched(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)

	n := review.NoteOnLines("code.txt", store.SideHead, review.Range{Start: 4, End: 6}, "kept")
	if got, want := reanchored(t, s, n, g, g), `code.txt head range 4:6 "kept"`; got != want {
		t.Errorf("reanchored = %s, want %s", got, want)
	}
}

func TestAReanchoredBaseSideNoteFollowsTheBaseMoving(t *testing.T) {
	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 9)+numbered(13, 20))
	f.Commit("drop three lines")

	s := f.mustOpen("")
	from := f.refresh(s)

	f.Git("checkout", "-q", "main")
	f.Write("code.txt", "upstream\n"+numbered(1, 20))
	f.Commit("upstream")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("rebase", "-q", "main")

	to := f.refresh(s)
	if to.ID == from.ID {
		t.Fatal("the base move built no new generation")
	}

	n := review.NoteOnLines("code.txt", store.SideBase, review.Range{Start: 10, End: 12}, "kept")
	if got, want := reanchored(t, s, n, from, to), `code.txt base range 11:13 "kept"`; got != want {
		t.Errorf("reanchored = %s, want %s", got, want)
	}
}

func TestAReanchoredBaseSideNoteOnARenamedFileKeepsTheHeadName(t *testing.T) {
	f := newFixture(t)
	f.Write("old.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Git("mv", "old.txt", "new.txt")
	f.Write("new.txt", numbered(1, 9)+numbered(13, 20))
	f.Commit("move it and drop three lines")

	s := f.mustOpen("")
	from := f.refresh(s)

	f.Write("new.txt", "head only\n"+numbered(1, 9)+numbered(13, 20))
	to := f.refresh(s)
	if to.ID == from.ID {
		t.Fatal("the edit built no new generation")
	}

	n := review.NoteOnLines("new.txt", store.SideBase, review.Range{Start: 10, End: 12}, "kept")
	if got, want := reanchored(t, s, n, from, to), `new.txt base range 10:12 "kept"`; got != want {
		t.Errorf("reanchored = %s, want %s", got, want)
	}
}
