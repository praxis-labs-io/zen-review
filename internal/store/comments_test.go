package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func holding(t *testing.T, db *store.DB, s store.Session, commit string) store.Generation {
	t.Helper()

	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: commit, CreatedAt: epoch,
	}, []store.GenFile{{Path: "a.go", Status: diff.FileModified, BaseBlob: "b1", HeadBlob: "h1"}}, store.Advance{})
	if err != nil {
		t.Fatalf("adding the generation %s: %v", commit, err)
	}
	return g
}

func comment(t *testing.T, db *store.DB, s store.Session, g store.Generation, id string, line int) store.Comment {
	t.Helper()

	c := store.Comment{
		ID:                  id,
		SessionID:           s.ID,
		GenerationID:        g.ID,
		CreatedGenerationID: g.ID,
		Path:                "a.go",
		Side:                store.SideHead,
		LineRange:           store.LineRange{Start: line, End: line},
		Scope:               store.ScopeLine,
		Body:                "this reads backwards",
		State:               store.CommentOpen,
		AnchorBlob:          "h1",
		CreatedRange:        store.LineRange{Start: line, End: line},
		CreatedAt:           epoch,
		UpdatedAt:           epoch,
	}
	if err := db.AddComment(t.Context(), c); err != nil {
		t.Fatalf("writing the comment %s: %v", id, err)
	}
	return c
}

func TestACommentRoundTrips(t *testing.T) {
	db := open(t)
	s := session(t, db, "commented")
	g := holding(t, db, s, "one")

	want := store.Comment{
		ID:                  "4f1c8a2b3d9e",
		SessionID:           s.ID,
		GenerationID:        g.ID,
		CreatedGenerationID: g.ID,
		Path:                "a.go",
		Side:                store.SideBase,
		LineRange:           store.LineRange{Start: 4, End: 9},
		Scope:               store.ScopeRange,
		Body:                "this reads backwards\nand the second line survives too",
		State:               store.CommentOrphaned,
		AnchorBlob:          "b1",
		CreatedRange:        store.LineRange{Start: 2, End: 7},
		LastPath:            "old.go",
		LastLine:            4,
		CreatedAt:           epoch,
		UpdatedAt:           epoch.Add(time.Hour),
	}
	if err := db.AddComment(t.Context(), want); err != nil {
		t.Fatalf("writing the comment: %v", err)
	}

	got, found, err := db.Comment(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
	}
	if !found {
		t.Fatal("the comment was written and did not come back")
	}
	if got != want {
		t.Errorf("comment = %+v, want %+v", got, want)
	}
}

func TestAnUnknownCommentIsAbsenceRatherThanAnError(t *testing.T) {
	db := open(t)

	got, found, err := db.Comment(t.Context(), "no-such-comment")
	if err != nil {
		t.Fatalf("reading an unknown comment: %v", err)
	}
	if found {
		t.Errorf("found = true for a comment that was never written, got %+v", got)
	}
}

func TestOnlyTheOpenCommentsOfOneGenerationAreCarried(t *testing.T) {
	db := open(t)
	s := session(t, db, "queue")

	first := holding(t, db, s, "one")
	comment(t, db, s, first, "open-here", 4)

	closed := comment(t, db, s, first, "resolved-here", 11)
	if _, _, err := db.FreezeComment(t.Context(), closed.ID,
		store.CommentOpen, store.CommentResolved, nil, epoch); err != nil {
		t.Fatalf("resolving the comment: %v", err)
	}

	second := holding(t, db, s, "two")
	comment(t, db, s, second, "open-later", 7)

	got, err := db.OpenComments(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("reading the open comments: %v", err)
	}
	if len(got) != 1 || got[0].ID != "open-here" {
		t.Fatalf("open comments = %+v, want only open-here", got)
	}
}

func TestCommentsComeBackInReadingOrder(t *testing.T) {
	db := open(t)
	s := session(t, db, "ordered")
	g := holding(t, db, s, "one")

	comment(t, db, s, g, "third", 40)
	comment(t, db, s, g, "first", 4)
	comment(t, db, s, g, "second", 12)

	got, err := db.Comments(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("reading the comments: %v", err)
	}

	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("got %d comments, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("comments = %+v, want %v", got, want)
		}
	}
}

func TestACarriedAnchorMovesOntoTheNewGeneration(t *testing.T) {
	db := open(t)
	s := session(t, db, "moving")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "carried", 4)

	second, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch.Add(time.Hour),
	}, []store.GenFile{{Path: "b.go", Status: diff.FileRenamed, OldPath: "a.go"}}, carrying(first, store.Carry{
		Comments: []store.CommentMove{{ID: c.ID, Path: "b.go", LineRange: store.LineRange{Start: 11, End: 11}}},
	}))
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	got, _, err := db.Comment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
	}
	if got.GenerationID != second.ID {
		t.Errorf("generationID = %d, want the generation it moved onto, %d", got.GenerationID, second.ID)
	}
	if got.CreatedGenerationID != first.ID {
		t.Errorf("createdGenerationID = %d, want the one it was written at, %d", got.CreatedGenerationID, first.ID)
	}
	if got.Path != "b.go" || got.Start != 11 || got.End != 11 {
		t.Errorf("anchor = %s %d:%d, want b.go 11:11", got.Path, got.Start, got.End)
	}
	if got.State != store.CommentOpen {
		t.Errorf("state = %s, want it still open", got.State)
	}
	if !got.UpdatedAt.Equal(epoch) {
		t.Errorf("updatedAt = %s, want the carry to have left it at %s", got.UpdatedAt, epoch)
	}
	if got.CreatedRange != (store.LineRange{Start: 4, End: 4}) || got.AnchorBlob != "h1" {
		t.Errorf("created = %s %+v, want the carry to have left both alone", got.AnchorBlob, got.CreatedRange)
	}
}

func TestALostAnchorOrphansTheCommentWhereItStands(t *testing.T) {
	db := open(t)
	s := session(t, db, "orphaning")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "lost", 4)

	if _, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch.Add(time.Hour),
	}, nil, carrying(first, store.Carry{Comments: []store.CommentMove{{ID: c.ID, Lost: true}}})); err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	got, _, err := db.Comment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
	}
	if got.State != store.CommentOrphaned {
		t.Errorf("state = %s, want orphaned", got.State)
	}
	if got.GenerationID != first.ID {
		t.Errorf("generationID = %d, want it left at %d", got.GenerationID, first.ID)
	}
	if got.LastPath != "a.go" || got.LastLine != 4 {
		t.Errorf("last known = %s:%d, want a.go:4", got.LastPath, got.LastLine)
	}
}

func TestAGenerationAndItsCommentMovesLandTogetherOrNotAtAll(t *testing.T) {
	db := open(t)
	s := session(t, db, "atomic-comments")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "stretched", 4)

	_, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, nil, carrying(first, store.Carry{
		Comments: []store.CommentMove{{ID: c.ID, Path: "a.go", LineRange: store.LineRange{Start: 4, End: 9}}},
	}))
	if err == nil {
		t.Fatal("a line comment stretched over a span should not write")
	}
	if !strings.Contains(err.Error(), c.ID) {
		t.Errorf("err = %v, want it to name the comment that failed", err)
	}

	latest, _, err := db.LatestGeneration(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("reading the latest generation: %v", err)
	}
	if latest.ID != first.ID {
		t.Errorf("latest = %+v, want the first generation still standing", latest)
	}

	got, _, err := db.Comment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
	}
	if got.Start != 4 || got.End != 4 {
		t.Errorf("anchor = %d:%d, want it left at 4:4", got.Start, got.End)
	}
}

func TestFreezingACommentRecordsWhereItWas(t *testing.T) {
	db := open(t)
	s := session(t, db, "freezing")
	g := holding(t, db, s, "one")
	c := comment(t, db, s, g, "claimed", 4)

	later := epoch.Add(time.Hour)
	frozen, won, err := db.FreezeComment(t.Context(), c.ID,
		store.CommentOpen, store.CommentAddressed, nil, later)
	if err != nil {
		t.Fatalf("addressing the comment: %v", err)
	}
	if !won {
		t.Fatal("the write did not land against the state it was read in")
	}

	got, _, err := db.Comment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
	}
	if frozen != got {
		t.Errorf("answered with %+v, want the row it wrote, %+v", frozen, got)
	}
	if got.State != store.CommentAddressed {
		t.Errorf("state = %s, want addressed", got.State)
	}
	if got.LastPath != "a.go" || got.LastLine != 4 {
		t.Errorf("last known = %s:%d, want a.go:4", got.LastPath, got.LastLine)
	}
	if !got.UpdatedAt.Equal(later) {
		t.Errorf("updatedAt = %s, want %s", got.UpdatedAt, later)
	}
	if !got.CreatedAt.Equal(epoch) {
		t.Errorf("createdAt = %s, want it left at %s", got.CreatedAt, epoch)
	}
}

func TestFreezingRecordsTheAnchorTheRowHasNow(t *testing.T) {
	db := open(t)
	s := session(t, db, "moved-under")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "shifted", 4)

	if _, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, []store.GenFile{{Path: "b.go", Status: diff.FileRenamed, OldPath: "a.go"}}, carrying(first, store.Carry{
		Comments: []store.CommentMove{{ID: c.ID, Path: "b.go", LineRange: store.LineRange{Start: 11, End: 11}}},
	})); err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	frozen, won, err := db.FreezeComment(t.Context(), c.ID,
		store.CommentOpen, store.CommentResolved, nil, epoch.Add(time.Hour))
	if err != nil || !won {
		t.Fatalf("resolving the comment: won = %v, err = %v", won, err)
	}

	if frozen.LastPath != "b.go" || frozen.LastLine != 11 {
		t.Errorf("last known = %s:%d, want b.go:11, where the carry left it",
			frozen.LastPath, frozen.LastLine)
	}
}

func TestFreezingAgainstAStateThatMovedChangesNothing(t *testing.T) {
	db := open(t)
	s := session(t, db, "contended")
	g := holding(t, db, s, "one")
	c := comment(t, db, s, g, "raced", 4)

	_, won, err := db.FreezeComment(t.Context(), c.ID, store.CommentOpen, store.CommentResolved, nil, epoch)
	if err != nil || !won {
		t.Fatalf("resolving the comment: won = %v, err = %v", won, err)
	}

	later := epoch.Add(time.Hour)
	_, won, err = db.FreezeComment(t.Context(), c.ID, store.CommentOpen, store.CommentAddressed, nil, later)
	if err != nil {
		t.Fatalf("addressing the comment: %v", err)
	}
	if won {
		t.Fatal("a write against a state the comment left should not land")
	}

	got, _, err := db.Comment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
	}
	if got.State != store.CommentResolved {
		t.Errorf("state = %s, want the resolved it already was", got.State)
	}
	if !got.UpdatedAt.Equal(epoch) {
		t.Errorf("updatedAt = %s, want the losing write to have left it at %s", got.UpdatedAt, epoch)
	}
}

func TestACommentStateOutsideTheVocabularyIsRefused(t *testing.T) {
	db := open(t)
	s := session(t, db, "states")
	g := holding(t, db, s, "one")

	for _, state := range []store.CommentState{
		store.CommentOpen, store.CommentAddressed, store.CommentResolved, store.CommentOrphaned,
	} {
		c := comment(t, db, s, g, string(state), 4)
		if _, _, err := db.FreezeComment(t.Context(), c.ID,
			store.CommentOpen, state, nil, epoch); err != nil {
			t.Errorf("the state %q was refused: %v", state, err)
		}
	}

	c := comment(t, db, s, g, "outside", 4)
	if _, _, err := db.FreezeComment(t.Context(), c.ID,
		store.CommentOpen, store.CommentState("done"), nil, epoch); err == nil {
		t.Error("a state outside the vocabulary should be refused")
	}
}

func TestCommentsAtRefusesAGenerationThatHasMoved(t *testing.T) {
	db := open(t)
	s := session(t, db, "paired")

	first := holding(t, db, s, "one")
	comment(t, db, s, first, "4f1c8a2b3d9e", 12)

	if _, err := db.CommentsAt(t.Context(), s.ID, first.ID); err != nil {
		t.Fatalf("reading at the latest generation: %v", err)
	}

	holding(t, db, s, "two")
	if _, err := db.CommentsAt(t.Context(), s.ID, first.ID); !errors.Is(err, store.ErrStaleGeneration) {
		t.Errorf("err = %v, want store.ErrStaleGeneration", err)
	}
}

func TestAnEditRewritesTheBodyAndNothingElse(t *testing.T) {
	db := open(t)
	s := session(t, db, "edited")
	g := holding(t, db, s, "one")
	c := comment(t, db, s, g, "typo", 4)

	later := epoch.Add(time.Hour)
	edited, found, err := db.EditComment(t.Context(), c.ID, s.ID, "this reads forwards", later)
	if err != nil {
		t.Fatalf("rewriting the comment: %v", err)
	}
	if !found {
		t.Fatal("the comment was not there to rewrite")
	}

	want := c
	want.Body, want.UpdatedAt = "this reads forwards", later
	if edited != want {
		t.Errorf("answered with %+v, want %+v", edited, want)
	}

	got, _, err := db.Comment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
	}
	if got != want {
		t.Errorf("stored %+v, want %+v", got, want)
	}
}

func TestADeleteHandsBackTheRowThatWent(t *testing.T) {
	db := open(t)
	s := session(t, db, "deleted")
	g := holding(t, db, s, "one")
	c := comment(t, db, s, g, "unmeant", 4)

	gone, found, err := db.DeleteComment(t.Context(), c.ID, s.ID)
	if err != nil {
		t.Fatalf("deleting the comment: %v", err)
	}
	if !found {
		t.Fatal("the comment was not there to delete")
	}
	if gone != c {
		t.Errorf("answered with %+v, want the row it removed, %+v", gone, c)
	}

	if _, found, err = db.Comment(t.Context(), c.ID); err != nil || found {
		t.Errorf("the comment is still there: found = %v, err = %v", found, err)
	}
}

func TestAnEditOrDeleteReachesOneSessionsCommentsAlone(t *testing.T) {
	db := open(t)
	mine := session(t, db, "mine")
	g := holding(t, db, mine, "one")
	c := comment(t, db, mine, g, "notyours", 4)

	theirs := session(t, db, "theirs")

	for _, tt := range []struct {
		name string
		miss func() (store.Comment, bool, error)
	}{
		{"an edit", func() (store.Comment, bool, error) {
			return db.EditComment(t.Context(), c.ID, theirs.ID, "reaching over", epoch.Add(time.Hour))
		}},
		{"a delete", func() (store.Comment, bool, error) {
			return db.DeleteComment(t.Context(), c.ID, theirs.ID)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, found, err := tt.miss()
			if err != nil {
				t.Fatalf("reaching over: %v", err)
			}
			if found {
				t.Error("it reached a comment of another session")
			}

			got, _, err := db.Comment(t.Context(), c.ID)
			if err != nil || got != c {
				t.Errorf("the comment is %+v, want it left as %+v: %v", got, c, err)
			}
		})
	}
}

func TestARefreshCarriesPastACommentThatHasGone(t *testing.T) {
	db := open(t)
	s := session(t, db, "raced")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "vanishing", 4)

	if _, _, err := db.DeleteComment(t.Context(), c.ID, s.ID); err != nil {
		t.Fatalf("deleting the comment: %v", err)
	}

	if _, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, []store.GenFile{{Path: "a.go", Status: diff.FileModified, BaseBlob: "b1", HeadBlob: "h2"}},
		carrying(first, store.Carry{
			Comments: []store.CommentMove{{ID: c.ID, Path: "a.go", LineRange: store.LineRange{Start: 9, End: 9}}},
		})); err != nil {
		t.Fatalf("carrying past a comment that has gone: %v", err)
	}
}

func ptr(s string) *string { return &s }

func TestAResponseLandsInTheSameWriteAsTheState(t *testing.T) {
	db := open(t)
	s := session(t, db, "answered")
	g := holding(t, db, s, "one")
	c := comment(t, db, s, g, "why is this here", 4)

	if c.Response != "" {
		t.Fatalf("a fresh comment has no response, got %q", c.Response)
	}

	frozen, won, err := db.FreezeComment(t.Context(), c.ID,
		store.CommentOpen, store.CommentAddressed, ptr("the retry loop needs it"), epoch.Add(time.Hour))
	if err != nil || !won {
		t.Fatalf("addressing the comment: won = %v, err = %v", won, err)
	}
	if frozen.Response != "the retry loop needs it" {
		t.Errorf("the response came back as %q", frozen.Response)
	}

	read, found, err := db.Comment(t.Context(), c.ID)
	if err != nil || !found {
		t.Fatalf("reading the comment back: found = %v, err = %v", found, err)
	}
	if read.Response != "the retry loop needs it" {
		t.Errorf("the response was not stored, got %q", read.Response)
	}
}
