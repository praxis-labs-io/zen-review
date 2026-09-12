package store_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func open(t *testing.T) *store.DB {
	t.Helper()

	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "zen-review", "state.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the database: %v", err)
		}
	})
	return db
}

func carrying(from store.Generation, c store.Carry) store.Advance {
	return store.Advance{From: from.ID, Carry: func(store.Prior) store.Carry { return c }}
}

func session(t *testing.T, db *store.DB, id string) store.Session {
	t.Helper()

	s := store.Session{
		ID:        id,
		RepoPath:  "/repo/.git",
		Kind:      store.KindBranch,
		Branch:    "feature/znr-11",
		BaseRef:   "origin/main",
		CreatedAt: epoch,
		UpdatedAt: epoch,
	}
	if err := db.SaveSession(t.Context(), s); err != nil {
		t.Fatalf("saving the session: %v", err)
	}
	return s
}

func TestASessionRoundTrips(t *testing.T) {
	db := open(t)

	want := store.Session{
		ID:        "0123456789abcdef",
		RepoPath:  "/repo/.git",
		Kind:      store.KindDetached,
		RangeSpec: "9f1c2d3",
		BaseRef:   "origin/main",
		Summary:   "the base moved twice",
		CreatedAt: epoch,
		UpdatedAt: epoch.Add(time.Hour),
	}
	if err := db.SaveSession(t.Context(), want); err != nil {
		t.Fatalf("saving the session: %v", err)
	}

	got, found, err := db.Session(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if !found {
		t.Fatal("the session was saved and did not come back")
	}
	if got != want {
		t.Errorf("session = %+v, want %+v", got, want)
	}
}

func TestAnUnknownSessionIsAbsenceRatherThanAnError(t *testing.T) {
	db := open(t)

	got, found, err := db.Session(t.Context(), "no-such-session")
	if err != nil {
		t.Fatalf("reading an unknown session: %v", err)
	}
	if found {
		t.Errorf("found = true for a session that was never written, got %+v", got)
	}
}

func TestSavingASessionTwiceKeepsItsCreatedAt(t *testing.T) {
	db := open(t)
	first := session(t, db, "resumed")

	later := first
	later.BaseRef = "origin/develop"
	later.CreatedAt = epoch.Add(72 * time.Hour)
	later.UpdatedAt = epoch.Add(72 * time.Hour)
	if err := db.SaveSession(t.Context(), later); err != nil {
		t.Fatalf("saving the session again: %v", err)
	}

	got, _, err := db.Session(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if !got.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("createdAt = %s, want it left at %s", got.CreatedAt, first.CreatedAt)
	}
	if !got.UpdatedAt.Equal(later.UpdatedAt) {
		t.Errorf("updatedAt = %s, want %s", got.UpdatedAt, later.UpdatedAt)
	}
	if got.BaseRef != later.BaseRef {
		t.Errorf("baseRef = %s, want the one from the second save", got.BaseRef)
	}
}

func TestSavingASessionLeavesTheSummaryAlone(t *testing.T) {
	db := open(t)
	first := session(t, db, "two-writers")

	if err := db.SetSessionSummary(t.Context(), first.ID, "held the store changes", epoch); err != nil {
		t.Fatalf("writing the summary: %v", err)
	}

	stale := first
	stale.BaseRef = "origin/develop"
	stale.UpdatedAt = epoch.Add(time.Hour)
	if err := db.SaveSession(t.Context(), stale); err != nil {
		t.Fatalf("saving the session again: %v", err)
	}

	got, _, err := db.Session(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if got.Summary != "held the store changes" {
		t.Errorf("summary = %q, want the one the other writer set", got.Summary)
	}
	if got.BaseRef != stale.BaseRef {
		t.Errorf("baseRef = %s, want the save to have moved it", got.BaseRef)
	}
}

func TestSettingTheSummaryLeavesTheBaseAlone(t *testing.T) {
	db := open(t)
	first := session(t, db, "noted")

	at := epoch.Add(time.Hour)
	if err := db.SetSessionSummary(t.Context(), first.ID, "picked it back up", at); err != nil {
		t.Fatalf("writing the summary: %v", err)
	}

	got, _, err := db.Session(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if got.Summary != "picked it back up" {
		t.Errorf("summary = %q, want the one just written", got.Summary)
	}
	if got.BaseRef != first.BaseRef {
		t.Errorf("baseRef = %s, want it left at %s", got.BaseRef, first.BaseRef)
	}
	if !got.UpdatedAt.Equal(at) {
		t.Errorf("updatedAt = %s, want %s", got.UpdatedAt, at)
	}
}

func TestSettingTheSummaryOfAnUnknownSessionFails(t *testing.T) {
	db := open(t)

	err := db.SetSessionSummary(t.Context(), "no-such-session", "anything", epoch)

	if err == nil {
		t.Fatal("writing the summary of a session that is not there should have failed")
	}
	if !strings.Contains(err.Error(), "no-such-session") {
		t.Errorf("err = %v, want it to name the session", err)
	}
}

func TestGenerationsAreNumberedAsTheyArrive(t *testing.T) {
	db := open(t)
	s := session(t, db, "numbered")

	for want := 1; want <= 3; want++ {
		got, err := db.AddGeneration(t.Context(), store.Generation{
			SessionID: s.ID,
			BaseSha:   "base",
			HeadSha:   "head",
			CommitSha: "commit",
			CreatedAt: epoch,
		}, nil, store.Advance{})
		if err != nil {
			t.Fatalf("adding generation %d: %v", want, err)
		}
		if got.Seq != want {
			t.Errorf("seq = %d, want %d", got.Seq, want)
		}
		if got.ID == 0 {
			t.Errorf("generation %d came back without an id", want)
		}
	}

	latest, found, err := db.LatestGeneration(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("reading the latest generation: %v", err)
	}
	if !found || latest.Seq != 3 {
		t.Errorf("latest = %+v, found = %v, want seq 3", latest, found)
	}
	if !latest.CreatedAt.Equal(epoch) {
		t.Errorf("createdAt = %s, want %s", latest.CreatedAt, epoch)
	}
}

func TestASessionWithNoGenerationsSaysSo(t *testing.T) {
	db := open(t)
	s := session(t, db, "fresh")

	_, found, err := db.LatestGeneration(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("reading the latest generation: %v", err)
	}
	if found {
		t.Error("found = true for a session with no generations")
	}
}

func TestAGenerationAndItsFilesLandTogetherOrNotAtAll(t *testing.T) {
	db := open(t)
	s := session(t, db, "atomic")

	if _, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "one", CreatedAt: epoch,
	}, []store.GenFile{{Path: "a.go", Status: diff.FileModified}}, store.Advance{}); err != nil {
		t.Fatalf("adding the first generation: %v", err)
	}

	_, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, []store.GenFile{
		{Path: "a.go", Status: diff.FileModified},
		{Path: "a.go", Status: diff.FileAdded},
	}, store.Advance{})
	if err == nil {
		t.Fatal("two files at one path should not write")
	}
	if !strings.Contains(err.Error(), "a.go") {
		t.Errorf("err = %v, want it to name the file that failed", err)
	}

	latest, _, err := db.LatestGeneration(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("reading the latest generation: %v", err)
	}
	if latest.CommitSha != "one" || latest.Seq != 1 {
		t.Errorf("latest = %+v, want the first generation still standing", latest)
	}

	next, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "three", CreatedAt: epoch,
	}, nil, store.Advance{})
	if err != nil {
		t.Fatalf("adding a generation after the failed one: %v", err)
	}
	if next.Seq != 2 {
		t.Errorf("seq = %d, want 2", next.Seq)
	}
}

func TestAGenerationWithoutItsSessionIsRefused(t *testing.T) {
	db := open(t)

	_, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: "never-saved", BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, nil, store.Advance{})
	if err == nil {
		t.Fatal("a generation for a session that does not exist should not write")
	}
}

func TestGenFilesComeBackOrderedByPath(t *testing.T) {
	db := open(t)
	s := session(t, db, "files")

	want := []store.GenFile{
		{Path: "a.go", Status: diff.FileModified, BaseBlob: "b1", HeadBlob: "h1"},
		{Path: "m.go", OldPath: "old.go", Status: diff.FileRenamed, BaseBlob: "b2", HeadBlob: "h2"},
		{Path: "z.go", Status: diff.FileAdded, HeadBlob: "h3"},
	}
	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, []store.GenFile{want[2], want[0], want[1]}, store.Advance{})
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	got, err := db.GenFiles(t.Context(), g.ID)
	if err != nil {
		t.Fatalf("reading the files: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d files, want %d", len(got), len(want))
	}
	for i := range want {
		want[i].GenerationID = g.ID
		if got[i] != want[i] {
			t.Errorf("file %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestACarriedCutLandsOnTheFileItNames(t *testing.T) {
	db := open(t)
	s := session(t, db, "cuts")

	files := []store.GenFile{
		{Path: "a.go", Status: diff.FileModified, BaseBlob: "b1", HeadBlob: "h1"},
		{Path: "z.go", Status: diff.FileModified, BaseBlob: "b2", HeadBlob: "h2"},
	}
	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, files, carrying(store.Generation{}, store.Carry{Cut: map[string]bool{"a.go": true, "gone.go": true}}))
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	got, err := db.GenFiles(t.Context(), g.ID)
	if err != nil {
		t.Fatalf("reading the files: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d files, want 2", len(got))
	}
	if !got[0].Cut {
		t.Errorf("a.go came back without the cut it was written with")
	}
	if got[1].Cut {
		t.Errorf("z.go came back cut, and nothing said it was")
	}

	one, found, err := db.GenFile(t.Context(), g.ID, "a.go")
	if err != nil || !found {
		t.Fatalf("reading a.go: found = %v, err = %v", found, err)
	}
	if !one.Cut {
		t.Error("a.go read one at a time came back without the cut")
	}
}

func TestAWriteSettlesOnlyTheCutItNames(t *testing.T) {
	db := open(t)
	s := session(t, db, "settling")

	files := []store.GenFile{
		{Path: "new.go", OldPath: "old.go", Status: diff.FileRenamed, BaseBlob: "b1", HeadBlob: "h1"},
		{Path: "z.go", Status: diff.FileModified, BaseBlob: "b2", HeadBlob: "h2"},
	}
	g, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, files, carrying(store.Generation{}, store.Carry{Cut: map[string]bool{"new.go": true, "z.go": true}}))
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	if err := writeSide(t.Context(), db, s.ID, g.ID, "old.go", store.SideBase, epoch, "new.go",
		keep(store.LineRange{Start: 1, End: 3})); err != nil {
		t.Fatalf("writing the base-side ranges: %v", err)
	}
	if err := writeSide(t.Context(), db, s.ID, g.ID, "z.go", store.SideHead, epoch, "",
		keep(store.LineRange{Start: 1, End: 3})); err != nil {
		t.Fatalf("writing z.go: %v", err)
	}

	got, err := db.GenFiles(t.Context(), g.ID)
	if err != nil {
		t.Fatalf("reading the files: %v", err)
	}
	if got[0].Cut {
		t.Errorf("new.go still holds a cut the write named")
	}
	if !got[1].Cut {
		t.Errorf("z.go lost a cut no write named")
	}
}

func TestReopeningFindsWhatWasWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen-review", "state.db")

	first, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	want := session(t, first, "resumable")
	if err := first.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}

	second, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopening the database: %v", err)
	}
	defer func() { _ = second.Close() }()

	got, found, err := second.Session(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if !found || got != want {
		t.Errorf("session = %+v, found = %v, want %+v", got, found, want)
	}
}

func TestOpeningANewDatabaseFromEveryDirectionAtOnce(t *testing.T) {
	for run := range 5 {
		path := filepath.Join(t.TempDir(), "zen-review", "state.db")

		var wg sync.WaitGroup
		opened := make([]*store.DB, 6)
		errs := make([]error, len(opened))
		for i := range opened {
			wg.Add(1)
			go func() {
				defer wg.Done()
				opened[i], errs[i] = store.Open(t.Context(), path)
			}()
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("run %d, opener %d: %v", run, i, err)
			}
		}
		for _, db := range opened {
			if db == nil {
				continue
			}
			if _, _, err := db.Session(t.Context(), "any"); err != nil {
				t.Errorf("run %d: reading through a raced connection: %v", run, err)
			}
			if err := db.Close(); err != nil {
				t.Errorf("run %d: closing: %v", run, err)
			}
		}
	}
}

func TestADatabaseOpensUnderAPathThatNeedsEscaping(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a question mark is not a legal path character on windows")
	}

	path := filepath.Join(t.TempDir(), "a dir? and more", "zen-review", "state.db")

	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening under an awkward path: %v", err)
	}
	want := session(t, db, "escaped")
	if err := db.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database is not at the path it was opened with: %v", err)
	}

	reopened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopening under an awkward path: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	got, found, err := reopened.Session(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if !found || got != want {
		t.Errorf("session = %+v, found = %v, want %+v", got, found, want)
	}
}

func TestTwoInstancesNumberGenerationsWithoutColliding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen-review", "state.db")

	first, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening the first instance: %v", err)
	}
	defer func() { _ = first.Close() }()

	second, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening the second instance: %v", err)
	}
	defer func() { _ = second.Close() }()

	s := session(t, first, "contended")

	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)

	seqs := make([]int, 2)
	errs := make([]error, 2)
	for i, db := range []*store.DB{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			g, err := db.AddGeneration(t.Context(), store.Generation{
				SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
			}, nil, store.Advance{})
			seqs[i], errs[i] = g.Seq, err
		}()
	}
	start.Done()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("instance %d: %v", i, err)
		}
	}
	if seqs[0] == seqs[1] {
		t.Fatalf("both instances took seq %d", seqs[0])
	}
	if seqs[0]+seqs[1] != 3 {
		t.Errorf("seqs = %v, want 1 and 2 in some order", seqs)
	}
}

func TestADatabaseThatCannotBeWrittenSaysWhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the permission bits do not bite on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory anyway")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("making the directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	path := filepath.Join(dir, "state.db")
	db, err := store.Open(t.Context(), path)
	if err == nil {
		_ = db.Close()
		t.Fatal("opening a database in a read-only directory should fail")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want it to name %s", err, path)
	}
}
