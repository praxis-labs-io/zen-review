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

var migrationName = regexp.MustCompile(`^(\d+)_[^/]+\.sql$`)

type migration struct {
	name    string
	version int
}

type versioned interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (db *DB) migrate(ctx context.Context) error {
	current, err := schemaVersion(ctx, db.handle)
	if err != nil {
		return err
	}

	files, err := embedded()
	if err != nil {
		return err
	}

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

func embedded() ([]migration, error) {
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

func schemaVersion(ctx context.Context, from versioned) (int, error) {
	var v int
	if err := from.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("reading the schema version: %w", err)
	}
	return v, nil
}

// apply re-reads the version under the write lock because two instances opening a new database both read 0.
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

	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return fmt.Errorf("recording the migration %s: %w", m.name, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing the migration %s: %w", m.name, err)
	}
	return nil
}
