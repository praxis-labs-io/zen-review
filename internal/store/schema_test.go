package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
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
