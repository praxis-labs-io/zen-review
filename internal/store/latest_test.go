package store_test

import (
	"errors"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/store"
)

var writes = map[string]func(t *testing.T, db *store.DB, s store.Session, generationID int64) error{
	"a mark": func(t *testing.T, db *store.DB, s store.Session, generationID int64) error {
		t.Helper()
		return writeSide(t.Context(), db, s.ID, generationID, "a.go", store.SideHead, epoch, "",
			keep(store.LineRange{Start: 1, End: 5}))
	},
	"a comment": func(t *testing.T, db *store.DB, s store.Session, generationID int64) error {
		t.Helper()
		return db.AddComment(t.Context(), store.Comment{
			ID: "raced", SessionID: s.ID, GenerationID: generationID, CreatedGenerationID: generationID,
			Path: "a.go", Side: store.SideHead, LineRange: store.LineRange{Start: 4, End: 4},
			Scope: store.ScopeLine, Body: "late", State: store.CommentOpen, AnchorBlob: "blob",
			CreatedAt: epoch, UpdatedAt: epoch,
		})
	},
}

func TestAWriteAgainstASupersededGenerationIsRefused(t *testing.T) {
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			s, first := generation(t, db, "superseded")
			if _, err := db.AddGeneration(t.Context(), store.Generation{
				SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
			}, nil, store.Advance{}); err != nil {
				t.Fatalf("adding the second generation: %v", err)
			}

			err := write(t, db, s, first.ID)

			if !errors.Is(err, store.ErrStaleGeneration) {
				t.Fatalf("err = %v, want store.ErrStaleGeneration", err)
			}
		})
	}
}

func TestAWriteAgainstASessionWithNoGenerationIsRefused(t *testing.T) {
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			s := session(t, db, "unbuilt")

			err := write(t, db, s, 1)

			if !errors.Is(err, store.ErrStaleGeneration) {
				t.Fatalf("err = %v, want store.ErrStaleGeneration", err)
			}
		})
	}
}

func TestARefusedWriteLeavesNoRowBehind(t *testing.T) {
	db := open(t)
	s, first := generation(t, db, "rolled-back")
	if _, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, nil, store.Advance{}); err != nil {
		t.Fatalf("adding the second generation: %v", err)
	}

	for name, write := range writes {
		if err := write(t, db, s, first.ID); !errors.Is(err, store.ErrStaleGeneration) {
			t.Fatalf("%s: err = %v, want store.ErrStaleGeneration", name, err)
		}
	}

	if got := ranges(t, db, first); len(got) != 0 {
		t.Errorf("ranges = %+v, want the refused mark to have left none", got)
	}
	got, err := db.Comments(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("comments = %+v, want the refused comment to have left none", got)
	}
}
