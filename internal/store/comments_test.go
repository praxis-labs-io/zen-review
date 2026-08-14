package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/store"
)

// holding is one generation holding a.go, which is what a comment needs
// something to anchor to.
func holding(t *testing.T, db *store.DB, s store.Session, commit string) store.Generation {
	t.Helper()

	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: commit, CreatedAt: epoch,
	}, []store.GenFile{{Path: "a.go", Status: diff.FileModified, BaseBlob: "b1", HeadBlob: "h1"}}, store.Carry{})
	if err != nil {
		t.Fatalf("adding the generation %s: %v", commit, err)
	}
	return g
}

// comment writes one open line comment on a.go.
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

// Callers ask before they write, and an unknown id is a message about the id
// rather than a failure of the read.
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

// A refresh translates the open comments of the generation it is moving off.
// Everything else has stopped moving, and a resolved comment carried forward
// would be reopening a question somebody closed.
func TestOnlyTheOpenCommentsOfOneGenerationAreCarried(t *testing.T) {
	db := open(t)
	s := session(t, db, "queue")
	first := holding(t, db, s, "one")
	second := holding(t, db, s, "two")

	comment(t, db, s, first, "open-here", 4)
	comment(t, db, s, second, "open-later", 7)

	closed := comment(t, db, s, first, "resolved-here", 11)
	if err := db.FreezeComment(t.Context(), closed.ID, store.CommentResolved, "a.go", 11, epoch); err != nil {
		t.Fatalf("resolving the comment: %v", err)
	}

	got, err := db.OpenComments(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("reading the open comments: %v", err)
	}
	if len(got) != 1 || got[0].ID != "open-here" {
		t.Fatalf("open comments = %+v, want only open-here", got)
	}
}

// Every comment of the session, live and frozen, by file and then down the file.
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

// A surviving anchor moves onto the generation being written and takes the
// file's new name with it, which is what follows a rename.
func TestACarriedAnchorMovesOntoTheNewGeneration(t *testing.T) {
	db := open(t)
	s := session(t, db, "moving")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "carried", 4)

	second, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch.Add(time.Hour),
	}, []store.GenFile{{Path: "b.go", Status: diff.FileRenamed, OldPath: "a.go"}}, store.Carry{
		Comments: []store.CommentMove{{ID: c.ID, Path: "b.go", LineRange: store.LineRange{Start: 11, End: 11}}},
	})
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
	if !got.UpdatedAt.Equal(epoch.Add(time.Hour)) {
		t.Errorf("updatedAt = %s, want the generation's time", got.UpdatedAt)
	}
}

// A lost anchor orphans the comment where it stands: it keeps the generation it
// last made sense at, and the place it was is written down where a reader is
// shown it.
func TestALostAnchorOrphansTheCommentWhereItStands(t *testing.T) {
	db := open(t)
	s := session(t, db, "orphaning")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "lost", 4)

	if _, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch.Add(time.Hour),
	}, nil, store.Carry{Comments: []store.CommentMove{{ID: c.ID, Lost: true}}}); err != nil {
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

// A generation whose comments moved without it leaves every anchor pointing at a
// generation that is no longer the latest, and the next refresh would find none
// of them. A line comment stretched over a span is the cheapest way to make the
// move fail: the schema refuses it.
func TestAGenerationAndItsCommentMovesLandTogetherOrNotAtAll(t *testing.T) {
	db := open(t)
	s := session(t, db, "atomic-comments")
	first := holding(t, db, s, "one")
	c := comment(t, db, s, first, "stretched", 4)

	_, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, nil, store.Carry{
		Comments: []store.CommentMove{{ID: c.ID, Path: "a.go", LineRange: store.LineRange{Start: 4, End: 9}}},
	})
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

// The state and the location go in one write. A frozen comment without one is a
// comment that lost where it was, and there is no later pass to fill it in.
func TestFreezingACommentRecordsWhereItWas(t *testing.T) {
	db := open(t)
	s := session(t, db, "freezing")
	g := holding(t, db, s, "one")
	c := comment(t, db, s, g, "claimed", 4)

	later := epoch.Add(time.Hour)
	if err := db.FreezeComment(t.Context(), c.ID, store.CommentAddressed, c.Path, c.Start, later); err != nil {
		t.Fatalf("addressing the comment: %v", err)
	}

	got, _, err := db.Comment(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("reading the comment: %v", err)
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

// The vocabulary is closed and the column is what every listing filters on, so a
// state outside it is a bug above this layer rather than a row to keep.
func TestACommentStateOutsideTheVocabularyIsRefused(t *testing.T) {
	db := open(t)
	s := session(t, db, "states")
	g := holding(t, db, s, "one")

	for _, state := range []store.CommentState{
		store.CommentOpen, store.CommentAddressed, store.CommentResolved, store.CommentOrphaned,
	} {
		c := comment(t, db, s, g, string(state), 4)
		if err := db.FreezeComment(t.Context(), c.ID, state, "a.go", 4, epoch); err != nil {
			t.Errorf("the state %q was refused: %v", state, err)
		}
	}

	c := comment(t, db, s, g, "outside", 4)
	if err := db.FreezeComment(t.Context(), c.ID, store.CommentState("done"), "a.go", 4, epoch); err == nil {
		t.Error("a state outside the vocabulary should be refused")
	}
}
