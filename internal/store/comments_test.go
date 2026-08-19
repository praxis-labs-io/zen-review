package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// holding is one generation holding a.go, which is what a comment needs
// something to anchor to.
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

	// Each comment is written while its own generation is the latest, because
	// that is the only time a comment can be written at all.
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
	// The lines under a comment moving is the code changing and not the comment.
	// Stamping it here would reset every comment in the session on every refresh,
	// which costs the column the only thing it says.
	if !got.UpdatedAt.Equal(epoch) {
		t.Errorf("updatedAt = %s, want the carry to have left it at %s", got.UpdatedAt, epoch)
	}
	// The blob and the range that slices it are one fact. Moving the range would
	// leave the blob sliced by lines it never had.
	if got.CreatedRange != (store.LineRange{Start: 4, End: 4}) || got.AnchorBlob != "h1" {
		t.Errorf("created = %s %+v, want the carry to have left both alone", got.AnchorBlob, got.CreatedRange)
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

// The state and the location go in one write, the location read off the row
// rather than taken from the caller. A frozen comment without one is a comment
// that lost where it was, and there is no later pass to fill it in.
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
	// The row it answered with is the row it wrote. A caller reading the state off
	// its own copy would be reading the one it held before the write.
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

// A carry moving an open anchor leaves the state alone, so the swap still wins.
// What it records has to be where the anchor is by then, not where the caller
// read it a moment before.
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

	// c is the read the caller started from, and says a.go:4.
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

// The state a caller read is what the write lands against. Without the swap the
// decision and the write are two statements, and a resolve landing between them
// would be overwritten by an address that was refused the moment it was read.
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

// TestCommentsAtRefusesAGenerationThatHasMoved. A caller pairing comments with a
// changeset needs both off one generation; a refresh landing between the two
// reads translates every open anchor, and the pair is then live line numbers
// against a diff that has moved.
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

// TestAnEditRewritesTheBodyAndNothingElse. The anchor is not a body's business,
// and a rewrite that moved one would be a remap with no translation behind it.
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

// TestADeleteHandsBackTheRowThatWent, which is what a caller prints. Nothing
// else can say what a comment said once it has gone.
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

// TestAnEditOrDeleteReachesOneSessionsCommentsAlone. The database is shared by
// every session in the repository, and one session's ids are not another's.
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

// TestARefreshCarriesPastACommentThatHasGone. A delete is what makes a move name
// nothing, and the generation it was written against still has to land.
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

// ptr is the response a freeze names. Nil is a transition leaving the row's own
// answer alone, so a caller writing one has to hand over an address.
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

// A resolve names no response, so the one an address left has to survive it. The
// alternative is reading it and writing it back, which loses an edit that landed
