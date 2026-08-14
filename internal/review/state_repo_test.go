package review_test

import (
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// read marks a hunk by the name the changeset lists it under, which is what a
// reader pressing r does and what review --hunk does.
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

// file is one file of the derived changeset.
func (f *fixture) file(s *review.Session, g review.Generation, path string) review.File {
	f.t.Helper()

	file, found := f.changeset(s, g).File(path)
	if !found {
		f.t.Fatalf("the changeset of generation %d holds no %s", g.Seq, path)
	}
	return file
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

// One call, both sides. A hunk anchored on its additions alone would swallow a
// deletion arriving later, because the lines it removes are not lines it has.
func TestMarkingAHunkTakesEverySideItTouches(t *testing.T) {
	f, _, g := changedLine(t)

	assertRanges(t, f.storedRanges(g), []string{
		"code.txt base 10:10",
		"code.txt head 10:10",
	})
}

// Marking a file is marking everything in it, and a file gathers its hunks from
// both sides the same way one hunk does.
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

// A binary file has no lines to name, so the mark is on the file itself. That is
// the case a loop over hunks alone leaves permanently unreviewed.
func TestMarkingAFileWithNoHunksMarksTheWholeOfIt(t *testing.T) {
	f := branched(t)
	f.Write("blob.bin", "\x00\x01binary\n")

	s := f.mustOpen("")
	g := f.refresh(s)
	if err := s.MarkFile(t.Context(), g, f.file(s, g, "blob.bin")); err != nil {
		t.Fatalf("marking the file: %v", err)
	}

	// 0:0 is the file itself rather than any line in it, which is what a file with
	// no lines to name has to be marked as.
	assertRanges(t, f.storedRanges(g), []string{"blob.bin head 0:0"})
	assertFile(t, f.file(s, g, "blob.bin"), review.Reviewed)
}

// A deleted file has no head blob, so a whole-file mark on the head would be
// keyed to bytes that are not there. On the base it is keyed to the bytes the
// deletion removes, which is what was read.
func TestAWholeFileMarkOnADeletionSitsOnTheBase(t *testing.T) {
	f, s, g := deletedBlob(t, "\x00\x02irrelevant here\n")

	assertRanges(t, f.storedRanges(g), []string{"logo.png base 0:0"})
	assertFile(t, f.file(s, g, "logo.png"), review.Reviewed)
}

// The mark that a head-side anchor could never lose. Upstream rewrites the very
// bytes the deletion removes, so what was read is gone and the mark goes with
// it.
func TestAWholeFileMarkOnADeletionGoesWhenTheBytesItRemovedMove(t *testing.T) {
	f, near, first := deletedBlob(t, "\x00\x02completely different\n")

	far, next := onUpstream(t, f, near, first)

	assertRanges(t, f.storedRanges(next), nil)
	assertFile(t, f.file(far, next, "logo.png"), review.Unreviewed)
}

// A whole-file mark is a claim about an entry with nothing to read, so it cannot
// outlive the entry gaining lines. Upstream replacing the binary with text
// leaves the file just as deleted and gives it a deletion-only hunk nobody has
// looked at.
func TestAWholeFileMarkOnADeletionGoesWhenTheFileGainsHunks(t *testing.T) {
	f, near, first := deletedBlob(t, numbered(1, 5))

	far, next := onUpstream(t, f, near, first)

	assertRanges(t, f.storedRanges(next), nil)
	assertFile(t, f.file(far, next, "logo.png"), review.Unreviewed, "  base 1:5 unreviewed")
}

// deletedBlob is a binary file on the base that the branch deletes, with the
// whole of it marked. It has no lines to name on either side and no head blob at
// all, which is the one shape a whole-file mark cannot sit on the head for.
//
// upstream is what the file holds on origin/main, which the base moves back onto
// once the mark is made. The branch turns it into a blob first, so the near base
// has a file with no lines to name and the mark has nowhere but the base to sit.
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

// onUpstream reopens the session against origin/main, which is the base moving
// off the blob and onto what upstream holds. A merge cannot stand in for it
// here: the branch deleted the file and upstream has its own version, which git
// stops on rather than resolves.
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

// Unmarking a file settles the change the refresh recorded against it, the same
// way unmarking lines does. A reader taking the whole file back by hand has made
// the coverage their own and there is no refresh due to write the record away.
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

// Withdrawing a mark says nothing happened to the code, so the file goes back to
// where it was before anybody read it.
func TestUnmarkingLeavesTheFileUnreviewed(t *testing.T) {
	f, s, g := added(t)

	if err := s.Unmark(t.Context(), g, "code.txt", store.SideHead, []review.Range{{Start: 1, End: 20}}); err != nil {
		t.Fatalf("unmarking: %v", err)
	}

	assertFile(t, f.file(s, g, "code.txt"), review.Unreviewed, "  head 1:20 unreviewed")
}
