package store_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/store"
)

// epoch is the one time every fixture is written at. The package holds no clock,
// so a pinned time here is what makes a round trip comparable field for field.
var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// open is a database in a temporary directory, closed when the test ends. The
// path carries a directory that does not exist yet, because Open creating it is
// what lets review point at $GIT_COMMON_DIR/zen-review without preparing it
// first.
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

// session is a saved session the generation tests can hang rows off.
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

// A session that is not there is an answer. Callers ask before they write, and
// an error would make the first run of every branch look like a failure.
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

// Resuming a session three days later is the same session, so the base and the
// summary move and the creation time does not.
func TestSavingASessionTwiceKeepsItsCreatedAt(t *testing.T) {
	db := open(t)
	first := session(t, db, "resumed")

	later := first
	later.BaseRef = "origin/develop"
	later.Summary = "picked it back up"
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
	if got.BaseRef != later.BaseRef || got.Summary != later.Summary {
		t.Errorf("session = %+v, want the base and summary from the second save", got)
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
		}, nil)
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

// A generation whose files are missing is one a remap would run through and find
// nothing in, so the two land together or neither does. Two files at one path
// break the primary key, which is the cheapest way to make the write fail
// partway.
func TestAGenerationAndItsFilesLandTogetherOrNotAtAll(t *testing.T) {
	db := open(t)
	s := session(t, db, "atomic")

	if _, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "one", CreatedAt: epoch,
	}, []store.GenFile{{Path: "a.go", Status: diff.FileModified}}); err != nil {
		t.Fatalf("adding the first generation: %v", err)
	}

	_, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "two", CreatedAt: epoch,
	}, []store.GenFile{
		{Path: "a.go", Status: diff.FileModified},
		{Path: "a.go", Status: diff.FileAdded},
	})
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

	// The rolled-back generation gave its number back, so the next one takes it
	// rather than leaving a gap the ref chain does not have.
	next, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: s.ID, BaseSha: "base", HeadSha: "head", CommitSha: "three", CreatedAt: epoch,
	}, nil)
	if err != nil {
		t.Fatalf("adding a generation after the failed one: %v", err)
	}
	if next.Seq != 2 {
		t.Errorf("seq = %d, want 2", next.Seq)
	}
}

// The foreign keys are only on because the DSN turned them on, and SQLite
// leaves them off by default. This is the check that proves the pragma survived.
func TestAGenerationWithoutItsSessionIsRefused(t *testing.T) {
	db := open(t)

	_, err := db.AddGeneration(t.Context(), store.Generation{
		SessionID: "never-saved", BaseSha: "base", HeadSha: "head", CommitSha: "commit", CreatedAt: epoch,
	}, nil)
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
	}, []store.GenFile{want[2], want[0], want[1]})
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

// The database is a file, and the whole point of it is that a review resumes
// days later. A second Open has to find the schema already there and leave it
// alone.
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

// Two instances opening one repository for the first time is the only moment
// they race: both find no schema, and both try to create it. The TUI and a
// subcommand launching together is enough to hit it, and losing meant zen-review
// refused to start.
//
// This is the test that has to run more than once. A single pass proves nothing
// about a window a few milliseconds wide.
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
			// Every one of them has to come away with a usable database, not just
			// an error-free Open.
			if _, _, err := db.Session(t.Context(), "any"); err != nil {
				t.Errorf("run %d: reading through a raced connection: %v", run, err)
			}
			if err := db.Close(); err != nil {
				t.Errorf("run %d: closing: %v", run, err)
			}
		}
	}
}

// .git not writable is a startup error, never a degraded mode where the review
// silently is not saved, so the line has to name the path that would not open.
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
