package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Kind is what a session is keyed on.
type Kind string

const (
	KindBranch   Kind = "branch"
	KindRange    Kind = "range"
	KindDetached Kind = "detached"
)

// Session is one repository plus one thing to review in it.
type Session struct {
	// ID is opaque and derived above this package. A branch name can hold
	// anything a ref can, and two of them cannot both become a ref path.
	ID string

	// RepoPath is the resolved git common dir.
	RepoPath string

	Kind   Kind
	Branch string

	// RangeSpec is the key for a session that is not a branch.
	RangeSpec string

	// BaseRef is what the changeset is measured from. Set once, and it sticks.
	BaseRef string

	// Summary is the session-level note.
	Summary string

	// CreatedAt and UpdatedAt are stored to the second. A value read back is the
	// one written truncated, so compare with Time.Equal against a truncated time
	// rather than against what was passed in.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Session reads one session. One that is not there comes back as
// (Session{}, false, nil): absence is an answer, not a failure.
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

// SaveSession writes a session, creating it or updating it in place.
//
// CreatedAt is left where it was on an update, because a session resumed three
// days later is the same session. Both times come off s rather than a clock
// here: this package holds none, which is what keeps its callers testable.
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
			summary    = excluded.summary,
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
