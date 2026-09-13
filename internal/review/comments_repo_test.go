package review_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func (f *fixture) note(s *review.Session, g review.Generation, n review.Note) store.Comment {
	f.t.Helper()

	c, err := s.AddComment(f.t.Context(), g, n)
	if err != nil {
		f.t.Fatalf("writing the comment on %s: %v", n.Path, err)
	}
	return c
}

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

	if got.Scope != store.ScopeLine || got.Start != got.End {
		t.Errorf("comment = %s %d:%d, want one line", got.Scope, got.Start, got.End)
	}
}

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

func TestAFileCommentSurvivesTheFileChanging(t *testing.T) {
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

	assertComments(t, f.storedComments(s), []string{"code.txt head 0:0 open"})
}

func TestAFileCommentOrphansWhenTheFileGoes(t *testing.T) {
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

	f.Git("rm", "-q", "-f", "code.txt")
	f.refresh(s)

	assertComments(t, f.storedComments(s), []string{"code.txt head 0:0 orphaned"})
}

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

	f.note(s, g, review.Note{
		Path:  "new.txt",
		Side:  store.SideBase,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 10, End: 12},
		Body:  "these three were load bearing",
	})
	assertComments(t, f.storedComments(s), []string{"old.txt base 10:12 open"})
}

func TestACommentThatHasStoppedMovingStaysWhereItWas(t *testing.T) {
	for _, tc := range []struct {
		state store.CommentState
		stop  func(*review.Session, string) error
	}{
		{
			state: store.CommentAddressed,
			stop: func(s *review.Session, id string) error {
				_, err := s.AddressComment(t.Context(), id, "")
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

func TestAnAgentCannotReachResolved(t *testing.T) {
	_, s, _, c := commented(t)

	if _, err := s.AddressComment(t.Context(), c.ID, ""); err != nil {
		t.Fatalf("addressing the comment: %v", err)
	}

	_, err := s.AddressComment(t.Context(), c.ID, "")
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
	if _, err := s.AddressComment(t.Context(), c.ID, ""); !errors.As(err, &state) {
		t.Errorf("err = %v, want a resolved comment to refuse being addressed", err)
	}
}

func TestAnOrphanCanStillBeAnswered(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	s := f.mustOpen("")
	g := f.refresh(s)
	c := f.note(s, g, review.Note{
		Path:  "code.txt",
		Side:  store.SideHead,
		Scope: store.ScopeRange,
		Range: review.Range{Start: 10, End: 12},
		Body:  "these three lines duplicate the loop above",
	})

	f.Write("code.txt", numbered(1, 9)+"deduplicated\n"+numbered(13, 20))
	f.refresh(s)

	if got := f.storedComment(c.ID).State; got != store.CommentOrphaned {
		t.Fatalf("state = %s, want orphaned so the case under test is the real one", got)
	}

	addressed, err := s.AddressComment(t.Context(), c.ID, "folded the three into one call")
	if err != nil {
		t.Fatalf("addressing an orphaned comment: %v", err)
	}
	if addressed.State != store.CommentAddressed {
		t.Errorf("state = %s, want addressed", addressed.State)
	}
	if addressed.Response != "folded the three into one call" {
		t.Errorf("response = %q, want what the agent wrote", addressed.Response)
	}
	if f.storedComment(c.ID).Response != "folded the three into one call" {
		t.Error("the response came back from the session and not from the row")
	}
}

func TestAddressingCarriesTheWordsThatBackIt(t *testing.T) {
	f, s, _, c := commented(t)

	addressed, err := s.AddressComment(t.Context(), c.ID, "the retry loop needs it first")
	if err != nil {
		t.Fatalf("addressing the comment: %v", err)
	}
	if addressed.Response != "the retry loop needs it first" {
		t.Errorf("response = %q, want what was written", addressed.Response)
	}
	if addressed.Body != c.Body {
		t.Errorf("body = %q, want the reader's words left alone", addressed.Body)
	}

	if got := f.storedComment(c.ID); got.Response != "the retry loop needs it first" {
		t.Errorf("stored response = %q, want what was written", got.Response)
	}
}

func TestAddressingTakesNoAnswerAtAll(t *testing.T) {
	f, s, _, c := commented(t)

	addressed, err := s.AddressComment(t.Context(), c.ID, "   \n\t ")
	if err != nil {
		t.Fatalf("addressing with no response: %v", err)
	}
	if addressed.Response != "" {
		t.Errorf("response = %q, want whitespace to count as none", addressed.Response)
	}
	if got := f.storedComment(c.ID); got.State != store.CommentAddressed {
		t.Errorf("state = %s, want the address to have landed anyway", got.State)
	}
}

func TestAResponseSurvivesARefreshAndAResolve(t *testing.T) {
	f, s, _, c := commented(t)

	if _, err := s.AddressComment(t.Context(), c.ID, "rewritten above"); err != nil {
		t.Fatalf("addressing the comment: %v", err)
	}

	f.Write("code.txt", numbered(1, 5)+"an inserted line\n"+numbered(6, 20))
	f.refresh(s)

	if got := f.storedComment(c.ID); got.Response != "rewritten above" {
		t.Fatalf("the refresh left the response as %q", got.Response)
	}

	resolved, err := s.ResolveComment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("resolving the comment: %v", err)
	}
	if resolved.Response != "rewritten above" {
		t.Errorf("the resolve took the response, leaving %q", resolved.Response)
	}
}

func TestResolvingClosesAnOrphan(t *testing.T) {
	f, s, _, c := commented(t)

	f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
	f.refresh(s)

	if _, err := s.ResolveComment(t.Context(), c.ID); err != nil {
		t.Fatalf("resolving the orphan: %v", err)
	}
	assertComments(t, f.storedComments(s), []string{"code.txt head 10:10 resolved"})
}

func TestAnUnknownCommentIsRefusedByBothVerbs(t *testing.T) {
	_, s, _, _ := commented(t)

	for name, verb := range map[string]func(string) error{
		"address": func(id string) error { _, err := s.AddressComment(t.Context(), id, ""); return err },
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
			name: "a hunk comment carrying none",
			note: review.Note{Side: store.SideHead, Scope: store.ScopeHunk, Body: "which hunk"},
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

	cs, err := f.db().Comments(t.Context(), s.ID())
	if err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	if got := cs[0].Scope; got != store.ScopeHunk {
		t.Errorf("scope = %q, want hunk: the reader named the hunk, not its lines", got)
	}
}

func TestCommentsComeBackInTheOrderTheChangesetDoes(t *testing.T) {
	f := branched(t)
	f.Write("main.go", numbered(1, 5))
	f.Write("pkg/deep.go", numbered(1, 5))
	f.Commit("a file beside a directory")

	s := f.mustOpen("")
	g := f.refresh(s)

	files := f.changeset(s, g).Files
	for _, file := range files {
		f.note(s, g, review.Note{
			Path: file.Diff.Path, Side: store.SideHead, Scope: store.ScopeFile,
			Body: "about " + file.Diff.Path,
		})
	}

	cs, err := s.Comments(t.Context())
	if err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	if len(cs) != len(files) {
		t.Fatalf("comments = %d, want one per file and there are %d", len(cs), len(files))
	}

	for i, file := range files {
		if cs[i].Path != file.Diff.Path {
			t.Errorf("comment %d is on %s, and the changeset reads %s there", i, cs[i].Path, file.Diff.Path)
		}
	}
	if cs[0].Path != "pkg/deep.go" {
		t.Errorf("the listing opens on %s, so the two orderings agreeing proves nothing", cs[0].Path)
	}
}

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

func TestAnEditRewritesTheBodyAndLeavesTheAnchor(t *testing.T) {
	f, s, _, c := commented(t)

	edited, err := s.EditComment(t.Context(), c.ID, "this reads forwards")
	if err != nil {
		t.Fatalf("rewriting the comment: %v", err)
	}
	if edited.Body != "this reads forwards" {
		t.Errorf("body = %q, want what was typed", edited.Body)
	}

	got := f.storedComment(c.ID)
	if got.Body != "this reads forwards" {
		t.Errorf("stored body = %q, want what was typed", got.Body)
	}
	if got.Path != c.Path || got.Side != c.Side || got.LineRange != c.LineRange {
		t.Errorf("anchor = %s %s %d:%d, want it left at %s %s %d:%d",
			got.Path, got.Side, got.Start, got.End, c.Path, c.Side, c.Start, c.End)
	}
	if got.UpdatedAt.Before(c.UpdatedAt) {
		t.Errorf("updatedAt = %s, want it stamped no earlier than %s", got.UpdatedAt, c.UpdatedAt)
	}
}

func TestAnEditRefusesAnEmptyBody(t *testing.T) {
	f, s, _, c := commented(t)

	if _, err := s.EditComment(t.Context(), c.ID, "   \n "); err == nil {
		t.Fatal("a body with nothing in it should be refused")
	}
	if got := f.storedComment(c.ID); got.Body != c.Body {
		t.Errorf("body = %q, want the refusal to have left %q", got.Body, c.Body)
	}
}

func TestADeleteTakesTheCommentOutOfTheSession(t *testing.T) {
	f, s, _, c := commented(t)

	gone, err := s.DeleteComment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("deleting the comment: %v", err)
	}
	if gone.Body != c.Body {
		t.Errorf("answered with %q, want the comment it removed", gone.Body)
	}
	assertComments(t, f.storedComments(s), nil)
}

func TestEditAndDeleteReachASettledComment(t *testing.T) {
	for _, tt := range []struct {
		name string
		act  func(*review.Session, string) error
		want []string
	}{
		{"an edit", func(s *review.Session, id string) error {
			_, err := s.EditComment(t.Context(), id, "still worth saying")
			return err
		}, []string{"code.txt head 10:10 resolved"}},
		{"a delete", func(s *review.Session, id string) error {
			_, err := s.DeleteComment(t.Context(), id)
			return err
		}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, s, _, c := commented(t)
			if _, err := s.ResolveComment(t.Context(), c.ID); err != nil {
				t.Fatalf("resolving the comment: %v", err)
			}

			if err := tt.act(s, c.ID); err != nil {
				t.Fatalf("reaching a resolved comment: %v", err)
			}
			assertComments(t, f.storedComments(s), tt.want)
		})
	}
}

func TestAnUnknownCommentIsRefusedByEditAndDelete(t *testing.T) {
	_, s, _, _ := commented(t)

	for name, verb := range map[string]func(string) error{
		"edit":   func(id string) error { _, err := s.EditComment(t.Context(), id, "hello"); return err },
		"delete": func(id string) error { _, err := s.DeleteComment(t.Context(), id); return err },
	} {
		t.Run(name, func(t *testing.T) {
			var missing *review.NoCommentError
			if err := verb("4f1c8a2b3d9e"); !errors.As(err, &missing) {
				t.Fatalf("err = %v, want it to say the session has no such comment", err)
			}
		})
	}
}

func TestAnEditLandsAfterTheGenerationMoved(t *testing.T) {
	f, s, _, c := commented(t)

	f.Write("code.txt", numbered(101, 105)+numbered(1, 20))
	f.refresh(s)

	if _, err := s.EditComment(t.Context(), c.ID, "this reads forwards"); err != nil {
		t.Fatalf("rewriting a comment the refresh moved: %v", err)
	}

	got := f.storedComment(c.ID)
	if got.Body != "this reads forwards" {
		t.Errorf("body = %q, want what was typed", got.Body)
	}
	if got.Start != 15 {
		t.Errorf("anchor = %d, want the 15 the refresh carried it to", got.Start)
	}
}
