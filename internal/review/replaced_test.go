package review_test

import (
	"slices"
	"testing"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// replaced is the block one comment carries, and whether it carries one at all.
func (f *fixture) replaced(s *review.Session, g review.Generation, id string) ([]string, bool) {
	f.t.Helper()

	cs, err := s.Comments(f.t.Context())
	if err != nil {
		f.t.Fatalf("reading the comments: %v", err)
	}

	blocks, err := s.Replaced(f.t.Context(), g, cs)
	if err != nil {
		f.t.Fatalf("reading what the responses replaced: %v", err)
	}
	got, held := blocks[id]
	return got, held
}

// address is the agent answering, which is what a block hangs off.
func (f *fixture) address(s *review.Session, id, response string) {
	f.t.Helper()

	if _, err := s.AddressComment(f.t.Context(), id, response); err != nil {
		f.t.Fatalf("addressing %s: %v", id, err)
	}
}

func assertBlock(t *testing.T, got []string, held bool, want []string) {
	t.Helper()

	if !held {
		t.Fatalf("no block came back, want %v", want)
	}
	if !slices.Equal(got, want) {
		t.Errorf("block = %v, want %v", got, want)
	}
}

// The whole point. The agent rewrites the lines and the block is what they said
// before, sliced out of the blob the comment was written against.
func TestAResponseCarriesTheLinesTheAgentRewrote(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "rewritten")

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

// A range comment carries every line it covered, and only those.
func TestABlockIsTheLinesTheCommentCovered(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	c := f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 5, End: 7},
		Body:  "these three say one thing three times",
	})
	f.address(s, c.ID, "cut two of them")

	f.Write("code.txt", numbered(1, 4)+"line 5 and 6 and 7\n"+numbered(8, 20))
	g = f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 5", "line 6", "line 7"})
}

// Nothing moved, so there is nothing to confirm and no block. On most cards
// this is the answer.
func TestACommentWhoseBytesHeldCarriesNoBlock(t *testing.T) {
	f, s, g, c := commented(t)
	f.address(s, c.ID, "it already reads that way")

	got, held := f.replaced(s, g, c.ID)
	if held {
		t.Errorf("block = %v, want none where the lines are as they were", got)
	}
}

// Before the agent acts there is nothing it replaced, so an open comment has no
// block however far the code moved under it.
func TestAnOpenCommentCarriesNoBlock(t *testing.T) {
	f, s, _, c := commented(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	if got, held := f.replaced(s, g, c.ID); held {
		t.Errorf("block = %v, want none on a comment nobody has answered", got)
	}
}

// A bare address is the verb it always was, and the code is the whole of what it
// has to say. The block is drawn on the act, not on the words.
func TestABareAddressCarriesTheBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "")

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

// A file comment names the file rather than any region of it, so the whole of
// the old file is not a block.
func TestAFileCommentCarriesNoBlock(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	c := f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeFile,
		Body:  "does this belong here at all",
	})
	f.address(s, c.ID, "moved it")

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g = f.refresh(s)

	if got, held := f.replaced(s, g, c.ID); held {
		t.Errorf("block = %v, want none on a comment that names the file", got)
	}
}

// The anchor blob is immune to a rename, so the block survives the file moving
// and the lines changing in the same generation.
func TestABlockFollowsARename(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "renamed and rewritten")

	f.Git("rm", "-q", "code.txt")
	f.Write("moved.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

// A base-side comment takes its block off the base blob rather than the head
// one. Upstream rewriting and the branch replaying is what moves those lines.
func TestABaseSideCommentCarriesItsOwnSide(t *testing.T) {
	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 14)+numbered(18, 20))
	f.Commit("drop three lines")

	s := f.mustOpen("")
	g := f.refresh(s)
	c := f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideBase,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 15, End: 17},
		Body:  "why did these three go",
	})
	f.address(s, c.ID, "two of them were dead, one moved")

	// Upstream rewrites the three lines the comment is on, and the branch replays
	// its own deletion on top. The base no longer holds what was asked about.
	f.Git("checkout", "-q", "main")
	f.Write("code.txt", numbered(1, 14)+"fifteen\nsixteen\nseventeen\n"+numbered(18, 20))
	f.Commit("upstream rewrite")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("reset", "-q", "--hard", "main")
	f.Write("code.txt", numbered(1, 14)+numbered(18, 20))
	f.Commit("drop the three again")

	g = f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 15", "line 16", "line 17"})
}

// A base move that only shifts the anchored lines is not a rewrite of them. The
// old read compared the two sides at the same numbers and called this a change.
func TestLinesTheBaseOnlyShiftedCarryNoBlock(t *testing.T) {
	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 14)+numbered(18, 20))
	f.Commit("drop three lines")

	s := f.mustOpen("")
	g := f.refresh(s)
	c := f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideBase,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 15, End: 17},
		Body:  "why did these three go",
	})
	f.address(s, c.ID, "two of them were dead, one moved")

	// Upstream folds three lines near the top into one, clear of what the comment
	// covers, so every base line below moves up two and none of them changes.
	f.Git("checkout", "-q", "main")
	f.Write("code.txt", "line 1\nlines 2 to 4, folded\n"+numbered(5, 20))
	f.Commit("upstream fold")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("rebase", "-q", "main")

	g = f.refresh(s)

	if got, held := f.replaced(s, g, c.ID); held {
		t.Errorf("block = %v, want none where the lines only moved", got)
	}
}

// The anchor moves with the code and the creation range does not, so a comment
// that travelled still slices its blob by the lines it was written on.
func TestABlockIsSlicedByWhereTheCommentStartedNotWhereItIs(t *testing.T) {
	f, s, _, c := commented(t)

	f.Write("code.txt", numbered(101, 105)+numbered(1, 20))
	f.refresh(s)

	if moved := f.storedComment(c.ID); moved.Start != 15 {
		t.Fatalf("the comment is on line %d, want it carried down to 15", moved.Start)
	}
	f.address(s, c.ID, "rewritten")

	f.Write("code.txt", numbered(101, 105)+numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

// An agent inserts a line above the anchor and the line commented on is still
// there, word for word, one row down. Nothing replaced it, so nothing is said.
func TestALineTheAgentOnlyShiftedCarriesNoBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "wrapped it in a guard")

	f.Write("code.txt", numbered(1, 9)+"if ok {\n"+numbered(10, 20))
	g := f.refresh(s)

	if got, held := f.replaced(s, g, c.ID); held {
		t.Errorf("block = %v, want none: line 10 is still there, one row down", got)
	}
}

// The other half of the same press: the guard goes in and the line inside it is
// rewritten, so the block is what it said before.
func TestALineRewrittenUnderAnInsertCarriesItsBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "wrapped it and turned it round")

	f.Write("code.txt", numbered(1, 9)+"if ok {\nline 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

// A blob the repository has lost is one comment without its evidence, not a
// failed call. The diffs used to run first, so the bad object took the listing.
func TestAnAnchorBlobThatHasGoneLosesOnlyItsOwnBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "turned it round")

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	// A comment anchored to an object nothing can resolve, which is what a
	// generation somebody pruned leaves behind.
	gone := store.Comment{
		ID:                  "aaaaaaaaaaaa",
		SessionID:           s.ID(),
		GenerationID:        g.ID,
		CreatedGenerationID: g.ID,
		Path:                "code.txt",
		Side:                store.SideHead,
		LineRange:           store.LineRange{Start: 1, End: 1},
		CreatedRange:        store.LineRange{Start: 1, End: 1},
		Scope:               store.ScopeLine,
		Body:                "written against bytes that have gone",
		State:               store.CommentAddressed,
		Response:            "done",
		AnchorBlob:          "0123456789012345678901234567890123456789",
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}
	if err := f.db().AddComment(f.t.Context(), gone); err != nil {
		t.Fatalf("writing the comment with no bytes behind it: %v", err)
	}

	if got, held := f.replaced(s, g, gone.ID); held {
		t.Errorf("block = %v, want none where the bytes cannot be read", got)
	}
	if _, held := f.replaced(s, g, c.ID); !held {
		t.Error("the other comment lost its block to the one that could not be read")
	}
}
