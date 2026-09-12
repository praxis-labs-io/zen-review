// Package store is the SQLite review database.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// params ride the DSN because the pool may redial, and busy_timeout, foreign_keys and _txlock are per-connection.
const params = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate"

const (
	// sqliteBusy is spelled out because the driver's constant lives in its generated C translation.
	sqliteBusy = 5

	openAttempts = 10
	openBackoff  = 20 * time.Millisecond
)

type DB struct {
	handle *sql.DB
}

type rower interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Open opens or creates the database at path, creating its directory, and migrates the schema. An unwritable path is an error.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("preparing the directory for %s: %w", path, err)
	}

	handle, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	handle.SetMaxOpenConns(1)

	if err := connect(ctx, handle); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	db := &DB{handle: handle}
	if err := db.migrate(ctx); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	if err := db.handle.Close(); err != nil {
		return fmt.Errorf("closing the database: %w", err)
	}
	return nil
}

// connect retries SQLITE_BUSY because the WAL switch on a new file takes a lock busy_timeout does not cover.
func connect(ctx context.Context, handle *sql.DB) error {
	var err error
	for attempt := 0; attempt < openAttempts; attempt++ {
		if err = handle.PingContext(ctx); err == nil {
			return nil
		}

		var serr *sqlite.Error
		if !errors.As(err, &serr) || serr.Code() != sqliteBusy {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(openBackoff):
		}
	}
	return err
}

// dsn escapes the path because the driver splits its parameters at the first '?', which a git directory may hold.
func dsn(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed, RawQuery: params}).String()
}

// stamp stores whole seconds because RFC3339Nano drops trailing zeros and then sorts out of order as text.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func moment(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading the timestamp %q: %w", s, err)
	}
	return t, nil
}
