package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Scope string

const (
	ScopeLine  Scope = "line"
	ScopeRange Scope = "range"
	ScopeFile  Scope = "file"
)

type CommentState string

const (
	CommentOpen      CommentState = "open"
	CommentAddressed CommentState = "addressed"
	CommentResolved  CommentState = "resolved"
	CommentOrphaned  CommentState = "orphaned"
)

type Comment struct {
	ID        string
	SessionID string

	// CreatedGenerationID is the generation AnchorBlob was read at.
	GenerationID        int64
	CreatedGenerationID int64

	Path string
	Side Side

	// LineRange is 0:0 on a file comment.
	LineRange

	Scope Scope
	Body  string
	State CommentState

	Response string

	// AnchorBlob is empty on a side the file has no blob on.
	AnchorBlob string

	// CreatedRange is where the anchor sat in AnchorBlob, and 0:0 on a file comment or a row older than the column.
	CreatedRange LineRange

	// LastPath and LastLine are where the anchor was when the comment stopped moving, and empty until then.
	LastPath string
	LastLine int

	// UpdatedAt is not stamped by a refresh moving the anchor.
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CommentMove struct {
	// Lost orphans the comment where it stands instead of moving it.
	ID   string
	Lost bool

	Path string
	LineRange
}

const commentColumns = `
	id, session_id, generation_id, created_generation_id,
	path, side, start_line, end_line, scope, body, state,
	anchor_blob, last_path, last_line, created_at, updated_at, response,
	created_start_line, created_end_line`

// AddComment returns ErrStaleGeneration when c.GenerationID is no longer the session's latest.
func (db *DB) AddComment(ctx context.Context, c Comment) (err error) {
	tx, err := db.handle.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("writing the comment on %s: %w", c.Path, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = assertLatest(ctx, tx, c.SessionID, c.GenerationID); err != nil {
		return err
	}

	const q = `
		INSERT INTO comments (` + commentColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = tx.ExecContext(ctx, q,
		c.ID, c.SessionID, c.GenerationID, c.CreatedGenerationID,
		c.Path, string(c.Side), c.Start, c.End, string(c.Scope), c.Body, string(c.State),
		c.AnchorBlob, c.LastPath, c.LastLine, stamp(c.CreatedAt), stamp(c.UpdatedAt), c.Response,
		c.CreatedRange.Start, c.CreatedRange.End,
	)
	if err != nil {
		return fmt.Errorf("writing the comment on %s: %w", c.Path, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("writing the comment on %s: %w", c.Path, err)
	}
	return nil
}

// Comments returns a session's comments ordered by path, start line and creation.
func (db *DB) Comments(ctx context.Context, sessionID string) ([]Comment, error) {
	const q = `
		SELECT ` + commentColumns + `
		FROM comments
		WHERE session_id = ?
		ORDER BY path, start_line, created_at, id`

	return comments(ctx, db.handle, q, fmt.Sprintf("reading the comments of %s", sessionID), sessionID)
}

// CommentsAt is Comments, returning ErrStaleGeneration when generationID is no longer the latest.
func (db *DB) CommentsAt(ctx context.Context, sessionID string, generationID int64) (_ []Comment, err error) {
	tx, err := db.handle.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("reading the comments of %s: %w", sessionID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if err = assertLatest(ctx, tx, sessionID, generationID); err != nil {
		return nil, err
	}

	const q = `
		SELECT ` + commentColumns + `
		FROM comments
		WHERE session_id = ?
		ORDER BY path, start_line, created_at, id`

	return comments(ctx, tx, q, fmt.Sprintf("reading the comments of %s", sessionID), sessionID)
}

func (db *DB) OpenComments(ctx context.Context, generationID int64) ([]Comment, error) {
	return openComments(ctx, db.handle, generationID)
}

func openComments(ctx context.Context, q rower, generationID int64) ([]Comment, error) {
	const read = `
		SELECT ` + commentColumns + `
		FROM comments
		WHERE generation_id = ? AND state = 'open'
		ORDER BY path, start_line, created_at, id`

	return comments(ctx, q, read,
		fmt.Sprintf("reading the open comments of generation %d", generationID), generationID)
}

func (db *DB) Comment(ctx context.Context, id string) (Comment, bool, error) {
	const q = `
		SELECT ` + commentColumns + `
		FROM comments
		WHERE id = ?`

	c, err := scanComment(db.handle.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, false, nil
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("reading the comment %s: %w", id, err)
	}
	return c, true, nil
}

// FreezeComment moves a comment from was to state and records its current anchor as LastPath and LastLine.
// A nil response keeps the stored one. Returns false, changing nothing, when the row is no longer in was.
func (db *DB) FreezeComment(
	ctx context.Context,
	id string,
	was, state CommentState,
	response *string,
	now time.Time,
) (Comment, bool, error) {
	const q = `
		UPDATE comments
		SET state = ?, response = COALESCE(?, response),
		    last_path = path, last_line = start_line, updated_at = ?
		WHERE id = ? AND state = ?
		RETURNING ` + commentColumns

	c, err := scanComment(db.handle.QueryRowContext(ctx, q,
		string(state), response, stamp(now), id, string(was)))
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, false, nil
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("marking the comment %s as %s: %w", id, state, err)
	}
	return c, true, nil
}

// EditComment rewrites the body alone. Returns false when sessionID holds no comment with id.
func (db *DB) EditComment(
	ctx context.Context,
	id, sessionID, body string,
	now time.Time,
) (Comment, bool, error) {
	const q = `
		UPDATE comments
		SET body = ?, updated_at = ?
		WHERE id = ? AND session_id = ?
		RETURNING ` + commentColumns

	c, err := scanComment(db.handle.QueryRowContext(ctx, q, body, stamp(now), id, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, false, nil
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("rewriting the comment %s: %w", id, err)
	}
	return c, true, nil
}

// DeleteComment returns the deleted row, or false when sessionID holds no comment with id.
func (db *DB) DeleteComment(ctx context.Context, id, sessionID string) (Comment, bool, error) {
	const q = `
		DELETE FROM comments
		WHERE id = ? AND session_id = ?
		RETURNING ` + commentColumns

	c, err := scanComment(db.handle.QueryRowContext(ctx, q, id, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, false, nil
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("deleting the comment %s: %w", id, err)
	}
	return c, true, nil
}

func comments(ctx context.Context, q rower, query, describe string, arg any) ([]Comment, error) {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", describe, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", describe, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", describe, err)
	}
	return out, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanComment(s scanner) (Comment, error) {
	var c Comment
	var created, updated string

	err := s.Scan(
		&c.ID, &c.SessionID, &c.GenerationID, &c.CreatedGenerationID,
		&c.Path, &c.Side, &c.Start, &c.End, &c.Scope, &c.Body, &c.State,
		&c.AnchorBlob, &c.LastPath, &c.LastLine, &created, &updated, &c.Response,
		&c.CreatedRange.Start, &c.CreatedRange.End,
	)
	if err != nil {
		return Comment{}, err
	}
	if c.CreatedAt, err = moment(created); err != nil {
		return Comment{}, err
	}
	if c.UpdatedAt, err = moment(updated); err != nil {
		return Comment{}, err
	}
	return c, nil
}

func moveComment(ctx context.Context, tx *sql.Tx, generationID int64, now time.Time, m CommentMove) error {
	const move = `
		UPDATE comments
		SET generation_id = ?, path = ?, start_line = ?, end_line = ?
		WHERE id = ?`

	const orphan = `
		UPDATE comments
		SET state = 'orphaned', last_path = path, last_line = start_line, updated_at = ?
		WHERE id = ?`

	var err error
	if m.Lost {
		_, err = tx.ExecContext(ctx, orphan, stamp(now), m.ID)
	} else {
		_, err = tx.ExecContext(ctx, move, generationID, m.Path, m.Start, m.End, m.ID)
	}
	if err != nil {
		return fmt.Errorf("carrying the comment %s: %w", m.ID, err)
	}
	return nil
}
