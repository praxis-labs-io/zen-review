package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Scope is what a comment is about: one line, a run of them, or the file itself.
type Scope string

const (
	ScopeLine  Scope = "line"
	ScopeRange Scope = "range"
	ScopeFile  Scope = "file"
)

// CommentState is where a comment sits in its lifecycle.
//
// An agent reaches CommentAddressed and never CommentResolved. The claim and the
// confirmation are different facts and the queue shows them as such.
type CommentState string

const (
	CommentOpen      CommentState = "open"
	CommentAddressed CommentState = "addressed"
	CommentResolved  CommentState = "resolved"
	CommentOrphaned  CommentState = "orphaned"
)

// Comment is one thing somebody said about the changeset.
//
// Path, Side and LineRange are the live anchor, and they only ever hold a
// translation that succeeded. GenerationID is the generation they are measured
// in, and it moves forward with them while the comment is open.
type Comment struct {
	ID        string
	SessionID string

	// GenerationID is where the anchor sits now. CreatedGenerationID is where the
	// comment started, which is the tree AnchorBlob resolves against.
	GenerationID        int64
	CreatedGenerationID int64

	Path string
	Side Side

	// LineRange is 0:0 for a file comment, which names the file rather than any
	// line in it, the way a whole-file reviewed range does.
	LineRange

	Scope Scope
	Body  string
	State CommentState

	// AnchorBlob is the file's blob at the generation the comment was written
	// against: the exact bytes it was about, immune to every rename and held
	// alive by the session ref. It is empty on a side the file has no blob on.
	AnchorBlob string

	// LastPath and LastLine are where the anchor was when the comment stopped
	// moving, so a reader is told where a frozen one lived without knowing which
	// generation it is pinned to. Both are empty until it stops.
	LastPath string
	LastLine int

	// UpdatedAt is when the comment last changed, which a refresh translating its
	// anchor is not: that is the code moving under it.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CommentMove is one comment's anchor after a translation.
//
// Lost is the anchor that did not survive, and orphans the comment where it
// stands rather than moving it anywhere.
type CommentMove struct {
	ID   string
	Lost bool

	Path string
	LineRange
}

const commentColumns = `
	id, session_id, generation_id, created_generation_id,
	path, side, start_line, end_line, scope, body, state,
	anchor_blob, last_path, last_line, created_at, updated_at`

// AddComment writes one comment.
//
// The id and both timestamps come from the caller. This package holds no clock
// and no randomness, which is what keeps everything above it testable.
//
// It returns ErrStaleGeneration when GenerationID is no longer the session's
// latest. One insert needs a transaction for that alone: the assertion and the
// write have to land together, or a comment lands anchored to a generation a
// refresh has already read past and shows a live anchor on code nobody wrote it
// about.
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = tx.ExecContext(ctx, q,
		c.ID, c.SessionID, c.GenerationID, c.CreatedGenerationID,
		c.Path, string(c.Side), c.Start, c.End, string(c.Scope), c.Body, string(c.State),
		c.AnchorBlob, c.LastPath, c.LastLine, stamp(c.CreatedAt), stamp(c.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("writing the comment on %s: %w", c.Path, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("writing the comment on %s: %w", c.Path, err)
	}
	return nil
}

// Comments is every comment of a session, in the order a reader reads them:
// by file, then down the file, then by when they were written.
//
// Filtering by state or by path happens above this. It is a handful of rows and
// one query beats four that drift.
func (db *DB) Comments(ctx context.Context, sessionID string) ([]Comment, error) {
	const q = `
		SELECT ` + commentColumns + `
		FROM comments
		WHERE session_id = ?
		ORDER BY path, start_line, created_at, id`

	return comments(ctx, db.handle, q, fmt.Sprintf("reading the comments of %s", sessionID), sessionID)
}

// CommentsAt is Comments, refused when generationID is no longer the latest. A
// refresh landing mid-read leaves a caller holding new anchors against an old diff.
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

// OpenComments is what a refresh translates: the open comments anchored to one
// generation. Everything else has stopped moving and has nothing to carry.
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

// Comment reads one comment by id. One that is not there comes back as
// (Comment{}, false, nil): absence is an answer, not a failure.
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

// FreezeComment stops a comment moving: the state it stopped in, and where its
// anchor was when it did. It returns the row as it landed, and false when the
// swap was lost.
//
// The location goes in the same write as the state, off the row's own columns.
// A frozen comment without one is a comment that lost where it was, and there is
// no later pass to fill it in. Taking it from the caller's read instead would
// write where the anchor was a moment ago: a refresh translating an anchor
// forward leaves the comment open, so the state swap still wins and records a
// line the comment no longer sits on.
//
// was is the state the caller read before deciding this transition was allowed,
// and the write only lands while the row is still in it. It reports false when it
// is not, which is another instance having got there first: without the swap the
// decision and the write are two statements, and a resolve landing between them
// would be overwritten by an address that was refused the moment it was read.
func (db *DB) FreezeComment(
	ctx context.Context,
	id string,
	was, state CommentState,
	now time.Time,
) (Comment, bool, error) {
	const q = `
		UPDATE comments
		SET state = ?, last_path = path, last_line = start_line, updated_at = ?
		WHERE id = ? AND state = ?
		RETURNING ` + commentColumns

	c, err := scanComment(db.handle.QueryRowContext(ctx, q, string(state), stamp(now), id, string(was)))
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, false, nil
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("marking the comment %s as %s: %w", id, state, err)
	}
	return c, true, nil
}

// EditComment rewrites a body and stamps the row. It returns the row as it
// landed, and false when that session holds no comment under the id.
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

// DeleteComment removes a comment and returns the row that went. The session is
// in the statement, so a delete cannot land on a row it was refused a read of.
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

// comments runs one of the listing queries above.
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

// scanner is what *sql.Row and *sql.Rows have in common, so one column list is
// read one way whether it came back alone or in a listing.
type scanner interface {
	Scan(dest ...any) error
}

func scanComment(s scanner) (Comment, error) {
	var c Comment
	var created, updated string

	err := s.Scan(
		&c.ID, &c.SessionID, &c.GenerationID, &c.CreatedGenerationID,
		&c.Path, &c.Side, &c.Start, &c.End, &c.Scope, &c.Body, &c.State,
		&c.AnchorBlob, &c.LastPath, &c.LastLine, &created, &updated,
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

// moveComment applies one translated anchor inside the caller's transaction.
//
// A surviving anchor moves onto the generation being written. A lost one leaves
// the comment where it stands and orphans it: its path and lines are the last
// place the anchor was, and they are copied into the columns a reader is shown
// so nothing has to know which generation the row is pinned at.
//
// The move leaves updated_at alone. The lines under a comment moving is the code
// changing and not the comment, and stamping it here would reset every comment in
// the session on every refresh, which costs the column the only thing it says.
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
