package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
)

//go:embed migrations/*.sql
var migrations embed.FS

// migrationName is the prefix every migration file carries. The number in it is
// what user_version holds, so the order the files apply in and the version
// recorded are one fact rather than two that can disagree.
var migrationName = regexp.MustCompile(`^(\d+)_[^/]+\.sql$`)

// migration is one embedded file and the version it brings the database to.
type migration struct {
	name    string
	version int
}

// versioned is what reading the schema version needs, so it can be read from a
// database or from inside a transaction with one function.
type versioned interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// migrate applies every migration numbered above the recorded user_version.
//
// Each file runs in its own transaction ending with the version bump, so an
// interrupted upgrade leaves the database on the last version that applied
// whole rather than partway through one.
func (db *DB) migrate(ctx context.Context) error {
	current, err := schemaVersion(ctx, db.handle)
	if err != nil {
		return err
	}

	files, err := embedded()
	if err != nil {
		return err
	}

	// A database a newer build migrated holds columns this one does not know
	// about, and may be missing ones it reads. Saying so is the same call as
	// refusing an unwritable directory: a review that half works is worse than
	// one that will not start.
	if last := len(files) - 1; last >= 0 && current > files[last].version {
		return fmt.Errorf(
			"the database is at schema version %d and this build knows %d: upgrade zen-review",
			current, files[last].version)
	}

	for _, m := range files {
		if m.version <= current {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("reading the migration %s: %w", m.name, err)
		}
		if err := db.apply(ctx, m, string(body)); err != nil {
			return err
		}
	}
	return nil
}

// embedded is every migration file, in the order they apply.
func embedded() ([]migration, error) {
	// fs.ReadDir sorts by filename, which for a zero-padded prefix is that order.
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading the migrations: %w", err)
	}

	files := make([]migration, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		match := migrationName.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("the migration %s is not named <number>_<what>.sql", name)
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("reading the number of the migration %s: %w", name, err)
		}
		files = append(files, migration{name: name, version: version})
	}

	if err := ordered(files); err != nil {
		return nil, err
	}
	return files, nil
}

// ordered rejects a set of migrations whose numbers do not climb.
//
// The names sort as text, and text order is not number order: 10 comes before 2.
// Applying them in that order records version 10, which makes 2 look already
// done, and it is skipped with nothing said. Requiring each number to be larger
// than the last catches that and rejects two files claiming one version with the
// same line.
func ordered(files []migration) error {
	for i := 1; i < len(files); i++ {
		if files[i].version <= files[i-1].version {
			return fmt.Errorf(
				"the migrations are out of order: %s follows %s, so pad the numbers to the same width",
				files[i].name, files[i-1].name)
		}
	}
	return nil
}

// schemaVersion is the version recorded in the database file.
func schemaVersion(ctx context.Context, from versioned) (int, error) {
	var v int
	if err := from.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("reading the schema version: %w", err)
	}
	return v, nil
}

// apply runs one migration and records its number.
func (db *DB) apply(ctx context.Context, m migration, body string) (err error) {
	tx, err := db.handle.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting the migration %s: %w", m.name, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// The version is read again here, because the read that chose this migration
	// happened before the write lock was held. Two instances opening one
	// repository for the first time both see 0, and the one that arrives second
	// would otherwise run CREATE TABLE against tables the first already made.
	current, err := schemaVersion(ctx, tx)
	if err != nil {
		return err
	}
	if current >= m.version {
		if done := tx.Rollback(); done != nil {
			return fmt.Errorf("ending the skipped migration %s: %w", m.name, done)
		}
		return nil
	}

	if _, err = tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("applying the migration %s: %w", m.name, err)
	}

	// user_version takes no bind parameter. The number came off a filename the
	// expression above already matched as digits, so there is nothing to inject.
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return fmt.Errorf("recording the migration %s: %w", m.name, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing the migration %s: %w", m.name, err)
	}
	return nil
}
