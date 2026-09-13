package review

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/store"
)

type NoCommentError struct {
	ID string
}

func (e *NoCommentError) Error() string {
	return fmt.Sprintf("this session has no comment %s", e.ID)
}

// CommentStateError refuses a transition To a state the comment's current state Is does not allow.
type CommentStateError struct {
	ID string
	Is store.CommentState
	To store.CommentState
}

func (e *CommentStateError) Error() string {
	return fmt.Sprintf("the comment %s is %s, so it cannot be marked %s", e.ID, e.Is, e.To)
}

// Note is a comment to be written. Path is the head-side name, whichever side the anchor is on.
type Note struct {
	Path  string
	Side  store.Side
	Scope store.Scope
	Range Range
	Body  string
}

// NoteOnHunk anchors to the side and lines h is named by, as a range even when it holds one line.
func NoteOnHunk(path string, h Hunk, body string) Note {
	a := h.Anchors[0]
	return Note{Path: path, Side: a.Side, Scope: store.ScopeRange, Range: a.Range, Body: body}
}

// NoteOnLines is a line comment when r is one line and a range comment otherwise.
func NoteOnLines(path string, side store.Side, r Range, body string) Note {
	scope := store.ScopeRange
	if r.Start == r.End {
		scope = store.ScopeLine
	}
	return Note{Path: path, Side: side, Scope: scope, Range: r, Body: body}
}

// NoteOnFile anchors to the side f has bytes on: the base for a deleted file, the head otherwise.
func NoteOnFile(f File, body string) Note {
	return Note{Path: f.Diff.Path, Side: wholeSide(f.Diff.Status), Scope: store.ScopeFile, Body: body}
}

// AddComment writes n against g and returns the row. It refuses a g that is not the latest,
// a path g does not hold, and a side the file has no bytes on.
func (s *Session) AddComment(ctx context.Context, g Generation, n Note) (store.Comment, error) {
	if err := n.check(); err != nil {
		return store.Comment{}, err
	}

	f, found, err := s.db.GenFile(ctx, g.ID, n.Path)
	if err != nil {
		return store.Comment{}, err
	}
	if !found {
		return store.Comment{}, fmt.Errorf("generation %d holds no %s, so there is nothing there to comment on",
			g.Seq, n.Path)
	}

	path, blob := f.Path, f.HeadBlob
	if n.Side == store.SideBase {
		blob = f.BaseBlob
		if f.OldPath != "" {
			path = f.OldPath
		}
	}

	if blob == "" {
		return store.Comment{}, fmt.Errorf("generation %d holds no %s-side bytes of %s, so there is nothing there to anchor to",
			g.Seq, n.Side, n.Path)
	}

	id, err := commentID()
	if err != nil {
		return store.Comment{}, err
	}

	at := store.LineRange{Start: n.Range.Start, End: n.Range.End}

	now := time.Now().UTC().Truncate(time.Second)
	c := store.Comment{
		ID:                  id,
		SessionID:           s.row.ID,
		GenerationID:        g.ID,
		CreatedGenerationID: g.ID,
		Path:                path,
		Side:                n.Side,
		LineRange:           at,
		Scope:               n.Scope,
		Body:                n.Body,
		State:               store.CommentOpen,
		AnchorBlob:          blob,
		CreatedRange:        at,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.db.AddComment(ctx, c); err != nil {
		return store.Comment{}, s.stale(ctx, g, err)
	}
	return c, nil
}

// Comments is every comment of the session, live and frozen, in file-tree order. A base-side
// comment carries the file's base-side name.
func (s *Session) Comments(ctx context.Context) ([]store.Comment, error) {
	rows, err := s.db.Comments(ctx, s.row.ID)
	if err != nil {
		return nil, err
	}
	return byTreeOrder(rows), nil
}

// CommentsAt is Comments, refused when g is no longer the latest.
func (s *Session) CommentsAt(ctx context.Context, g Generation) ([]store.Comment, error) {
	rows, err := s.db.CommentsAt(ctx, s.row.ID, g.ID)
	if err != nil {
		return nil, s.stale(ctx, g, err)
	}
	return byTreeOrder(rows), nil
}

func byTreeOrder(rows []store.Comment) []store.Comment {
	slices.SortStableFunc(rows, func(a, b store.Comment) int { return byTree(a.Path, b.Path) })
	return rows
}

// AddressComment moves an open or orphaned comment to addressed. A blank response stores none.
func (s *Session) AddressComment(ctx context.Context, id, response string) (store.Comment, error) {
	if strings.TrimSpace(response) == "" {
		response = ""
	}
	return s.freeze(ctx, id, store.CommentAddressed, &response,
		store.CommentOpen, store.CommentOrphaned)
}

// ResolveComment closes an open, addressed or orphaned comment, and refuses a resolved one.
func (s *Session) ResolveComment(ctx context.Context, id string) (store.Comment, error) {
	return s.freeze(ctx, id, store.CommentResolved, nil,
		store.CommentOpen, store.CommentAddressed, store.CommentOrphaned)
}

// EditComment replaces a comment's body in any state, leaving its anchor and response.
func (s *Session) EditComment(ctx context.Context, id, body string) (store.Comment, error) {
	if err := checkBody(body); err != nil {
		return store.Comment{}, err
	}

	now := time.Now().UTC().Truncate(time.Second)
	c, found, err := s.db.EditComment(ctx, id, s.row.ID, body, now)
	if err != nil {
		return store.Comment{}, err
	}
	if !found {
		return store.Comment{}, &NoCommentError{ID: id}
	}
	return c, nil
}

// DeleteComment removes a comment for good, in any state.
func (s *Session) DeleteComment(ctx context.Context, id string) (store.Comment, error) {
	c, found, err := s.db.DeleteComment(ctx, id, s.row.ID)
	if err != nil {
		return store.Comment{}, err
	}
	if !found {
		return store.Comment{}, &NoCommentError{ID: id}
	}
	return c, nil
}

// freezeAttempts outlasts a comment's life, which moves it at most twice.
const freezeAttempts = 3

func (s *Session) freeze(
	ctx context.Context,
	id string,
	to store.CommentState,
	response *string,
	from ...store.CommentState,
) (store.Comment, error) {
	for range freezeAttempts {
		c, err := s.comment(ctx, id)
		if err != nil {
			return store.Comment{}, err
		}
		if !slices.Contains(from, c.State) {
			return store.Comment{}, &CommentStateError{ID: id, Is: c.State, To: to}
		}

		if s.beforeFreeze != nil {
			s.beforeFreeze()
		}

		now := time.Now().UTC().Truncate(time.Second)
		frozen, won, err := s.db.FreezeComment(ctx, id, c.State, to, response, now)
		if err != nil {
			return store.Comment{}, err
		}
		if won {
			return frozen, nil
		}
	}
	return store.Comment{}, fmt.Errorf("the comment %s is being changed from somewhere else faster than this can read it", id)
}

func (s *Session) comment(ctx context.Context, id string) (store.Comment, error) {
	c, found, err := s.db.Comment(ctx, id)
	if err != nil {
		return store.Comment{}, err
	}
	if !found || c.SessionID != s.row.ID {
		return store.Comment{}, &NoCommentError{ID: id}
	}
	return c, nil
}

func (n Note) check() error {
	if err := checkBody(n.Body); err != nil {
		return err
	}
	if n.Side != store.SideHead && n.Side != store.SideBase {
		return fmt.Errorf("a comment anchors to the head or the base, not %q", n.Side)
	}

	switch n.Scope {
	case store.ScopeFile:
		if !n.Range.whole() || n.Range.End != 0 {
			return errors.New("a file comment names the file rather than lines in it")
		}
	case store.ScopeLine:
		if n.Range.Start < 1 || n.Range.Start != n.Range.End {
			return fmt.Errorf("a line comment is on one line, and %d:%d is not one", n.Range.Start, n.Range.End)
		}
	case store.ScopeRange:
		if n.Range.Start < 1 || n.Range.End < n.Range.Start {
			return fmt.Errorf("a range comment runs from a line to a later one, and %d:%d does not",
				n.Range.Start, n.Range.End)
		}
	default:
		return fmt.Errorf("a comment is scoped to a line, a range or a file, not %q", n.Scope)
	}
	return nil
}

func checkBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment with nothing in it says nothing")
	}
	return nil
}

// commentID is random, not derived, because two identical comments on one line are still two.
func commentID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("naming the comment: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
