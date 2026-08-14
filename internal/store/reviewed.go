package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Side is which blob a reviewed range or a comment anchor is measured on.
//
// It is head except for a deletion-only hunk, which has no head-side lines and
// anchors to the base blob instead.
type Side string

const (
	SideHead Side = "head"
	SideBase Side = "base"
)

// LineRange is a closed interval of lines.
//
// A Start of 0 means the file as a whole rather than any line in it. That is how
// a file with no hunks is marked: a binary file, a mode change, a rename that
// moved nothing.
type LineRange struct {
	Start int
	End   int
}

// ReviewedRange is one run of lines somebody read, at one generation.
type ReviewedRange struct {
	Path string
	Side Side

	LineRange

	// CreatedAt is when the lines were read rather than when the row was
	// written, so it survives being carried into a later generation.
	CreatedAt time.Time
}

// ReviewedRanges is every range recorded against a generation, ordered by path,
// side and start line so a listing and a golden file get the same sequence
// without the caller sorting.
func (db *DB) ReviewedRanges(ctx context.Context, generationID int64) ([]ReviewedRange, error) {
	const q = `
		SELECT path, side, start_line, end_line, created_at
		FROM reviewed_ranges
		WHERE generation_id = ?
		ORDER BY path, side, start_line`

	rows, err := db.handle.QueryContext(ctx, q, generationID)
	if err != nil {
		return nil, fmt.Errorf("reading the reviewed ranges of generation %d: %w", generationID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ReviewedRange
	for rows.Next() {
		var r ReviewedRange
		var created string
		if err := rows.Scan(&r.Path, &r.Side, &r.Start, &r.End, &created); err != nil {
			return nil, fmt.Errorf("reading the reviewed ranges of generation %d: %w", generationID, err)
		}
		if r.CreatedAt, err = moment(created); err != nil {
			return nil, fmt.Errorf("reading the reviewed ranges of generation %d: %w", generationID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the reviewed ranges of generation %d: %w", generationID, err)
	}
	return out, nil
}

// SideChange is one file-and-side to rewrite and the arithmetic to rewrite it
// with.
//
// Path and Side both vary across a single write. A hunk that adds and removes is
// two sides of one file, and a base-side write stores under the name the file
// has on the base, which a rename makes a different one.
type SideChange struct {
	Path string
	Side Side

	// Change is handed the stored ranges in start order and returns the set to
	// keep. Returning none leaves that side with none. It runs holding the pool's
	// only connection, so it must not touch the database itself.
	Change func([]LineRange) []LineRange
}

// UpdateReviewedRanges replaces the ranges of one file at one generation with
// whatever each change returns, reading and writing inside one transaction.
//
// Every side goes in together. Reading a hunk means reading both of the sides it
// touches, so recording one without the other is not a smaller version of the
// same fact, and a caller told the write failed would be looking at half of it
// applied.
//
// The arithmetic belongs to the caller and the transaction does not. A change
// runs between the read and the write, under _txlock=immediate, so two instances
// marking one file cannot both merge against the same pre-state and both insert.
// There is no UNIQUE constraint to catch that afterwards.
//
// A returned range keeps the read time of the stored ranges it overlaps, oldest
// first, and takes now only where it covers lines nothing had marked. Stamping
// the whole set with now would make an unrelated mark on line 40 reset when line
// 5 was read.
//
// answers is the head-side path whose recorded cut this write settles, empty for
// a write that settles none. It goes in the same transaction as the ranges,
// because a cleared record beside ranges that never landed is a file reading as
// nobody's business.
//
// It is its own argument rather than a path off changes, because a base-side
// write stores under the file's base name and gen_files keys on its head one.
func (db *DB) UpdateReviewedRanges(
	ctx context.Context,
	sessionID string,
	generationID int64,
	now time.Time,
	answers string,
	changes []SideChange,
) (err error) {
	tx, err := db.handle.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("updating the reviewed ranges: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, c := range changes {
		if err = rewriteSide(ctx, tx, sessionID, generationID, now, c); err != nil {
			return err
		}
	}

	if answers != "" {
		const settle = "UPDATE gen_files SET cut = 0 WHERE generation_id = ? AND path = ?"
		if _, err = tx.ExecContext(ctx, settle, generationID, answers); err != nil {
			return fmt.Errorf("clearing the cut recorded against %s: %w", answers, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("updating the reviewed ranges: %w", err)
	}
	return nil
}

// rewriteSide replaces one file-and-side's ranges inside the caller's
// transaction: read what is there, hand it to the arithmetic, write back what
// comes out.
func rewriteSide(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	generationID int64,
	now time.Time,
	c SideChange,
) error {
	const read = `
		SELECT start_line, end_line, created_at
		FROM reviewed_ranges
		WHERE generation_id = ? AND path = ? AND side = ?
		ORDER BY start_line`

	rows, err := tx.QueryContext(ctx, read, generationID, c.Path, c.Side)
	if err != nil {
		return fmt.Errorf("reading the reviewed ranges of %s: %w", c.Path, err)
	}

	var current []ReviewedRange
	for rows.Next() {
		var r ReviewedRange
		var created string
		if err = rows.Scan(&r.Start, &r.End, &created); err != nil {
			_ = rows.Close()
			return fmt.Errorf("reading the reviewed ranges of %s: %w", c.Path, err)
		}
		if r.CreatedAt, err = moment(created); err != nil {
			_ = rows.Close()
			return fmt.Errorf("reading the reviewed ranges of %s: %w", c.Path, err)
		}
		current = append(current, r)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("reading the reviewed ranges of %s: %w", c.Path, err)
	}
	if err = rows.Close(); err != nil {
		return fmt.Errorf("reading the reviewed ranges of %s: %w", c.Path, err)
	}

	const clear = "DELETE FROM reviewed_ranges WHERE generation_id = ? AND path = ? AND side = ?"
	if _, err = tx.ExecContext(ctx, clear, generationID, c.Path, c.Side); err != nil {
		return fmt.Errorf("clearing the reviewed ranges of %s: %w", c.Path, err)
	}

	for _, r := range c.Change(lines(current)) {
		if err = insertRange(ctx, tx, sessionID, generationID, ReviewedRange{
			Path:      c.Path,
			Side:      c.Side,
			LineRange: r,
			CreatedAt: readAt(current, r, now),
		}); err != nil {
			return err
		}
	}
	return nil
}

// lines is what the change function sees, which is the arithmetic and not the
// bookkeeping around it.
func lines(rs []ReviewedRange) []LineRange {
	out := make([]LineRange, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.LineRange)
	}
	return out
}

// readAt is when the lines in r were read: the oldest stamp among the stored
// ranges it overlaps, and now for a range covering lines nothing had marked.
//
// Splitting and merging leave no row on the far side to carry one row's own
// stamp onto, so the oldest is the honest answer to how long these lines have
// been read.
func readAt(current []ReviewedRange, r LineRange, now time.Time) time.Time {
	at := now
	for _, c := range current {
		if c.Start > r.End || c.End < r.Start {
			continue
		}
		if c.CreatedAt.Before(at) {
			at = c.CreatedAt
		}
	}
	return at
}

// insertRange writes one row. It takes the transaction rather than the pool
// because both callers are mid-transaction: a mark, and a generation carrying
// its predecessor's state in.
func insertRange(ctx context.Context, tx *sql.Tx, sessionID string, generationID int64, r ReviewedRange) error {
	const write = `
		INSERT INTO reviewed_ranges (session_id, generation_id, path, side, start_line, end_line, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := tx.ExecContext(ctx, write, sessionID, generationID, r.Path, string(r.Side), r.Start, r.End, stamp(r.CreatedAt))
	if err != nil {
		return fmt.Errorf("writing the reviewed range %d:%d of %s: %w", r.Start, r.End, r.Path, err)
	}
	return nil
}
