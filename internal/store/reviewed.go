package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Side is the blob a range is measured on: head, except base for a deletion-only hunk.
type Side string

const (
	SideHead Side = "head"
	SideBase Side = "base"
)

// LineRange is a closed interval of lines. A Start of 0 means the whole file.
type LineRange struct {
	Start int
	End   int
}

type ReviewedRange struct {
	Path string
	Side Side

	LineRange

	// CreatedAt is when the lines were read, and survives being carried into a later generation.
	CreatedAt time.Time
}

// ReviewedRanges returns a generation's ranges ordered by path, side and start line.
func (db *DB) ReviewedRanges(ctx context.Context, generationID int64) ([]ReviewedRange, error) {
	return reviewedRanges(ctx, db.handle, generationID)
}

func reviewedRanges(ctx context.Context, q rower, generationID int64) ([]ReviewedRange, error) {
	const read = `
		SELECT path, side, start_line, end_line, created_at
		FROM reviewed_ranges
		WHERE generation_id = ?
		ORDER BY path, side, start_line`

	rows, err := q.QueryContext(ctx, read, generationID)
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

type SideChange struct {
	Path string
	Side Side

	// Change takes the stored ranges in start order and returns the set to keep. It must not touch the database.
	Change func([]LineRange) []LineRange
}

// UpdateReviewedRanges applies every change, and clears the cut on the head-side path answers unless empty, in one transaction.
// A kept range takes the oldest read time it overlaps, or now. Returns ErrStaleGeneration when generationID is not the latest.
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

	if err = assertLatest(ctx, tx, sessionID, generationID); err != nil {
		return err
	}

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

func lines(rs []ReviewedRange) []LineRange {
	out := make([]LineRange, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.LineRange)
	}
	return out
}

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
