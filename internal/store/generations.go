package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
)

// Generation is one snapshot of the changeset, written into git as a real
// commit under the session's ref.
type Generation struct {
	// ID and Seq are assigned by AddGeneration and ignored on the way in.
	ID  int64
	Seq int

	SessionID string

	// BaseSha is the merge base the changeset was measured from, HeadSha the
	// branch tip at the time, and CommitSha the generation commit itself.
	BaseSha   string
	HeadSha   string
	CommitSha string

	// CreatedAt is stored to the second, as on Session.
	CreatedAt time.Time
}

// GenFile is one file in the changeset at one generation. Its blob shas are
// real objects, because the generation commit wrote them.
type GenFile struct {
	// GenerationID is ignored on the way in, because AddGeneration writes the
	// generation it just numbered. GenFiles sets it on what it reads back.
	GenerationID int64

	Path string

	// OldPath is set on a rename or a copy.
	OldPath string

	Status diff.Status

	// BaseBlob and HeadBlob are empty on the side the file does not exist on.
	//
	// An embedded repository is the exception to their name: git records one as a
	// mode 160000 gitlink, so the sha here is that repository's commit and is not
	// an object in this one. It is kept because it is the identity that changed,
	// and a caller resolving these has to allow for one that does not.
	BaseBlob string
	HeadBlob string
}

// Carry is the review state moved into a generation as it is written.
//
// Its rows arrive without a generation id, because AddGeneration stamps the one
// it just numbered. It is a struct rather than a slice so the comments that
// travel the same way have somewhere to go.
type Carry struct {
	Ranges []ReviewedRange
}

// LatestGeneration is the highest-numbered generation of a session. A session
// with none comes back as (Generation{}, false, nil).
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

// AddGeneration writes a generation, its files and the review state carried
// into it together, and returns it with ID and Seq filled in.
//
// All three go in one transaction. A generation whose files are missing is one a
// remap would run through and find nothing in, and one whose carried ranges are
// missing reads as a review nobody did. Seq is assigned here rather than by the
// caller: _txlock=immediate takes the write lock at BEGIN, so reading the
// previous number and writing the next cannot interleave with another instance
// doing the same.
func (db *DB) AddGeneration(ctx context.Context, g Generation, files []GenFile, carry Carry) (_ Generation, err error) {
	tx, err := db.handle.BeginTx(ctx, nil)
	if err != nil {
		return Generation{}, fmt.Errorf("starting a generation for %s: %w", g.SessionID, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

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
		INSERT INTO gen_files (generation_id, path, old_path, status, base_blob, head_blob)
		VALUES (?, ?, ?, ?, ?, ?)`

	for _, f := range files {
		_, err = tx.ExecContext(ctx, writeFile,
			g.ID, f.Path, f.OldPath, string(f.Status), f.BaseBlob, f.HeadBlob,
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

	if err = tx.Commit(); err != nil {
		return Generation{}, fmt.Errorf("committing generation %d of %s: %w", g.Seq, g.SessionID, err)
	}
	return g, nil
}

// GenFiles is every file in a generation, ordered by path so a listing and a
// golden file get the same sequence without the caller sorting.
func (db *DB) GenFiles(ctx context.Context, generationID int64) ([]GenFile, error) {
	const q = `
		SELECT generation_id, path, old_path, status, base_blob, head_blob
		FROM gen_files
		WHERE generation_id = ?
		ORDER BY path`

	rows, err := db.handle.QueryContext(ctx, q, generationID)
	if err != nil {
		return nil, fmt.Errorf("reading the files of generation %d: %w", generationID, err)
	}
	defer func() { _ = rows.Close() }()

	var files []GenFile
	for rows.Next() {
		var f GenFile
		if err := rows.Scan(&f.GenerationID, &f.Path, &f.OldPath, &f.Status, &f.BaseBlob, &f.HeadBlob); err != nil {
			return nil, fmt.Errorf("reading the files of generation %d: %w", generationID, err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the files of generation %d: %w", generationID, err)
	}
	return files, nil
}
