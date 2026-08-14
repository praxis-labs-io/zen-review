package review_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// note writes a comment and hands the row back.
func (f *fixture) note(s *review.Session, g review.Generation, n review.Note) store.Comment {
	f.t.Helper()

	c, err := s.AddComment(f.t.Context(), g, n)
	if err != nil {
		f.t.Fatalf("writing the comment on %s: %v", n.Path, err)
	}
	return c
}

// storedComments reads through a second handle on the database rather than
// through the session that wrote, so a method returning the right value while
// writing nothing cannot pass.
func (f *fixture) storedComments(s *review.Session) []string {
	f.t.Helper()

	cs, err := f.db().Comments(f.t.Context(), s.ID())
	if err != nil {
		f.t.Fatalf("reading the comments: %v", err)
	}

	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, fmt.Sprintf("%s %s %d:%d %s", c.Path, c.Side, c.Start, c.End, c.State))
	}
	return out
}

// storedComment is one comment, whole, off the same second handle.
func (f *fixture) storedComment(id string) store.Comment {
	f.t.Helper()

	c, found, err := f.db().Comment(f.t.Context(), id)
	if err != nil {
		f.t.Fatalf("reading the comment %s: %v", id, err)
	}
	if !found {
		f.t.Fatalf("the comment %s is not there", id)
	}
	return c
}

func assertComments(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("comments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("comment %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// commented is the state most of these start from: twenty numbered lines in the
// changeset, generation one built, and a line comment on line 10.
func commented(t *testing.T) (*fixture, *review.Session, review.Generation, store.Comment) {
	t.Helper()

	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	c := f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeLine,
		Range: review.Range{Start: 10, End: 10},
		Body:  "this reads backwards",
	})
	return f, s, g, c
}

// The whole point of the anchor. Five lines arrive above the comment and it is
// still on the line it was written about, under a number that has moved.
func TestACommentMovesWithTheLinesItIsOn(t *testing.T) {
	f, s, first, c := commented(t)

	f.Write("code.txt", numbered(101, 105)+numbered(1, 20))
	next := f.refresh(s)
	if next.ID == first.ID {
		t.Fatal("the edit built no new generation")
	}

	assertComments(t, f.storedComments(s), []string{"code.txt head 15:15 open"})

	got := f.storedComment(c.ID)
	if got.GenerationID != next.ID {
		t.Errorf("generationID = %d, want the generation it moved onto, %d", got.GenerationID, next.ID)
	}
	if got.CreatedGenerationID != first.ID {
		t.Errorf("createdGenerationID = %d, want the one it was written at, %d", got.CreatedGenerationID, first.ID)
	}

	// A line comment is on one line however far it travels. The scope and the
	// lines disagreeing is a comment that cannot be translated as what it says it
	// is, and the schema refuses the write outright.
	if got.Scope != store.ScopeLine || got.Start != got.End {
		t.Errorf("comment = %s %d:%d, want one line", got.Scope, got.Start, got.End)
	}
}

// The line it was about is gone, so the comment is not about anything any more.
// It keeps its text and where it was, because a rewrite never silently swallows
// something somebody said.
func TestACommentWhoseLineIsRewrittenOrphans(t *testing.T) {
	f, s, _, c := commented(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	f.refresh(s)

	assertComments(t, f.storedComments(s), []string{"code.txt head 10:10 orphaned"})

	got := f.storedComment(c.ID)
	if got.LastPath != "code.txt" || got.LastLine != 10 {
		t.Errorf("last known = %s:%d, want code.txt:10", got.LastPath, got.LastLine)
	}
	if got.Body != "this reads backwards" {
		t.Errorf("body = %q, want it kept", got.Body)
	}
}

// A comment on ten lines is about a region, and the agent rewriting a line in
// the middle of that region is usually the comment being acted on. This is where
// an anchor and a reviewed range part company: the range is cut into the pieces
// either side, and the comment stays whole.
func TestARangeCommentSurvivesALineRewrittenInsideIt(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 5, End: 9},
		Body:  "this whole block is the wrong shape",
	})

	f.Write("code.txt", numbered(1, 6)+"line 7 rewritten\n"+numbered(8, 20))
	f.refresh(s)

	assertComments(t, f.storedComments(s), []string{"code.txt head 5:9 open"})
}

// A rename moves the file without touching a line of it, so the comment follows
// it under the new name.
func TestAFileCommentFollowsARename(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeFile,
		Body:  "this file belongs under internal",
	})

	f.Git("mv", "code.txt", "moved.txt")
	f.refresh(s)

	assertComments(t, f.storedComments(s), []string{"moved.txt head 0:0 open"})
}

// A file comment names the file rather than any line in it, so it comes through
// while the content does and is lost when the bytes change. That is the rule a
// whole-file reviewed mark takes, and for the same reason.
func TestAFileCommentOrphansWhenTheFileChanges(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeFile,
		Body:  "this file belongs under internal",
	})

	f.Write("code.txt", numbered(1, 21))
	f.refresh(s)

	assertComments(t, f.storedComments(s), []string{"code.txt head 0:0 orphaned"})
}

// A deletion-only hunk has no head-side lines and anchors to the base blob, so
// the base moving is what moves the comment. It is the only reason the base diff
// runs at all.
func TestABaseSideCommentTranslatesWhenTheBaseMoves(t *testing.T) {
	f := newFixture(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("code.txt", numbered(1, 9)+numbered(13, 20))
	f.Commit("drop three lines")

	s := f.mustOpen("")
	first := f.refresh(s)
	f.note(s, first, review.Note{
		Path:  "code.txt",
		Side:  store.SideBase,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 10, End: 12},
		Body:  "these three were load bearing",
	})

	// Upstream inserts a line at the top of the same file and the branch replays
	// onto it, so every base-side line the comment names moved down one.
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
	assertComments(t, f.storedComments(s), []string{"code.txt base 11:13 open"})
}

// A rename gives a file two names, and its base blob sits under the old one. A
// base-side comment keyed by the head name would never be found in the base
// diff, and would sit unmoved on top of lines that shifted underneath it.
func TestABaseSideCommentOnARenamedFileIsKeyedByTheBaseName(t *testing.T) {
	f := newFixture(t)
	f.Write("old.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Git("mv", "old.txt", "new.txt")
	f.Write("new.txt", numbered(1, 9)+numbered(13, 20))
	f.Commit("move it and drop three lines")

	s := f.mustOpen("")
	g := f.refresh(s)

	// Written against the name the changeset lists the file by, which is the new
	// one.
	f.note(s, g, review.Note{
		Path:  "new.txt",
		Side:  store.SideBase,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 10, End: 12},
		Body:  "these three were load bearing",
	})
	assertComments(t, f.storedComments(s), []string{"old.txt base 10:12 open"})
}

// A comment stops moving the moment it is addressed or resolved. Carrying one
// forward would move a claim onto code nobody made it about, and reopen a
// question somebody closed.
func TestACommentThatHasStoppedMovingStaysWhereItWas(t *testing.T) {
	for _, tc := range []struct {
		state store.CommentState
		stop  func(*review.Session, string) error
	}{
		{
			state: store.CommentAddressed,
			stop: func(s *review.Session, id string) error {
				_, err := s.AddressComment(t.Context(), id)
				return err
			},
		},
		{
			state: store.CommentResolved,
			stop: func(s *review.Session, id string) error {
				_, err := s.ResolveComment(t.Context(), id)
				return err
			},
		},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			f, s, first, c := commented(t)

			if err := tc.stop(s, c.ID); err != nil {
				t.Fatalf("marking the comment %s: %v", tc.state, err)
			}

			f.Write("code.txt", numbered(101, 105)+numbered(1, 20))
			next := f.refresh(s)
			if next.ID == first.ID {
				t.Fatal("the edit built no new generation")
			}

			assertComments(t, f.storedComments(s),
				[]string{fmt.Sprintf("code.txt head 10:10 %s", tc.state)})

			if got := f.storedComment(c.ID); got.GenerationID != first.ID {
				t.Errorf("generationID = %d, want it left at %d", got.GenerationID, first.ID)
			}
		})
	}
}

// The claim and the confirmation are different facts. An agent marks a comment
// addressed and the queue shows it as a claim, which is what it is, and nothing
// an agent can reach closes it.
func TestAnAgentCannotReachResolved(t *testing.T) {
	_, s, _, c := commented(t)

	if _, err := s.AddressComment(t.Context(), c.ID); err != nil {
		t.Fatalf("addressing the comment: %v", err)
	}

	_, err := s.AddressComment(t.Context(), c.ID)
	var state *review.CommentStateError
	if !errors.As(err, &state) {
		t.Fatalf("err = %v, want a refusal naming the state it is in", err)
	}
	if state.Is != store.CommentAddressed {
		t.Errorf("the refusal says it is %s, want addressed", state.Is)
	}

	if _, err := s.ResolveComment(t.Context(), c.ID); err != nil {
		t.Fatalf("resolving the comment: %v", err)
	}
	if _, err := s.AddressComment(t.Context(), c.ID); !errors.As(err, &state) {
		t.Errorf("err = %v, want a resolved comment to refuse being addressed", err)
	}
}

// The code an orphan was about is gone, and saying it is dealt with is the
// reader's call. Nothing else clears it out of the queue.
func TestResolvingClosesAnOrphan(t *testing.T) {
	f, s, _, c := commented(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	f.refresh(s)

	if _, err := s.ResolveComment(t.Context(), c.ID); err != nil {
		t.Fatalf("resolving the orphan: %v", err)
	}
	assertComments(t, f.storedComments(s), []string{"code.txt head 10:10 resolved"})
}

// The database holds every session in the repository, and one session's ids are
// not another's business.
func TestAnUnknownCommentIsRefusedByBothVerbs(t *testing.T) {
	_, s, _, _ := commented(t)

	for name, verb := range map[string]func(string) error{
		"address": func(id string) error { _, err := s.AddressComment(t.Context(), id); return err },
		"resolve": func(id string) error { _, err := s.ResolveComment(t.Context(), id); return err },
	} {
		t.Run(name, func(t *testing.T) {
			var missing *review.NoCommentError
			if err := verb("4f1c8a2b3d9e"); !errors.As(err, &missing) {
				t.Fatalf("err = %v, want it to say the session has no such comment", err)
			}
		})
	}
}

// A comment anchored to an older generation is never picked up by the carry, so
// it would sit there never moving again. The refusal is the same one a mark
// gets, for the same reason.
func TestACommentAgainstAStaleGenerationIsRefused(t *testing.T) {
	f, s, first, _ := commented(t)

	f.Write("code.txt", numbered(101, 105)+numbered(1, 20))
	f.refresh(s)

	_, err := s.AddComment(t.Context(), first, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeLine,
		Range: review.Range{Start: 4, End: 4},
		Body:  "too late",
	})
	var stale *review.StaleGenerationError
	if !errors.As(err, &stale) {
		t.Fatalf("err = %v, want a stale generation refusal", err)
	}
}

// The blob is the exact bytes the comment was about, held alive by the session
// ref and immune to every rename after it.
func TestACommentRecordsTheBlobItWasWrittenAgainst(t *testing.T) {
	f, _, g, c := commented(t)

	want := f.genFiles(g)["code.txt"].HeadBlob
	if want == "" {
		t.Fatal("the generation recorded no head blob for code.txt")
	}
	if c.AnchorBlob != want {
		t.Errorf("anchorBlob = %q, want the head blob of the generation it was written at, %q", c.AnchorBlob, want)
	}
	if f.storedComment(c.ID).AnchorBlob != want {
		t.Error("the blob came back from the session and not from the row")
	}
}

// Nothing anchors it, no blob describes it, and no listing would ever show it.
func TestACommentOnAPathTheGenerationDoesNotHoldIsRefused(t *testing.T) {
	_, s, g, _ := commented(t)

	_, err := s.AddComment(t.Context(), g, review.Note{
		Path:  "absent.txt",
		Side:  store.SideHead,
		Scope: store.ScopeFile,
		Body:  "about nothing",
	})
	if err == nil {
		t.Fatal("a comment on a path the changeset does not hold should be refused")
	}
	if !strings.Contains(err.Error(), "absent.txt") {
		t.Errorf("err = %v, want it to name the path", err)
	}
}

// An added file has no base blob and a deleted one has no head blob. A note on
// the side it is missing from anchors to no bytes at all, and it cannot even
// orphan: that side's diff never lists a file it does not have, so the anchor
// would come through untouched on every refresh forever.
func TestACommentOnASideTheFileIsNotOnIsRefused(t *testing.T) {
	f := newFixture(t)
	f.Write("gone.txt", numbered(1, 20))
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Git("rm", "-q", "gone.txt")
	f.Write("added.txt", numbered(1, 20))
	f.Commit("drop one and add another")

	s := f.mustOpen("")
	g := f.refresh(s)

	for _, tc := range []struct {
		name string
		path string
		side store.Side
	}{
		{name: "the base side of a file that was added", path: "added.txt", side: store.SideBase},
		{name: "the head side of a file that was deleted", path: "gone.txt", side: store.SideHead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.AddComment(t.Context(), g, review.Note{
				Path:  tc.path,
				Side:  tc.side,
				Scope: store.ScopeFile,
				Body:  "about bytes that are not there",
			})
			if err == nil {
				t.Fatal("a comment on a side the file is not on should be refused")
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Errorf("err = %v, want it to name the file", err)
			}
		})
	}
}

// A scope is a claim about what the comment is on, and the lines are how it is
// kept. The two disagreeing gets a sentence here rather than a constraint
// violation from three layers down.
func TestANoteThatDisagreesWithItselfIsRefused(t *testing.T) {
	_, s, g, _ := commented(t)

	for _, tc := range []struct {
		name string
		note review.Note
	}{
		{
			name: "nothing said",
			note: review.Note{Side: store.SideHead, Scope: store.ScopeLine, Range: review.Range{Start: 4, End: 4}},
		},
		{
			name: "a side that is neither",
			note: review.Note{Side: "middle", Scope: store.ScopeFile, Body: "where"},
		},
		{
			name: "a scope outside the three",
			note: review.Note{Side: store.SideHead, Scope: "session", Body: "elsewhere"},
		},
		{
			name: "a file comment carrying lines",
			note: review.Note{
				Side: store.SideHead, Scope: store.ScopeFile,
				Range: review.Range{Start: 4, End: 9}, Body: "both at once",
			},
		},
		{
			name: "a line comment over a span",
			note: review.Note{
				Side: store.SideHead, Scope: store.ScopeLine,
				Range: review.Range{Start: 4, End: 9}, Body: "which line",
			},
		},
		{
			name: "a range comment carrying none",
			note: review.Note{Side: store.SideHead, Scope: store.ScopeRange, Body: "which lines"},
		},
		{
			name: "a range ending before it starts",
			note: review.Note{
				Side: store.SideHead, Scope: store.ScopeRange,
				Range: review.Range{Start: 9, End: 4}, Body: "backwards",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.note.Path = "code.txt"
			if _, err := s.AddComment(t.Context(), g, tc.note); err == nil {
				t.Fatal("the note should have been refused")
			}
		})
	}
}

// A hunk is commented on at the side and lines it is named by, so the CLI and
// the TUI both write the same anchor rather than each deriving one.
func TestCommentingOnAHunkAnchorsToWhatItIsNamedBy(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)

	h, found := f.changeset(s, g).Hunk("code.txt", store.SideHead, 1)
	if !found {
		t.Fatal("the changeset has no hunk of code.txt named head 1")
	}
	f.note(s, g, review.NoteOnHunk("code.txt", h, "the whole of this is unnecessary"))

	assertComments(t, f.storedComments(s), []string{"code.txt head 1:20 open"})
}

// The scope of a comment on lines falls out of the lines rather than out of how
// a caller spelled them, so one place decides it and no two callers disagree.
func TestCommentingOnLinesTakesItsScopeFromThem(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)

	for _, tc := range []struct {
		name  string
		lines review.Range
		scope store.Scope
	}{
		{name: "one line", lines: review.Range{Start: 4, End: 4}, scope: store.ScopeLine},
		{name: "several lines", lines: review.Range{Start: 4, End: 9}, scope: store.ScopeRange},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := f.note(s, g, review.NoteOnLines("code.txt", store.SideHead, tc.lines, "here"))

			got := f.storedComment(c.ID)
			if got.Scope != tc.scope {
				t.Errorf("scope = %s, want %s", got.Scope, tc.scope)
			}
			if got.Start != tc.lines.Start || got.End != tc.lines.End {
				t.Errorf("lines = %d:%d, want %d:%d", got.Start, got.End, tc.lines.Start, tc.lines.End)
			}
		})
	}
}

// A file comment is anchored on the side the file has bytes on. A deleted file
// has none on the head, and an anchor there would name bytes that are not there
// and survive every rewrite of the ones it actually removed.
func TestCommentingOnAFileTakesTheSideItHasBytesOn(t *testing.T) {
	f := branched(t)
	f.Write("added.txt", "brand new\n")
	f.Git("rm", "-q", "a.txt")
	f.Commit("one of each")

	s := f.mustOpen("")
	g := f.refresh(s)
	c := f.changeset(s, g)

	for _, tc := range []struct {
		path string
		side store.Side
	}{
		{path: "added.txt", side: store.SideHead},
		{path: "a.txt", side: store.SideBase},
	} {
		t.Run(tc.path, func(t *testing.T) {
			file, found := c.File(tc.path)
			if !found {
				t.Fatalf("the changeset has no %s", tc.path)
			}

			got := f.storedComment(f.note(s, g, review.NoteOnFile(file, "about the whole thing")).ID)
			switch {
			case got.Side != tc.side:
				t.Errorf("side = %s, want %s", got.Side, tc.side)
			case got.Scope != store.ScopeFile:
				t.Errorf("scope = %s, want file", got.Scope)
			case got.Start != 0 || got.End != 0:
				t.Errorf("lines = %d:%d, want 0:0: a file comment names no line", got.Start, got.End)
			}
		})
	}
}
