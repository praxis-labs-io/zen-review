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

// NoteOnLines is a comment on lines somebody picked out.
//
// The scope falls out of the lines rather than out of how they were spelled. One
// line is a line comment however it was typed, and a caller that had to say
// which would be a second place the two could disagree.
func NoteOnLines(path string, side store.Side, r Range, body string) Note {
	scope := store.ScopeRange
	if r.Start == r.End {
		scope = store.ScopeLine
	}
	return Note{Path: path, Side: side, Scope: scope, Range: r, Body: body}
}

// NoteOnFile is a comment on the file itself rather than on lines in it,
// anchored on the side the file has bytes on.
//
// A deleted file is commented on the base, the same rule a whole-file mark
// takes: a head-side anchor on one would name bytes that are not there, and it
// would survive every rewrite of the bytes it actually removed.
func NoteOnFile(f File, body string) Note {
	return Note{Path: f.Diff.Path, Side: wholeSide(f.Diff), Scope: store.ScopeFile, Body: body}
}

// AddComment writes a comment against a generation and returns the row.
//
// It refuses a stale generation for the reason a mark does, from inside the same
// transaction as the insert: the carry runs from the latest generation, so a
// comment anchored to an older one is never picked up and never moves again,
// while still showing a live anchor on code nobody wrote it about.
//
// A path the generation does not hold is refused rather than stored. There is
// nothing there to anchor to, no blob to record, and nothing that would ever
// show the comment.
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

	// The base side stores under the name the file has on the base, which a rename
	// makes a different one, and the blob it records is the one that side has.
	path, blob := f.Path, f.HeadBlob
	if n.Side == store.SideBase {
		blob = f.BaseBlob
		if f.OldPath != "" {
			path = f.OldPath
		}
	}

	// An added file has no base blob and a deleted one has no head blob. A note on
	// the side it is missing from anchors to no bytes at all, and it cannot even
	// orphan: that side's diff never lists a file it does not have, so the anchor
	// comes through untouched on every refresh forever.
	if blob == "" {
		return store.Comment{}, fmt.Errorf("generation %d holds no %s-side bytes of %s, so there is nothing there to anchor to",
			g.Seq, n.Side, n.Path)
	}

	id, err := commentID()
	if err != nil {
		return store.Comment{}, err
	}

	// The live range and the creation range start equal. Only the first ever
	// moves; the second is what slices AnchorBlob a dozen generations later.
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

// Comments is every comment of the session, live and frozen, by file and then
// down the file.
//
// A base-side comment carries the file's base-side name, the same way a
// base-side reviewed range does, so a caller listing them beside a changeset
// joins a renamed one back through the old path.
//
// The files come back in the order a file tree reads, which is the order
// Session.Files hands the changeset back in. The store orders by path and that
// is bytewise, so without this a listing and the table beside it disagree about
// what comes first the moment the changeset has a directory in it. Stable, so
// the store's ordering within one file survives.
//
// A base-side row sorts under the name it is recorded by, which on a rename is
// the name the file has on the base rather than the one the changeset lists it
// under. So the two comments on a renamed file sit apart. Putting them together
// would mean resolving each row's path through the generation it belongs to,
// and these span every generation the session has.
func (s *Session) Comments(ctx context.Context) ([]store.Comment, error) {
	rows, err := s.db.Comments(ctx, s.row.ID)
	if err != nil {
		return nil, err
	}
	return byTreeOrder(rows), nil
}

// CommentsAt is Comments for a caller pairing them with a changeset, refused
// when g is no longer the latest. Two unversioned reads can straddle a refresh.
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

// AddressComment is the agent's verb: a claim that the comment has been handled,
// and the response that backs it.
//
// Only an open comment can be addressed. Nothing here reaches resolved, because
// the claim and the confirmation are different facts and a queue that let one
// stand for the other would be worth nothing.
//
// The response is optional. Half a queue is change requests where the diff is
// the response, and demanding a sentence there gets "done" typed into every one.
// AddressComment is the agent's verb, and an orphan takes it. The anchor going
// is usually this comment being acted on: the lines it named were rewritten
// because it asked for them to be. Refusing the answer there leaves the one
// state where an explanation is most wanted as the one state that cannot hold
// one, and leaves the reader resolving a comment blind.
//
// Safe on a frozen row because a response is written beside the anchor rather
// than derived from it. anchor_blob and the created_ columns are written once
// and never move, so an orphan still says where it lived.
func (s *Session) AddressComment(ctx context.Context, id, response string) (store.Comment, error) {
	// Whitespace alone is no response. Leading indent somebody meant is kept, the
	// way a body's is.
	if strings.TrimSpace(response) == "" {
		response = ""
	}
	return s.freeze(ctx, id, store.CommentAddressed, &response,
		store.CommentOpen, store.CommentOrphaned)
}

// ResolveComment is the reader's verb, and it closes anything that is not
// already closed. An orphaned comment is resolved the same way: the code it was
// about is gone, and saying so is the reader's call.
func (s *Session) ResolveComment(ctx context.Context, id string) (store.Comment, error) {
	return s.freeze(ctx, id, store.CommentResolved, nil,
		store.CommentOpen, store.CommentAddressed, store.CommentOrphaned)
}

// EditComment rewrites what a comment says, in any state: a typo in a resolved
// one is still a typo. The anchor never moves, so re-scoping is delete and write.
//
// The body and nothing else. A response is the agent's words, and the reader's
// verb reaching them is how the wrong half gets rewritten.
func (s *Session) EditComment(ctx context.Context, id, body string) (store.Comment, error) {
	if err := checkBody(body); err != nil {
		return store.Comment{}, err
	}

	// No generation is asserted. A body is true at every one of them, and a
	// refresh carries an anchor through columns this does not write.
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

// DeleteComment removes a comment for good, in any state. A deleted state would
// have to be filtered out of every count and every ring forever.
func (s *Session) DeleteComment(ctx context.Context, id string) (store.Comment, error) {
	// A refresh translating a row that has gone moves nothing and carries on, so
	// there is no generation here to be stale.
	c, found, err := s.db.DeleteComment(ctx, id, s.row.ID)
	if err != nil {
		return store.Comment{}, err
	}
	if !found {
		return store.Comment{}, &NoCommentError{ID: id}
	}
	return c, nil
}

// freezeAttempts is how many goes at the swap below. A comment moves at most
// twice, open to addressed or orphaned and then to resolved, so this is the
// length of its life rather than a guess at how unlucky one call can get.
const freezeAttempts = 3

// freeze stops a comment moving, recording where its anchor was when it did.
//
// A comment belonging to another session is not found rather than refused. The
// database is shared by every session in the repository and one session's ids
// are not another's business.
//
// The write only lands while the comment is still in the state that allowed it,
// so a transition another instance made first is refused rather than overwritten.
// Losing that swap goes again against the state that is there now, and refuses
// only once that state is one this transition does not accept. Answering the
// first loss with a refusal would turn a refresh orphaning a comment into a
// refused resolve, and resolving an orphan is the reader's call either way.
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
			// The row as it landed, rather than the read this go started from. A
			// refresh between the two moves the anchor, and the write records where
			// it moved to.
			return frozen, nil
		}
	}
	return store.Comment{}, fmt.Errorf("the comment %s is being changed from somewhere else faster than this can read it", id)
}

// comment is one of this session's comments, by id.
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

// check reads a note against itself, so a scope that disagrees with its lines
// gets a sentence rather than a constraint violation from three layers down.
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

// checkBody is the one rule a body keeps, which a write and a rewrite both take.
func checkBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment with nothing in it says nothing")
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
