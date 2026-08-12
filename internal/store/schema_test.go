package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
)

// These tests are inside the package because what they check has no public
// surface. The pragmas and the schema version are what every promise above them
// rests on, and a test that could only reach them through the API would be
// asserting something else.

func openHere(t *testing.T) *DB {
	t.Helper()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "zen-review", "state.db"))
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

func TestTheConnectionCarriesItsPragmas(t *testing.T) {
	db := openHere(t)

	// WAL is what lets two instances on one repository read while one writes, and
	// the busy timeout is what keeps the one that arrives second from failing
	// outright. foreign_keys is off by default in SQLite, so every cascade in the
	// schema is only real because of this line.
	for _, tc := range []struct{ pragma, want string }{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
	} {
		var got string
		if err := db.handle.QueryRowContext(t.Context(), "PRAGMA "+tc.pragma).Scan(&got); err != nil {
			t.Fatalf("reading %s: %v", tc.pragma, err)
		}
		if got != tc.want {
			t.Errorf("%s = %s, want %s", tc.pragma, got, tc.want)
		}
	}
}

func TestTheSchemaVersionMatchesTheLastMigration(t *testing.T) {
	db := openHere(t)

	files, err := embedded()
	if err != nil {
		t.Fatalf("reading the migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migrations are embedded")
	}
	last := files[len(files)-1]

	got, err := schemaVersion(t.Context(), db.handle)
	if err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if got != last.version {
		t.Errorf("user_version = %d, want %d from %s", got, last.version, last.name)
	}
}

// The embedded files sort as text and apply in that order, so a number that is
// not zero-padded to the same width silently reorders the run. 10 lands before
// 2, the version recorded makes 2 look already applied, and it never runs. The
// check is against the parsed list rather than the real embed.FS, because there
// is one migration today and the failure has to be catchable before a second one
// is written badly.
func TestMigrationsWhoseNumbersDoNotClimbAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []migration
		ok    bool
	}{
		{
			name:  "padded to one width",
			files: []migration{{name: "0001_init.sql", version: 1}, {name: "0002_next.sql", version: 2}},
			ok:    true,
		},
		{
			name:  "unpadded, so ten sorts before two",
			files: []migration{{name: "1_init.sql", version: 1}, {name: "10_next.sql", version: 10}, {name: "2_later.sql", version: 2}},
		},
		{
			name:  "two files claiming one version",
			files: []migration{{name: "0001_init.sql", version: 1}, {name: "0001_again.sql", version: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ordered(tc.files)
			if tc.ok {
				if err != nil {
					t.Fatalf("ordered = %v, want it accepted", err)
				}
				return
			}
			if err == nil {
				t.Fatal("the migrations should have been refused")
			}
			if !strings.Contains(err.Error(), "out of order") {
				t.Errorf("err = %v, want it to say what is wrong", err)
			}
		})
	}
}

// A database a newer build migrated is refused rather than half read. The
// worktree case is real: one checkout on a released binary, another on a build
// from source, both against the database the common dir holds.
func TestADatabaseFromANewerBuildIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen-review", "state.db")

	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	files, err := embedded()
	if err != nil {
		t.Fatalf("reading the migrations: %v", err)
	}
	ahead := files[len(files)-1].version + 1
	if _, err := db.handle.ExecContext(t.Context(), fmt.Sprintf("PRAGMA user_version = %d", ahead)); err != nil {
		t.Fatalf("moving the schema version ahead: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}

	reopened, err := Open(t.Context(), path)
	if err == nil {
		_ = reopened.Close()
		t.Fatal("a database from a newer build should not open")
	}
	if !strings.Contains(err.Error(), "upgrade zen-review") {
		t.Errorf("err = %v, want it to say what to do about it", err)
	}
}

// Running migrate twice has to be a no-op. Every command opens the database, so
// this is the path taken far more often than the first one.
func TestMigratingAnUpToDateDatabaseChangesNothing(t *testing.T) {
	db := openHere(t)

	var before int
	if err := db.handle.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&before); err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if err := db.migrate(t.Context()); err != nil {
		t.Fatalf("migrating an up-to-date database: %v", err)
	}

	var after int
	if err := db.handle.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&after); err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if after != before {
		t.Errorf("user_version moved from %d to %d", before, after)
	}
}

// The spec's five tables and no more. A sixth is a design decision, and it
// should not be able to arrive without this line changing.
func TestTheSchemaIsTheFiveTablesTheSpecNames(t *testing.T) {
	db := openHere(t)

	rows, err := db.handle.QueryContext(t.Context(),
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("listing the tables: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading a table name: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("listing the tables: %v", err)
	}

	want := []string{"comments", "gen_files", "generations", "reviewed_ranges", "sessions"}
	if len(got) != len(want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables = %v, want %v", got, want)
		}
	}
}

// The vocabulary is closed, so a status outside it is a bug above this layer
// rather than a row to keep. The gitlink was the case that held this constraint
// back, and it needs no value of its own: git records an embedded repository as
// a mode 160000 entry that diffs as an ordinary added, modified or deleted file.
func TestGenFileStatusIsConstrainedToTheVocabulary(t *testing.T) {
	db := openHere(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	if err := db.SaveSession(ctx, Session{ID: "s", RepoPath: "/repo", Kind: KindBranch, Branch: "main", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("saving the session: %v", err)
	}

	add := func(status diff.Status) error {
		_, err := db.AddGeneration(ctx,
			Generation{SessionID: "s", BaseSha: "b", HeadSha: "h", CommitSha: "c", CreatedAt: now},
			[]GenFile{{Path: "a.txt", Status: status}}, Carry{})
		return err
	}

	for _, status := range []diff.Status{diff.FileAdded, diff.FileModified, diff.FileDeleted, diff.FileRenamed, diff.FileCopied} {
		if err := add(status); err != nil {
			t.Errorf("status %q was refused: %v", status, err)
		}
	}
	if err := add(diff.Status("submodule")); err == nil {
		t.Error("a status outside the vocabulary should be refused")
	}
}
