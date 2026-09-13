package review_test

import (
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func (f *fixture) read(s *review.Session, g review.Generation, path string, side store.Side, line int) {
	f.t.Helper()

	h, found := f.changeset(s, g).Hunk(path, side, line)
	if !found {
		f.t.Fatalf("no hunk of %s is named %s %d", path, side, line)
	}
	if err := s.MarkHunk(f.t.Context(), g, path, h); err != nil {
		f.t.Fatalf("marking the hunk of %s named %s %d: %v", path, side, line, err)
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

func (f *fixture) file(s *review.Session, g review.Generation, path string) review.File {
	f.t.Helper()

	file, found := f.changeset(s, g).File(path)
	if !found {
		f.t.Fatalf("the changeset of generation %d holds no %s", g.Seq, path)
	}
	return file
}

func assertFile(t *testing.T, f review.File, state review.State, hunks ...string) {
	t.Helper()

	if f.State != state {
		t.Errorf("%s = %s, want %s", f.Diff.Path, f.State, state)
	}
	assertRanges(t, hunkLines(f), hunks)
}

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

	again := f.refresh(s)
	if again.Seq != g.Seq {
		t.Fatalf("generation %d, want the one already built, %d", again.Seq, g.Seq)
	}
	assertFile(t, f.file(s, again, "code.txt"), review.Reviewed, "  head 1:20 reviewed")
}

func TestAHunkEditedAfterReadingReadsPartial(t *testing.T) {
	f, s, _ := added(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	next := f.refresh(s)

	assertFile(t, f.file(s, next, "code.txt"), review.Partial, "  head 1:20 partial")
}

func TestADeletionAddedToAReadHunkShowsUp(t *testing.T) {
	f, s, g := changedLine(t)

	assertFile(t, f.file(s, g, "code.txt"), review.Reviewed, "  head 10:10 base 10:10 reviewed")

	f.Write("code.txt", numbered(1, 9)+"line 10 changed\nline 11\n"+numbered(13, 20))
	next := f.refresh(s)

	assertFile(t, f.file(s, next, "code.txt"), review.Partial, "  head 10:10 base 10:12 partial")
}

func TestAHunkRewrittenWholeReadsUnreviewed(t *testing.T) {
	f, s, _ := added(t)

	f.Write("code.txt", numbered(101, 120))
	next := f.refresh(s)

	assertFile(t, f.file(s, next, "code.txt"), review.Unreviewed, "  head 1:20 unreviewed")
}

func TestMarkingAHunkTakesEverySideItTouches(t *testing.T) {
	f, _, g := changedLine(t)

	assertRanges(t, f.storedRanges(g), []string{
		"code.txt base 10:10",
		"code.txt head 10:10",
	})
}

func TestMarkingAFileTakesEveryHunkInIt(t *testing.T) {
	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", "line 1 changed\n"+numbered(2, 19)+"line 20 changed\n")

	s := f.mustOpen("")
	g := f.refresh(s)
	if err := s.MarkFile(t.Context(), g, f.file(s, g, "code.txt")); err != nil {
		t.Fatalf("marking the file: %v", err)
	}

	assertFile(t, f.file(s, g, "code.txt"), review.Reviewed,
		"  head 1:1 base 1:1 reviewed",
		"  head 20:20 base 20:20 reviewed",
	)
}

func TestMarkingAFileWithNoHunksMarksTheWholeOfIt(t *testing.T) {
	f := branched(t)
	f.Write("blob.bin", "\x00\x01binary\n")

	s := f.mustOpen("")
	g := f.refresh(s)
	if err := s.MarkFile(t.Context(), g, f.file(s, g, "blob.bin")); err != nil {
		t.Fatalf("marking the file: %v", err)
	}

	assertRanges(t, f.storedRanges(g), []string{"blob.bin head 0:0"})
	assertFile(t, f.file(s, g, "blob.bin"), review.Reviewed)
}

func TestAWholeFileMarkOnADeletionSitsOnTheBase(t *testing.T) {
	f, s, g := deletedBlob(t, "\x00\x02irrelevant here\n")

	assertRanges(t, f.storedRanges(g), []string{"logo.png base 0:0"})
	assertFile(t, f.file(s, g, "logo.png"), review.Reviewed)
}

func TestAWholeFileMarkOnADeletionGoesWhenTheBytesItRemovedMove(t *testing.T) {
	f, near, first := deletedBlob(t, "\x00\x02completely different\n")

	far, next := onUpstream(t, f, near, first)

	assertRanges(t, f.storedRanges(next), nil)
	assertFile(t, f.file(far, next, "logo.png"), review.Unreviewed)
}

func TestAWholeFileMarkOnADeletionGoesWhenTheFileGainsHunks(t *testing.T) {
	f, near, first := deletedBlob(t, numbered(1, 5))

	far, next := onUpstream(t, f, near, first)

	assertRanges(t, f.storedRanges(next), nil)
	assertFile(t, f.file(far, next, "logo.png"), review.Unreviewed, "  base 1:5 unreviewed")
}

func deletedBlob(t *testing.T, upstream string) (*fixture, *review.Session, review.Generation) {
	t.Helper()

	f := newFixture(t)
	f.Write("logo.png", upstream)
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("logo.png", "\x00\x01binary\n")
	f.Commit("make the logo a blob")
	f.Git("rm", "-q", "logo.png")
	f.Commit("drop the logo")

	s := f.mustOpen("feature~1")
	g := f.refresh(s)
	if err := s.MarkFile(t.Context(), g, f.file(s, g, "logo.png")); err != nil {
		t.Fatalf("marking the file: %v", err)
	}
	return f, s, g
}

func onUpstream(t *testing.T, f *fixture, near *review.Session, first review.Generation) (*review.Session, review.Generation) {
	t.Helper()

	if err := near.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	far := f.mustOpen("origin/main")
	next := f.refresh(far)
	if next.ID == first.ID {
		t.Fatal("the base move built no new generation")
	}
	return far, next
}

func TestUnmarkingAFileAnswersTheRecordedChange(t *testing.T) {
	f, s, _ := marked(t)

	f.Write("code.txt", numbered(1, 4)+"alpha\nbeta\ngamma\ndelta\nepsilon\n"+numbered(10, 20))
	cut := f.refresh(s)
	assertRanges(t, f.storedCuts(cut), []string{"code.txt"})

	if err := s.UnmarkFile(t.Context(), cut, f.file(s, cut, "code.txt")); err != nil {
		t.Fatalf("unmarking the file: %v", err)
	}

	assertRanges(t, f.storedCuts(cut), nil)
	assertFile(t, f.file(s, cut, "code.txt"), review.Unreviewed, "  head 1:20 unreviewed")
}

func TestUnmarkingLeavesTheFileUnreviewed(t *testing.T) {
	f, s, g := added(t)

	if err := s.Unmark(t.Context(), g, "code.txt", store.SideHead, []review.Range{{Start: 1, End: 20}}); err != nil {
		t.Fatalf("unmarking: %v", err)
	}

	assertFile(t, f.file(s, g, "code.txt"), review.Unreviewed, "  head 1:20 unreviewed")
}
