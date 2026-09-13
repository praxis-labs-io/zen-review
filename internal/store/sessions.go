package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Kind string

const (
	KindBranch   Kind = "branch"
	KindRange    Kind = "range"
	KindDetached Kind = "detached"
)

type Session struct {
	ID string

	// RepoPath is the resolved git common dir.
	RepoPath string

	Kind   Kind
	Branch string

	RangeSpec string

	BaseRef string

	Summary string

	// CreatedAt and UpdatedAt round-trip truncated to the second.
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (db *DB) Session(ctx context.Context, id string) (Session, bool, error) {
	const q = `
		SELECT id, repo_path, kind, branch, range_spec, base_ref, summary, created_at, updated_at
		FROM sessions
		WHERE id = ?`

	var s Session
	var created, updated string

	err := db.handle.QueryRowContext(ctx, q, id).Scan(
		&s.ID, &s.RepoPath, &s.Kind, &s.Branch, &s.RangeSpec, &s.BaseRef, &s.Summary, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("reading the session %s: %w", id, err)
	}

	if s.CreatedAt, err = moment(created); err != nil {
		return Session{}, false, fmt.Errorf("reading the session %s: %w", id, err)
	}
	if s.UpdatedAt, err = moment(updated); err != nil {
		return Session{}, false, fmt.Errorf("reading the session %s: %w", id, err)
	}
	return s, true, nil
}

// SaveSession inserts or updates s. An update keeps the stored CreatedAt and Summary, which SetSessionSummary owns.
func (db *DB) SaveSession(ctx context.Context, s Session) error {
	const q = `
		INSERT INTO sessions (id, repo_path, kind, branch, range_spec, base_ref, summary, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			repo_path  = excluded.repo_path,
			kind       = excluded.kind,
			branch     = excluded.branch,
			range_spec = excluded.range_spec,
			base_ref   = excluded.base_ref,
			updated_at = excluded.updated_at`

	_, err := db.handle.ExecContext(ctx, q,
		s.ID, s.RepoPath, string(s.Kind), s.Branch, s.RangeSpec, s.BaseRef, s.Summary,
		stamp(s.CreatedAt), stamp(s.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("saving the session %s: %w", s.ID, err)
	}
	return nil
}

// SetSessionSummary writes the summary alone, and errors when the session has no row.
func (db *DB) SetSessionSummary(ctx context.Context, id, summary string, at time.Time) error {
	const q = `UPDATE sessions SET summary = ?, updated_at = ? WHERE id = ?`

	res, err := db.handle.ExecContext(ctx, q, summary, stamp(at), id)
	if err != nil {
		return fmt.Errorf("writing the summary of the session %s: %w", id, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("writing the summary of the session %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("the session %s is no longer in the database", id)
	}
	return nil
}
