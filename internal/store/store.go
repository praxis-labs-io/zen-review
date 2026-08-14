// Package store is the review database: SQLite, its migrations, and the row
// types every layer above reads.
//
// It is the only package that imports database/sql. Nothing above it sees a
// connection, a transaction or a driver name, so there is one place that knows
// how review state is written down.
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

	// modernc's driver is pure Go. The cgo one is faster, but it puts a C
	// toolchain in the path of every cross-compile and CI runner for a few
	// thousand rows.
	"modernc.org/sqlite"
)

// params are the pragmas every connection opens with.
//
// They ride the DSN rather than an Exec afterwards because two of the three are
// per-connection, and database/sql may retire a connection and dial a new one
// whenever it likes. A pragma set once against a pool is a pragma that silently
// stops applying.
//
// journal_mode is a property of the file and outlives the connection. The other
// two do not: busy_timeout is what stops two instances on one repository from
// failing each other's writes outright, and _txlock=immediate takes the write
// lock at BEGIN so a transaction that reads before it writes cannot deadlock
// against another one doing the same.
const params = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate"

// Creating the file is the one moment two instances collide. Switching a
// database to WAL takes a lock the busy timeout does not cover, so an opener
// arriving while another is still creating the file is told the database is
// locked outright rather than made to wait. The window is one journal_mode
// switch long, and once the file is in WAL there is nothing left to contend
// for: repeated opens of an existing database never hit this.
const (
	// sqliteBusy is SQLITE_BUSY. It is spelled out rather than imported, because
	// the constant lives in the driver's generated C translation and pulling that
	// package in for one integer is a poor trade.
	sqliteBusy = 5

	openAttempts = 10
	openBackoff  = 20 * time.Millisecond
)

// DB is the open review database.
type DB struct {
	handle *sql.DB
}

// rower is what the pool and a transaction have in common. The listing queries
// run both ways: on their own for a reader, and inside the transaction that
// writes a generation, which is where they have to run to be sure of what they
// read.
type rower interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Open opens the database at path, creating it and its directory if they are
// not there, and brings the schema up to date.
//
// A path that cannot be written is an error rather than a degraded mode. A
// review that is silently not being saved is worse than one that refuses to
// start.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("preparing the directory for %s: %w", path, err)
	}

	handle, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	// One connection. There is no concurrency here to win, and a pool turns
	// per-connection state into something every query has to reason about.
	handle.SetMaxOpenConns(1)

	// sql.Open dials nothing, so an unwritable directory would otherwise surface
	// as a failed migration rather than as a database that would not open.
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

// Close releases the connection.
func (db *DB) Close() error {
	if err := db.handle.Close(); err != nil {
		return fmt.Errorf("closing the database: %w", err)
	}
	return nil
}

// connect dials the database, waiting out another instance that is busy
// creating it.
//
// Every attempt takes a new connection, so each one re-runs the pragmas and
// gets its own go at the WAL switch. Anything that is not SQLITE_BUSY is
// returned as it arrived: an unwritable directory does not become better by
// being asked ten times.
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

// dsn builds the file: URI the driver opens.
//
// The path goes through url.URL rather than being concatenated, because the
// driver splits its own parameters off at the first '?' it finds and a git
// directory is allowed to hold one. Escaping is what keeps such a path from
// becoming a truncated path and a nonsense pragma.
//
// Windows arrives as C:\repo\.git and has to leave as /C:/repo/.git, which is
// the form SQLite's URI parser expects.
func dsn(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed, RawQuery: params}).String()
}

// stamp writes a time the way every timestamp column holds it: RFC3339 in UTC,
// so sqlite3 on the file reads without a decoder.
//
// **Timestamps are stored to the second.** A time.Now() saved and read back
// comes home truncated, and comparing the two with == finds them different. The
// fractional form is not used on purpose: RFC3339Nano drops trailing zeros, so
// the text is no longer fixed width and ".5Z" sorts before "Z", which puts a
// later timestamp ahead of an earlier one under SQLite's text collation.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// moment reads a stamp back. A column that will not parse is a corrupt row
// rather than a zero time to carry forward.
func moment(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading the timestamp %q: %w", s, err)
	}
	return t, nil
}
