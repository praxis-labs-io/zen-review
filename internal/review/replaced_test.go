package review_test

import (
	"slices"
	"testing"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

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

func TestAResponseCarriesTheLinesTheAgentRewrote(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "rewritten")

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

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

func TestACommentWhoseBytesHeldCarriesNoBlock(t *testing.T) {
	f, s, g, c := commented(t)
	f.address(s, c.ID, "it already reads that way")

	got, held := f.replaced(s, g, c.ID)
	if held {
		t.Errorf("block = %v, want none where the lines are as they were", got)
	}
}

func TestAnOpenCommentCarriesNoBlock(t *testing.T) {
	f, s, _, c := commented(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	if got, held := f.replaced(s, g, c.ID); held {
		t.Errorf("block = %v, want none on a comment nobody has answered", got)
	}
}

func TestABareAddressCarriesTheBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "")

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

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

func TestABlockFollowsARename(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "renamed and rewritten")

	f.Git("rm", "-q", "code.txt")
	f.Write("moved.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

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

func TestALineTheAgentOnlyShiftedCarriesNoBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "wrapped it in a guard")

	f.Write("code.txt", numbered(1, 9)+"if ok {\n"+numbered(10, 20))
	g := f.refresh(s)

	if got, held := f.replaced(s, g, c.ID); held {
		t.Errorf("block = %v, want none: line 10 is still there, one row down", got)
	}
}

func TestALineRewrittenUnderAnInsertCarriesItsBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "wrapped it and turned it round")

	f.Write("code.txt", numbered(1, 9)+"if ok {\nline 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

	got, held := f.replaced(s, g, c.ID)
	assertBlock(t, got, held, []string{"line 10"})
}

func TestAnAnchorBlobThatHasGoneLosesOnlyItsOwnBlock(t *testing.T) {
	f, s, _, c := commented(t)
	f.address(s, c.ID, "turned it round")

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	g := f.refresh(s)

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
