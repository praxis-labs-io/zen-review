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

// UpdateReviewedRanges replaces the ranges of one file at one generation with
// whatever change returns, reading and writing inside one transaction.
//
// The arithmetic belongs to the caller and the transaction does not. change runs
// between the read and the write, under _txlock=immediate, so two instances
// marking one file cannot both merge against the same pre-state and both insert.
// There is no UNIQUE constraint to catch that afterwards.
//
// change is handed the stored ranges in start order and returns the set to keep.
// Returning none leaves the file with none.
func (db *DB) UpdateReviewedRanges(
	ctx context.Context,
	sessionID string,
	generationID int64,
	path string,
	side Side,
	now time.Time,
	change func([]LineRange) []LineRange,
) (err error) {
	tx, err := db.handle.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("updating the reviewed ranges of %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const read = `
		SELECT start_line, end_line
		FROM reviewed_ranges
		WHERE generation_id = ? AND path = ? AND side = ?
		ORDER BY start_line`

	rows, err := tx.QueryContext(ctx, read, generationID, path, side)
	if err != nil {
		return fmt.Errorf("reading the reviewed ranges of %s: %w", path, err)
	}

	var current []LineRange
	for rows.Next() {
		var r LineRange
		if err = rows.Scan(&r.Start, &r.End); err != nil {
			_ = rows.Close()
			return fmt.Errorf("reading the reviewed ranges of %s: %w", path, err)
		}
		current = append(current, r)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("reading the reviewed ranges of %s: %w", path, err)
	}
	if err = rows.Close(); err != nil {
		return fmt.Errorf("reading the reviewed ranges of %s: %w", path, err)
	}

	const clear = "DELETE FROM reviewed_ranges WHERE generation_id = ? AND path = ? AND side = ?"
	if _, err = tx.ExecContext(ctx, clear, generationID, path, side); err != nil {
		return fmt.Errorf("clearing the reviewed ranges of %s: %w", path, err)
	}

	for _, r := range change(current) {
		if err = insertRange(ctx, tx, sessionID, generationID, ReviewedRange{
			Path:      path,
			Side:      side,
			LineRange: r,
			CreatedAt: now,
		}); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("updating the reviewed ranges of %s: %w", path, err)
	}
	return nil
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
