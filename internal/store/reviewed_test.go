package store_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/zen-review/zen-review/internal/store"
)

// generation is a session and one generation to hang ranges off.
func generation(t *testing.T, db *store.DB, id string) (store.Session, store.Generation) {
	t.Helper()

	s := session(t, db, id)
	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, nil, store.Carry{})
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}
	return s, g
}

// keep is the change function for a write that does not care what was there.
func keep(rs ...store.LineRange) func([]store.LineRange) []store.LineRange {
	return func([]store.LineRange) []store.LineRange { return rs }
}

func ranges(t *testing.T, db *store.DB, g store.Generation) []store.ReviewedRange {
	t.Helper()

	got, err := db.ReviewedRanges(t.Context(), g.ID)
	if err != nil {
		t.Fatalf("reading the reviewed ranges: %v", err)
	}
	return got
}

func TestReviewedRangesComeBackOrderedByPathSideAndLine(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "ordered")

	writes := []struct {
		path   string
		side   store.Side
		ranges []store.LineRange
	}{
		{"z.go", store.SideHead, []store.LineRange{{Start: 40, End: 44}, {Start: 10, End: 12}}},
		{"a.go", store.SideBase, []store.LineRange{{Start: 7, End: 7}}},
		{"a.go", store.SideHead, []store.LineRange{{Start: 1, End: 3}}},
	}
	for _, w := range writes {
		if err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, w.path, w.side, epoch, keep(w.ranges...)); err != nil {
			t.Fatalf("writing the ranges of %s: %v", w.path, err)
		}
	}

	want := []store.ReviewedRange{
		{Path: "a.go", Side: store.SideBase, LineRange: store.LineRange{Start: 7, End: 7}, CreatedAt: epoch},
		{Path: "a.go", Side: store.SideHead, LineRange: store.LineRange{Start: 1, End: 3}, CreatedAt: epoch},
		{Path: "z.go", Side: store.SideHead, LineRange: store.LineRange{Start: 10, End: 12}, CreatedAt: epoch},
		{Path: "z.go", Side: store.SideHead, LineRange: store.LineRange{Start: 40, End: 44}, CreatedAt: epoch},
	}

	got := ranges(t, db, g)
	if len(got) != len(want) {
		t.Fatalf("got %d ranges, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].Side != want[i].Side || got[i].LineRange != want[i].LineRange {
			t.Errorf("range %d = %+v, want %+v", i, got[i], want[i])
		}
		if !got[i].CreatedAt.Equal(want[i].CreatedAt) {
			t.Errorf("range %d createdAt = %s, want %s", i, got[i].CreatedAt, want[i].CreatedAt)
		}
	}
}

// The change function is handed what is stored and its return is what replaces
// it, so one call can add, remove and rewrite in the same breath.
func TestAnUpdateReplacesWhatTheChangeFunctionWasHanded(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "replace")

	if err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, "a.go", store.SideHead, epoch,
		keep(store.LineRange{Start: 1, End: 5}, store.LineRange{Start: 20, End: 25})); err != nil {
		t.Fatalf("writing the first set: %v", err)
	}

	var handed []store.LineRange
	err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, "a.go", store.SideHead, epoch,
		func(cur []store.LineRange) []store.LineRange {
			handed = cur
			return []store.LineRange{{Start: 20, End: 25}}
		})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}

	if len(handed) != 2 || handed[0] != (store.LineRange{Start: 1, End: 5}) {
		t.Errorf("the change function was handed %+v, want both stored ranges in start order", handed)
	}

	got := ranges(t, db, g)
	if len(got) != 1 || got[0].LineRange != (store.LineRange{Start: 20, End: 25}) {
		t.Errorf("ranges = %+v, want only 20:25", got)
	}
}

// Returning nothing is how the last mark on a file comes off, and it has to
// leave no row rather than the row it was handed.
func TestAnUpdateReturningNothingClearsTheFile(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "cleared")

	if err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, "a.go", store.SideHead, epoch,
		keep(store.LineRange{Start: 1, End: 5})); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, "a.go", store.SideHead, epoch, keep()); err != nil {
		t.Fatalf("clearing: %v", err)
	}

	if got := ranges(t, db, g); len(got) != 0 {
		t.Errorf("ranges = %+v, want none", got)
	}
}

// An update touches one file on one side and leaves every other key alone, or
// marking a.go would quietly unmark b.go.
func TestAnUpdateLeavesTheOtherKeysAlone(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "scoped")

	for _, w := range []struct {
		path string
		side store.Side
	}{{"a.go", store.SideHead}, {"a.go", store.SideBase}, {"b.go", store.SideHead}} {
		if err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, w.path, w.side, epoch,
			keep(store.LineRange{Start: 1, End: 5})); err != nil {
			t.Fatalf("writing %s %s: %v", w.path, w.side, err)
		}
	}

	if err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, "a.go", store.SideHead, epoch, keep()); err != nil {
		t.Fatalf("clearing a.go head: %v", err)
	}

	got := ranges(t, db, g)
	if len(got) != 2 {
		t.Fatalf("ranges = %+v, want the two keys that were not touched", got)
	}
	if got[0].Path != "a.go" || got[0].Side != store.SideBase || got[1].Path != "b.go" {
		t.Errorf("ranges = %+v, want a.go base and b.go head", got)
	}
}

// The whole reason the arithmetic runs inside the transaction. Read-then-write
// from above leaves two instances merging against the same pre-state and both
// inserting, and there is no UNIQUE constraint to catch it afterwards.
func TestConcurrentMarksOnOneFileAllSurvive(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "concurrent")

	const instances = 8

	var start sync.WaitGroup
	start.Add(1)

	var wg sync.WaitGroup
	errs := make([]error, instances)

	for i := range instances {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()

			// Each instance marks its own two lines, well clear of every other
			// so nothing merges and a lost write is a missing row.
			mine := store.LineRange{Start: i*10 + 1, End: i*10 + 2}
			errs[i] = db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, "a.go", store.SideHead, epoch,
				func(cur []store.LineRange) []store.LineRange { return append(cur, mine) })
		}()
	}
	start.Done()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("instance %d: %v", i, err)
		}
	}

	got := ranges(t, db, g)
	if len(got) != instances {
		t.Fatalf("got %d ranges, want one per instance: %+v", len(got), got)
	}
	for i, r := range got {
		want := store.LineRange{Start: i*10 + 1, End: i*10 + 2}
		if r.LineRange != want {
			t.Errorf("range %d = %+v, want %+v", i, r.LineRange, want)
		}
	}
}

// A generation whose carried ranges are missing reads as a review nobody did, so
// they land with it or neither does. Two files at one path break the primary key,
// which is the cheapest way to fail the write partway.
func TestAGenerationAndItsCarriedRangesLandTogetherOrNotAtAll(t *testing.T) {
	db := open(t)
	s := session(t, db, "carried")

	carry := store.Carry{Ranges: []store.ReviewedRange{
		{Path: "a.go", Side: store.SideHead, LineRange: store.LineRange{Start: 1, End: 5}, CreatedAt: epoch},
	}}

	first, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "one", CreatedAt: epoch,
	}, nil, carry)
	if err != nil {
		t.Fatalf("adding the first generation: %v", err)
	}

	got := ranges(t, db, first)
	if len(got) != 1 || got[0].Path != "a.go" || got[0].LineRange != (store.LineRange{Start: 1, End: 5}) {
		t.Fatalf("ranges = %+v, want the carried one stamped with this generation", got)
	}

	_, err = db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, []store.GenFile{{Path: "a.go"}, {Path: "a.go"}}, carry)
	if err == nil {
		t.Fatal("two files at one path should not write")
	}

	latest, _, err := db.LatestGeneration(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("reading the latest generation: %v", err)
	}
	if latest.CommitSha != "one" {
		t.Errorf("latest = %+v, want the first generation still standing", latest)
	}

	all, err := db.ReviewedRanges(t.Context(), latest.ID)
	if err != nil {
		t.Fatalf("reading the ranges back: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("ranges = %+v, want the rolled-back generation to have left none behind", all)
	}
}

// side carries a CHECK, so a value outside the vocabulary is a write that fails
// rather than a row every read has to allow for.
func TestASideOutsideTheVocabularyIsRefused(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "side")

	err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, "a.go", store.Side("both"), epoch,
		keep(store.LineRange{Start: 1, End: 1}))
	if err == nil {
		t.Fatal("a side outside the vocabulary should be refused")
	}
	if !strings.Contains(err.Error(), "a.go") {
		t.Errorf("err = %v, want it to name the file that failed", err)
	}
}
