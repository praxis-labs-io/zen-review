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

	"github.com/zen-review/zen-review/internal/store"
)

// NoCommentError means this session holds no comment under that id.
type NoCommentError struct {
	ID string
}

func (e *NoCommentError) Error() string {
	return fmt.Sprintf("this session has no comment %s", e.ID)
}

// CommentStateError means a comment cannot go where it was asked to go.
//
// An agent reaches addressed and never resolved, and nothing re-opens a comment
// that has stopped moving. Both refusals arrive here.
type CommentStateError struct {
	ID string
	Is store.CommentState
	To store.CommentState
}

func (e *CommentStateError) Error() string {
	return fmt.Sprintf("the comment %s is %s, so it cannot be marked %s", e.ID, e.Is, e.To)
}

// Note is what a comment is written from: where it points, and what it says.
//
// Path is the head-side name the changeset lists the file under, whichever side
// the anchor is on. The base-side name a base anchor is stored under is settled
// below, because a rename makes it a different one and only the generation knows.
type Note struct {
	Path  string
	Side  store.Side
	Scope store.Scope
	Range Range
	Body  string
}

// NoteOnHunk is a comment on a whole hunk, anchored at the side and lines the
// hunk is named by.
//
// It is a range rather than a line even where the hunk holds one, because a hunk
// is a region and a selection of one line is still a selection.
func NoteOnHunk(path string, h Hunk, body string) Note {
	a := h.Anchors[0]
	return Note{Path: path, Side: a.Side, Scope: store.ScopeRange, Range: a.Range, Body: body}
}

// AddComment writes a comment against a generation and returns the row.
//
// It refuses a stale generation for the reason a mark does: the carry runs from
// the latest generation, so a comment anchored to an older one is never picked
// up and never moves again.
//
// A path the generation does not hold is refused rather than stored. There is
// nothing there to anchor to, no blob to record, and nothing that would ever
// show the comment.
func (s *Session) AddComment(ctx context.Context, g Generation, n Note) (store.Comment, error) {
	if err := n.check(); err != nil {
		return store.Comment{}, err
	}

	latest, found, err := s.db.LatestGeneration(ctx, s.row.ID)
	if err != nil {
		return store.Comment{}, err
	}
	if !found || latest.ID != g.ID {
		return store.Comment{}, &StaleGenerationError{Seq: g.Seq, Current: latest.Seq}
	}

	f, found, err := s.db.GenFile(ctx, g.ID, n.Path)
	if err != nil {
		return store.Comment{}, err
	}
	if !found {
		return store.Comment{}, fmt.Errorf("generation %d holds no %s, so there is nothing there to comment on",
			g.Seq, n.Path)
	}

	// The base side stores under the name the file has on the base, which a rename
	// makes a different one, and the blob it records is the one that side has.
	path, blob := f.Path, f.HeadBlob
	if n.Side == store.SideBase {
		blob = f.BaseBlob
		if f.OldPath != "" {
			path = f.OldPath
		}
	}

	id, err := commentID()
	if err != nil {
		return store.Comment{}, err
	}

	now := time.Now().UTC().Truncate(time.Second)
	c := store.Comment{
		ID:                  id,
		SessionID:           s.row.ID,
		GenerationID:        g.ID,
		CreatedGenerationID: g.ID,
		Path:                path,
		Side:                n.Side,
		LineRange:           store.LineRange{Start: n.Range.Start, End: n.Range.End},
		Scope:               n.Scope,
		Body:                n.Body,
		State:               store.CommentOpen,
		AnchorBlob:          blob,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.db.AddComment(ctx, c); err != nil {
		return store.Comment{}, err
	}
	return c, nil
}

// Comments is every comment of the session, live and frozen, by file and then
// down the file.
//
// A base-side comment carries the file's base-side name, the same way a
// base-side reviewed range does, so a caller listing them beside a changeset
// joins a renamed one back through the old path.
func (s *Session) Comments(ctx context.Context) ([]store.Comment, error) {
	return s.db.Comments(ctx, s.row.ID)
}

// AddressComment is the agent's verb: a claim that the comment has been handled.
//
// Only an open comment can be addressed. Nothing here reaches resolved, because
// the claim and the confirmation are different facts and a queue that let one
// stand for the other would be worth nothing.
func (s *Session) AddressComment(ctx context.Context, id string) (store.Comment, error) {
	return s.freeze(ctx, id, store.CommentAddressed, store.CommentOpen)
}

// ResolveComment is the reader's verb, and it closes anything that is not
// already closed. An orphaned comment is resolved the same way: the code it was
// about is gone, and saying so is the reader's call.
func (s *Session) ResolveComment(ctx context.Context, id string) (store.Comment, error) {
	return s.freeze(ctx, id, store.CommentResolved,
		store.CommentOpen, store.CommentAddressed, store.CommentOrphaned)
}

// freeze stops a comment moving, recording where its anchor was when it did.
//
// A comment belonging to another session is not found rather than refused. The
// database is shared by every session in the repository and one session's ids
// are not another's business.
func (s *Session) freeze(
	ctx context.Context,
	id string,
	to store.CommentState,
	from ...store.CommentState,
) (store.Comment, error) {
	c, found, err := s.db.Comment(ctx, id)
	if err != nil {
		return store.Comment{}, err
	}
	if !found || c.SessionID != s.row.ID {
		return store.Comment{}, &NoCommentError{ID: id}
	}
	if !slices.Contains(from, c.State) {
		return store.Comment{}, &CommentStateError{ID: id, Is: c.State, To: to}
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.db.FreezeComment(ctx, id, to, c.Path, c.Start, now); err != nil {
		return store.Comment{}, err
	}

	c.State, c.LastPath, c.LastLine, c.UpdatedAt = to, c.Path, c.Start, now
	return c, nil
}

// check reads a note against itself, so a scope that disagrees with its lines
// gets a sentence rather than a constraint violation from three layers down.
func (n Note) check() error {
	if strings.TrimSpace(n.Body) == "" {
		return errors.New("a comment with nothing in it says nothing")
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

// commentID is what a comment is named by on the command line.
//
// Twelve hex characters: short enough to paste into a resolve, and wide enough
// that a collision inside one repository is not a thing to write code about.
// Random rather than derived, because two comments on one line saying different
// things are two comments.
func commentID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("naming the comment: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
