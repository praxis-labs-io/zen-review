package review_test

import (
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// read marks every anchor of a hunk, which is what a reader pressing r does. A
// hunk that both adds and removes takes two marks, because the lines it removes
// are not lines it has.
func (f *fixture) read(s *review.Session, g review.Generation, path string, side store.Side, line int) {
	f.t.Helper()

	h, found := f.changeset(s, g).Hunk(path, side, line)
	if !found {
		f.t.Fatalf("no hunk of %s is named %s %d", path, side, line)
	}
	for _, a := range h.Anchors {
		f.mark(s, g, path, a.Side, a.Range)
	}
}

func (f *fixture) changeset(s *review.Session, g review.Generation) review.Changeset {
	f.t.Helper()

	c, err := s.Changeset(f.t.Context(), g)
	if err != nil {
		f.t.Fatalf("deriving the changeset of generation %d: %v", g.Seq, err)
	}
	return c
}

// file is one file of the derived changeset.
func (f *fixture) file(s *review.Session, g review.Generation, path string) review.File {
	f.t.Helper()

	for _, file := range f.changeset(s, g).Files {
		if file.Diff.Path == path {
			return file
		}
	}
	f.t.Fatalf("the changeset of generation %d holds no %s", g.Seq, path)
	return review.File{}
}

// assertFile checks a file's state and the line per hunk under it.
func assertFile(t *testing.T, f review.File, state review.State, hunks ...string) {
	t.Helper()

	if f.State != state {
		t.Errorf("%s = %s, want %s", f.Diff.Path, f.State, state)
	}
	assertRanges(t, hunkLines(f), hunks)
}

// added is a session whose one file is twenty numbered lines the branch added,
// with the whole of it read.
func added(t *testing.T) (*fixture, *review.Session, review.Generation) {
	t.Helper()

	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	f.read(s, g, "code.txt", store.SideHead, 1)
	return f, s, g
}

// changedLine is the shape a deletion can hide in: the file is on the base, the
// branch changed one line of it, and that hunk has been read on both sides.
func changedLine(t *testing.T) (*fixture, *review.Session, review.Generation) {
	t.Helper()

	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")

	f.Write("code.txt", numbered(1, 9)+"line 10 changed\n"+numbered(11, 20))
	f.Commit("change line 10")

	s := f.mustOpen("")
	g := f.refresh(s)
	f.read(s, g, "code.txt", store.SideHead, 10)
	return f, s, g
}

func TestAReadHunkReadsReviewed(t *testing.T) {
	f, s, g := added(t)

	assertFile(t, f.file(s, g, "code.txt"), review.Reviewed, "  head 1:20 reviewed")

	// Nothing moved, so the refresh returns the generation that is already there
	// and what was read is still read.
	again := f.refresh(s)
	if again.Seq != g.Seq {
		t.Fatalf("generation %d, want the one already built, %d", again.Seq, g.Seq)
	}
	assertFile(t, f.file(s, again, "code.txt"), review.Reviewed, "  head 1:20 reviewed")
}

// An agent editing one line of twenty leaves nineteen read lines inside the
// hunk, and a hunk that is not wholly read is not read.
func TestAHunkEditedAfterReadingReadsPartial(t *testing.T) {
	f, s, _ := added(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	next := f.refresh(s)

	assertFile(t, f.file(s, next, "code.txt"), review.Partial, "  head 1:20 partial")
}

// The deletion a head-side anchor would swallow. Line 10 was read on both
// sides; the agent then deletes line 12, which git folds into the same hunk, so
// the hunk now removes two more lines nobody has looked at.
func TestADeletionAddedToAReadHunkShowsUp(t *testing.T) {
	f, s, g := changedLine(t)

	assertFile(t, f.file(s, g, "code.txt"), review.Reviewed, "  head 10:10 base 10:10 reviewed")

	f.Write("code.txt", numbered(1, 9)+"line 10 changed\nline 11\n"+numbered(13, 20))
	next := f.refresh(s)

	assertFile(t, f.file(s, next, "code.txt"), review.Partial, "  head 10:10 base 10:12 partial")
}

// A hunk rewritten end to end loses its range whole, so it reads exactly like a
// hunk nobody opened. Telling the two apart takes the refresh recording what the
// translation cut, which nothing does yet, and inferring it from the coverage
// alone cannot work: a withdrawn mark leaves the same thing behind.
func TestAHunkRewrittenWholeReadsUnreviewed(t *testing.T) {
	f, s, _ := added(t)

	f.Write("code.txt", numbered(101, 120))
	next := f.refresh(s)

	assertFile(t, f.file(s, next, "code.txt"), review.Unreviewed, "  head 1:20 unreviewed")
}

// Withdrawing a mark says nothing happened to the code, so the file goes back to
// where it was before anybody read it.
func TestUnmarkingLeavesTheFileUnreviewed(t *testing.T) {
	f, s, g := added(t)

	if err := s.Unmark(t.Context(), g, "code.txt", store.SideHead, []review.Range{{Start: 1, End: 20}}); err != nil {
		t.Fatalf("unmarking: %v", err)
	}

	assertFile(t, f.file(s, g, "code.txt"), review.Unreviewed, "  head 1:20 unreviewed")
}
