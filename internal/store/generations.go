package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
)

type Generation struct {
	// ID and Seq are assigned by AddGeneration and ignored on the way in.
	ID  int64
	Seq int

	SessionID string

	BaseSha   string
	HeadSha   string
	CommitSha string

	CreatedAt time.Time
}

type GenFile struct {
	// GenerationID is ignored on the way in.
	GenerationID int64

	Path string

	// OldPath is set on a rename or a copy.
	OldPath string

	Status diff.Status

	// BaseBlob and HeadBlob are empty on the side the file does not exist on.
	// For an embedded repository they are that repository's commit, not an object in this one.
	BaseBlob string
	HeadBlob string

	// Cut is set from Carry.Cut and ignored on the way in.
	Cut bool
}

type Carry struct {
	Ranges []ReviewedRange

	// Cut is keyed by head-side path. A path the generation does not hold is ignored.
	Cut map[string]bool

	Comments []CommentMove
}

type Prior struct {
	Ranges   []ReviewedRange
	Files    []GenFile
	Comments []Comment
}

type Advance struct {
	// From is the generation carried out of, or 0 for a session with none.
	From int64

	// Carry runs inside the writing transaction and must not touch the database. Nil carries nothing and asserts nothing.
	Carry func(Prior) Carry
}

func (db *DB) LatestGeneration(ctx context.Context, sessionID string) (Generation, bool, error) {
	const q = `
		SELECT id, session_id, seq, base_sha, head_sha, commit_sha, created_at
		FROM generations
		WHERE session_id = ?
		ORDER BY seq DESC
		LIMIT 1`

	var g Generation
	var created string

	err := db.handle.QueryRowContext(ctx, q, sessionID).Scan(
		&g.ID, &g.SessionID, &g.Seq, &g.BaseSha, &g.HeadSha, &g.CommitSha, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, false, nil
	}
	if err != nil {
		return Generation{}, false, fmt.Errorf("reading the latest generation of %s: %w", sessionID, err)
	}
	if g.CreatedAt, err = moment(created); err != nil {
		return Generation{}, false, fmt.Errorf("reading the latest generation of %s: %w", sessionID, err)
	}
	return g, true, nil
}

// AddGeneration writes g, its files and the carried state in one transaction and returns g with ID and Seq set.
// Returns ErrStaleGeneration when adv.From is no longer the session's latest.
func (db *DB) AddGeneration(ctx context.Context, g Generation, files []GenFile, adv Advance) (_ Generation, err error) {
	tx, err := db.handle.BeginTx(ctx, nil)
	if err != nil {
		return Generation{}, fmt.Errorf("starting a generation for %s: %w", g.SessionID, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	carry, err := carried(ctx, tx, g.SessionID, adv)
	if err != nil {
		return Generation{}, err
	}

	const next = "SELECT COALESCE(MAX(seq), 0) + 1 FROM generations WHERE session_id = ?"
	if err = tx.QueryRowContext(ctx, next, g.SessionID).Scan(&g.Seq); err != nil {
		return Generation{}, fmt.Errorf("numbering a generation for %s: %w", g.SessionID, err)
	}

	const write = `
		INSERT INTO generations (session_id, seq, base_sha, head_sha, commit_sha, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, write,
		g.SessionID, g.Seq, g.BaseSha, g.HeadSha, g.CommitSha, stamp(g.CreatedAt),
	)
	if err != nil {
		return Generation{}, fmt.Errorf("writing generation %d of %s: %w", g.Seq, g.SessionID, err)
	}
	if g.ID, err = res.LastInsertId(); err != nil {
		return Generation{}, fmt.Errorf("writing generation %d of %s: %w", g.Seq, g.SessionID, err)
	}

	const writeFile = `
		INSERT INTO gen_files (generation_id, path, old_path, status, base_blob, head_blob, cut)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	for _, f := range files {
		_, err = tx.ExecContext(ctx, writeFile,
			g.ID, f.Path, f.OldPath, string(f.Status), f.BaseBlob, f.HeadBlob, carry.Cut[f.Path],
		)
		if err != nil {
			return Generation{}, fmt.Errorf("writing %s into generation %d of %s: %w", f.Path, g.Seq, g.SessionID, err)
		}
	}

	for _, r := range carry.Ranges {
		if err = insertRange(ctx, tx, g.SessionID, g.ID, r); err != nil {
			return Generation{}, err
		}
	}

	for _, m := range carry.Comments {
		if err = moveComment(ctx, tx, g.ID, g.CreatedAt, m); err != nil {
			return Generation{}, err
		}
	}

	if err = tx.Commit(); err != nil {
		return Generation{}, fmt.Errorf("committing generation %d of %s: %w", g.Seq, g.SessionID, err)
	}
	return g, nil
}

var ErrStaleGeneration = errors.New("the generation is no longer the session's latest")

func assertLatest(ctx context.Context, tx *sql.Tx, sessionID string, generationID int64) error {
	const q = "SELECT id FROM generations WHERE session_id = ? ORDER BY seq DESC LIMIT 1"

	var latest int64
	err := tx.QueryRowContext(ctx, q, sessionID).Scan(&latest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("reading the latest generation of %s: %w", sessionID, err)
	}
	if latest != generationID {
		return ErrStaleGeneration
	}
	return nil
}

// carried reads the prior state inside the writing transaction so a write committed during the git work is not read past.
func carried(ctx context.Context, tx *sql.Tx, sessionID string, adv Advance) (Carry, error) {
	if adv.Carry == nil {
		return Carry{}, nil
	}
	if err := assertLatest(ctx, tx, sessionID, adv.From); err != nil {
		return Carry{}, err
	}
	if adv.From == 0 {
		return adv.Carry(Prior{}), nil
	}

	var p Prior
	var err error
	if p.Ranges, err = reviewedRanges(ctx, tx, adv.From); err != nil {
		return Carry{}, err
	}
	if p.Files, err = genFiles(ctx, tx, adv.From); err != nil {
		return Carry{}, err
	}
	if p.Comments, err = openComments(ctx, tx, adv.From); err != nil {
		return Carry{}, err
	}
	return adv.Carry(p), nil
}

func (db *DB) GenFile(ctx context.Context, generationID int64, path string) (GenFile, bool, error) {
	const q = `
		SELECT generation_id, path, old_path, status, base_blob, head_blob, cut
		FROM gen_files
		WHERE generation_id = ? AND path = ?`

	var f GenFile
	err := db.handle.QueryRowContext(ctx, q, generationID, path).Scan(
		&f.GenerationID, &f.Path, &f.OldPath, &f.Status, &f.BaseBlob, &f.HeadBlob, &f.Cut,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return GenFile{}, false, nil
	}
	if err != nil {
		return GenFile{}, false, fmt.Errorf("reading %s from generation %d: %w", path, generationID, err)
	}
	return f, true, nil
}

// GenFiles returns a generation's files ordered by path.
func (db *DB) GenFiles(ctx context.Context, generationID int64) ([]GenFile, error) {
	return genFiles(ctx, db.handle, generationID)
}

func genFiles(ctx context.Context, q rower, generationID int64) ([]GenFile, error) {
	const read = `
		SELECT generation_id, path, old_path, status, base_blob, head_blob, cut
		FROM gen_files
		WHERE generation_id = ?
		ORDER BY path`

	rows, err := q.QueryContext(ctx, read, generationID)
	if err != nil {
		return nil, fmt.Errorf("reading the files of generation %d: %w", generationID, err)
	}
	defer func() { _ = rows.Close() }()

	var files []GenFile
	for rows.Next() {
		var f GenFile
		if err := rows.Scan(&f.GenerationID, &f.Path, &f.OldPath, &f.Status, &f.BaseBlob, &f.HeadBlob, &f.Cut); err != nil {
			return nil, fmt.Errorf("reading the files of generation %d: %w", generationID, err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the files of generation %d: %w", generationID, err)
	}
	return files, nil
}
