package review_test

import (
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// read marks a hunk through the anchor the changeset names it under, which is
// what a reader pressing r does.
func (f *fixture) read(s *review.Session, g review.Generation, path string, line int) {
	f.t.Helper()

	h, found := f.changeset(s, g).Hunk(path, store.SideHead, line)
	if !found {
		f.t.Fatalf("no hunk of %s starts at line %d", path, line)
	}
	f.mark(s, g, path, h.Side, h.Range)
}

func (f *fixture) changeset(s *review.Session, g review.Generation) review.Changeset {
	f.t.Helper()

	c, err := s.Changeset(f.t.Context(), g)
	if err != nil {
		f.t.Fatalf("deriving the changeset of generation %d: %v", g.Seq, err)
	}
	return c
}

// stateOf is what one file of the changeset reads as, and how many of its hunks
// are read.
func (f *fixture) stateOf(s *review.Session, g review.Generation, path string) (review.State, string) {
	f.t.Helper()

	for _, file := range f.changeset(s, g).Files {
		if file.Diff.Path == path {
			return file.State, describe(review.Changeset{Files: []review.File{file}})[1]
		}
	}
	f.t.Fatalf("the changeset of generation %d holds no %s", g.Seq, path)
	return "", ""
}

// twenty is a session whose one file is twenty numbered lines the branch added,
// with the whole of it read.
func twenty(t *testing.T) (*fixture, *review.Session, review.Generation) {
	t.Helper()

	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	f.read(s, g, "code.txt", 1)
	return f, s, g
}

func TestAReadHunkReadsReviewed(t *testing.T) {
	f, s, g := twenty(t)

	state, hunk := f.stateOf(s, g, "code.txt")
	if state != review.Reviewed {
		t.Errorf("code.txt = %s, want %s", state, review.Reviewed)
	}
	if hunk != "  head 1:20 reviewed" {
		t.Errorf("hunk = %q, want %q", hunk, "  head 1:20 reviewed")
	}

	// Nothing moved, so the refresh returns the generation that is already there
	// and what was read is still read.
	again := f.refresh(s)
	if again.Seq != g.Seq {
		t.Fatalf("generation %d, want the one already built, %d", again.Seq, g.Seq)
	}
	if state, _ := f.stateOf(s, again, "code.txt"); state != review.Reviewed {
		t.Errorf("code.txt = %s after a refresh that changed nothing, want %s", state, review.Reviewed)
	}
}

// An agent editing one line of twenty leaves nineteen read lines inside the
// hunk, and that is what changed after review looks like from the inside.
func TestAHunkEditedAfterReadingReadsChanged(t *testing.T) {
	f, s, _ := twenty(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	next := f.refresh(s)

	state, hunk := f.stateOf(s, next, "code.txt")
	if state != review.Changed {
		t.Errorf("code.txt = %s, want %s", state, review.Changed)
	}
	if hunk != "  head 1:20 changed" {
		t.Errorf("hunk = %q, want %q", hunk, "  head 1:20 changed")
	}
}

// The case the look-back exists for. Every line moved, so the range went whole
// and the hunk reads exactly like one nobody has opened. Only the previous
// generation's coverage says otherwise.
func TestAHunkRewrittenWholeLeavesTheFileChanged(t *testing.T) {
	f, s, _ := twenty(t)

	f.Write("code.txt", numbered(101, 120))
	next := f.refresh(s)

	state, hunk := f.stateOf(s, next, "code.txt")
	if state != review.Changed {
		t.Errorf("code.txt = %s, want %s", state, review.Changed)
	}
	if hunk != "  head 1:20 unreviewed" {
		t.Errorf("hunk = %q, want %q", hunk, "  head 1:20 unreviewed")
	}
}

// The limit, asserted so it cannot drift into being fixed by accident. The drop
// is found by comparing one generation with the one before, so a generation
// later there is no drop left to find.
func TestTheChangedSignalIsOneGenerationDeep(t *testing.T) {
	f, s, _ := twenty(t)

	f.Write("code.txt", numbered(101, 120))
	f.refresh(s)

	f.Write("other.txt", "something else\n")
	third := f.refresh(s)

	if state, _ := f.stateOf(s, third, "code.txt"); state != review.Unreviewed {
		t.Errorf("code.txt = %s a generation after the drop, want %s", state, review.Unreviewed)
	}
}
