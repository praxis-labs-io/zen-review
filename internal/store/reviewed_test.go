package store_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func generation(t *testing.T, db *store.DB, id string) (store.Session, store.Generation) {
	t.Helper()

	s := session(t, db, id)
	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, nil, store.Advance{})
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}
	return s, g
}

func keep(rs ...store.LineRange) func([]store.LineRange) []store.LineRange {
	return func([]store.LineRange) []store.LineRange { return rs }
}

func writeSide(
	ctx context.Context,
	db *store.DB,
	sessionID string,
	generationID int64,
	path string,
	side store.Side,
	now time.Time,
	answers string,
	change func([]store.LineRange) []store.LineRange,
) error {
	return db.UpdateReviewedRanges(ctx, sessionID, generationID, now, answers,
		[]store.SideChange{{Path: path, Side: side, Change: change}})
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
		if err := writeSide(t.Context(), db, s.ID, g.ID, w.path, w.side, epoch, "", keep(w.ranges...)); err != nil {
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

func TestAnUpdateReplacesWhatTheChangeFunctionWasHanded(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "replace")

	if err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, epoch, "",
		keep(store.LineRange{Start: 1, End: 5}, store.LineRange{Start: 20, End: 25})); err != nil {
		t.Fatalf("writing the first set: %v", err)
	}

	var handed []store.LineRange
	err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, epoch, "",
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

func TestAnUpdateReturningNothingClearsTheFile(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "cleared")

	if err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, epoch, "",
		keep(store.LineRange{Start: 1, End: 5})); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, epoch, "", keep()); err != nil {
		t.Fatalf("clearing: %v", err)
	}

	if got := ranges(t, db, g); len(got) != 0 {
		t.Errorf("ranges = %+v, want none", got)
	}
}

func TestAnUpdateLeavesTheOtherKeysAlone(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "scoped")

	for _, w := range []struct {
		path string
		side store.Side
	}{{"a.go", store.SideHead}, {"a.go", store.SideBase}, {"b.go", store.SideHead}} {
		if err := writeSide(t.Context(), db, s.ID, g.ID, w.path, w.side, epoch, "",
			keep(store.LineRange{Start: 1, End: 5})); err != nil {
			t.Fatalf("writing %s %s: %v", w.path, w.side, err)
		}
	}

	if err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, epoch, "", keep()); err != nil {
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

func TestTwoInstancesMarkingOneFileBothSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen-review", "state.db")

	const instances = 4

	dbs := make([]*store.DB, instances)
	for i := range dbs {
		db, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatalf("opening instance %d: %v", i, err)
		}
		defer func() { _ = db.Close() }()
		dbs[i] = db
	}

	s, g := generation(t, dbs[0], "concurrent")

	var start sync.WaitGroup
	start.Add(1)

	var wg sync.WaitGroup
	errs := make([]error, instances)

	for i, db := range dbs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()

			mine := store.LineRange{Start: i*10 + 1, End: i*10 + 2}
			errs[i] = writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, epoch, "",
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

	got := ranges(t, dbs[0], g)
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

func TestGenFileFindsOnePathAndSaysWhenItIsNotThere(t *testing.T) {
	db := open(t)
	s := session(t, db, "onefile")

	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, []store.GenFile{
		{Path: "a.go", Status: diff.FileModified},
		{Path: "new.go", OldPath: "old.go", Status: diff.FileRenamed},
	}, store.Advance{})
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	got, found, err := db.GenFile(t.Context(), g.ID, "new.go")
	if err != nil {
		t.Fatalf("reading new.go: %v", err)
	}
	if !found || got.OldPath != "old.go" || got.Status != diff.FileRenamed {
		t.Errorf("file = %+v, found = %v, want the rename off old.go", got, found)
	}

	if _, found, err = db.GenFile(t.Context(), g.ID, "old.go"); err != nil {
		t.Fatalf("reading old.go: %v", err)
	}
	if found {
		t.Error("found = true for a path only the old side has")
	}
}

func TestAMarkKeepsTheReadTimeOfTheRangesItDoesNotTouch(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "stamps")

	monday := epoch
	friday := epoch.Add(96 * time.Hour)

	if err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, monday, "",
		keep(store.LineRange{Start: 5, End: 9})); err != nil {
		t.Fatalf("marking on monday: %v", err)
	}
	if err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, friday, "",
		keep(store.LineRange{Start: 5, End: 9}, store.LineRange{Start: 40, End: 44})); err != nil {
		t.Fatalf("marking on friday: %v", err)
	}

	want := []time.Time{monday, friday}
	got := ranges(t, db, g)
	if len(got) != len(want) {
		t.Fatalf("got %d ranges, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].CreatedAt.Equal(want[i]) {
			t.Errorf("range %+v was read at %s, want %s", got[i].LineRange, got[i].CreatedAt, want[i])
		}
	}

	if err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.SideHead, friday, "",
		keep(store.LineRange{Start: 1, End: 50})); err != nil {
		t.Fatalf("widening: %v", err)
	}
	if got := ranges(t, db, g); len(got) != 1 || !got[0].CreatedAt.Equal(monday) {
		t.Errorf("ranges = %+v, want one read on monday", got)
	}
}

func TestAGenerationAndItsCarriedRangesLandTogetherOrNotAtAll(t *testing.T) {
	db := open(t)
	s := session(t, db, "carried")

	rows := store.Carry{Ranges: []store.ReviewedRange{
		{Path: "a.go", Side: store.SideHead, LineRange: store.LineRange{Start: 1, End: 5}, CreatedAt: epoch},
	}}

	first, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "one", CreatedAt: epoch,
	}, nil, carrying(store.Generation{}, rows))
	if err != nil {
		t.Fatalf("adding the first generation: %v", err)
	}

	got := ranges(t, db, first)
	if len(got) != 1 || got[0].Path != "a.go" || got[0].LineRange != (store.LineRange{Start: 1, End: 5}) {
		t.Fatalf("ranges = %+v, want the carried one stamped with this generation", got)
	}

	_, err = db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, []store.GenFile{{Path: "a.go"}, {Path: "a.go"}}, carrying(first, rows))
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

func TestEverySideOfOneWriteLandsOrNoneDoes(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "sides")

	err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, epoch, "", []store.SideChange{
		{Path: "a.go", Side: store.SideHead, Change: keep(store.LineRange{Start: 1, End: 5})},
		{Path: "a.go", Side: store.Side("neither"), Change: keep(store.LineRange{Start: 1, End: 5})},
	})
	if err == nil {
		t.Fatal("a side outside the vocabulary should be refused")
	}

	if got := ranges(t, db, g); len(got) != 0 {
		t.Errorf("ranges = %+v, want none: the side that failed took the one before it with it", got)
	}
}

func TestAWriteCoveringTwoSidesRecordsBoth(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "both")

	if err := db.UpdateReviewedRanges(t.Context(), s.ID, g.ID, epoch, "", []store.SideChange{
		{Path: "new.go", Side: store.SideHead, Change: keep(store.LineRange{Start: 10, End: 12})},
		{Path: "old.go", Side: store.SideBase, Change: keep(store.LineRange{Start: 4, End: 4})},
	}); err != nil {
		t.Fatalf("writing both sides: %v", err)
	}

	got := ranges(t, db, g)
	if len(got) != 2 {
		t.Fatalf("ranges = %+v, want one row per side", got)
	}
	if got[0].Path != "new.go" || got[0].Side != store.SideHead {
		t.Errorf("range 0 = %+v, want new.go on the head", got[0])
	}
	if got[1].Path != "old.go" || got[1].Side != store.SideBase {
		t.Errorf("range 1 = %+v, want old.go on the base", got[1])
	}
}

func TestASideOutsideTheVocabularyIsRefused(t *testing.T) {
	db := open(t)
	s, g := generation(t, db, "side")

	err := writeSide(t.Context(), db, s.ID, g.ID, "a.go", store.Side("both"), epoch, "", keep(store.LineRange{Start: 1, End: 1}))
	if err == nil {
		t.Fatal("a side outside the vocabulary should be refused")
	}
	if !strings.Contains(err.Error(), "a.go") {
		t.Errorf("err = %v, want it to name the file that failed", err)
	}
}
