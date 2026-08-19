// Package review is the engine: sessions, generations, review state, comments
// and remapping.
//
// Every state change goes through here, and the CLI and the TUI call the same
// functions. A behaviour reachable by key and not by subcommand is in the wrong
// place, and the test for anything landing in this package is whether the CLI
// could answer the question with no terminal attached.
//
// Nothing above this package opens a database or shells out to git.
package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/git"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// Options is what the caller knows that the session does not.
type Options struct {
	// BaseRef, when set, is used and stored, replacing whatever the session had.
	// Empty keeps the stored base, or detects one for a session that has none.
	BaseRef string
}

// Base is the ref that named the starting point, and the merge base the diff
// actually runs against.
type Base struct {
	Ref string
	SHA string

	// Fallback tags a base nobody asked for, in a word or two saying what it is.
	// Empty when the base is the one that was asked for.
	Fallback string
}

// EmptyTree is a base with no commit under it: HEAD is unborn, so the changeset
// is measured from the empty tree and no commit can parent on it.
func (b Base) EmptyTree() bool { return b.Ref == "" }

// Name is the base as a reader reads it. The empty tree has a sha and no ref to
// be named by.
func (b Base) Name() string {
	if b.EmptyTree() {
		return "empty tree"
	}
	return b.Ref
}

// Session is one repository plus one thing to review in it, open against the
// database and resumable days later.
type Session struct {
	repo *git.Repo
	db   *store.DB
	row  store.Session
	base Base

	// The windows a concurrent write lands in, as seams the concurrency tests
	// drive them through. Nothing outside those tests sets one.
	duringRefresh func()
	afterSwap     func()
	beforeFreeze  func()
}

// Open resolves the session for the repository containing path, creating it on
// first use, and settles what it is measured from. The caller closes it.
func Open(ctx context.Context, path string, opts Options) (*Session, error) {
	repo, err := git.Open(ctx, path)
	if err != nil {
		return nil, err
	}

	head, err := repo.Head(ctx)
	if err != nil {
		return nil, err
	}

	// The store's own error is passed through rather than wrapped. It already
	// names the path it could not open, or says the database came from a newer
	// build, and asserting a cause on top of that produces a line that
	// contradicts itself: telling a reader to fix permissions that are fine,
	// in the same breath as telling them to upgrade.
	//
	// Either way this is a startup failure and never a mode where the review is
	// silently not being saved.
	db, err := store.Open(ctx, databasePath(repo))
	if err != nil {
		return nil, err
	}

	s := &Session{repo: repo, db: db}
	if err := s.load(ctx, head, opts); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Session) Close() error { return s.db.Close() }

// ID is the opaque key this session is stored and named by.
func (s *Session) ID() string { return s.row.ID }

// Kind is what the session is keyed on.
func (s *Session) Kind() store.Kind { return s.row.Kind }

// Branch is empty on a session that is not keyed to one.
func (s *Session) Branch() string { return s.row.Branch }

// Repo names the repository under review. That is what a reader recognises;
// the absolute path is not, and it is too long to put anywhere a name goes.
//
// It comes off the common directory rather than the work tree, because a
// linked worktree and the checkout it came from share one session, keyed on
// exactly that directory. Naming the work tree would give one review two names
// depending on which directory it was opened from.
func (s *Session) Repo() string {
	dir := s.repo.CommonDir()
	if filepath.Base(dir) == ".git" {
		dir = filepath.Dir(dir)
	}
	return strings.TrimSuffix(filepath.Base(dir), ".git")
}

// Base is what the changeset is measured from.
func (s *Session) Base() Base { return s.base }

// SetBase replaces the ref this session keeps. It validates the ref against
// HEAD before writing anything, and a chosen ref never carries a fallback tag.
func (s *Session) SetBase(ctx context.Context, ref string) error {
	head, err := s.repo.Head(ctx)
	if err != nil {
		return err
	}
	if head.Unborn() {
		return errors.New("an unborn branch has no base to choose")
	}

	base, why, err := s.tryBase(ctx, ref, head.SHA)
	if err != nil {
		return err
	}
	if why != "" {
		return fmt.Errorf("%s does not resolve to a base of HEAD", ref)
	}

	if s.row.BaseRef != ref {
		now := time.Now().UTC().Truncate(time.Second)
		row := s.row
		row.BaseRef, row.UpdatedAt = ref, now
		if err := s.db.SaveSession(ctx, row); err != nil {
			return err
		}
		s.row = row
	}
	s.base = base
	return nil
}

// Summary is the session-level note, read rather than answered off the row this
// session opened on: a reader open for an hour is not a snapshot.
func (s *Session) Summary(ctx context.Context) (string, error) {
	row, found, err := s.db.Session(ctx, s.row.ID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("the session %s is no longer in the database", s.row.ID)
	}

	s.row.Summary = row.Summary
	return row.Summary, nil
}

// SetSummary writes the session-level note, replacing whatever was there. Empty
// clears it, which is the only way to take one back.
//
// The note is written on its own rather than through the whole row, because
// resolving a base writes the row too and holds the note it read at open time.
func (s *Session) SetSummary(ctx context.Context, text string) error {
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.db.SetSessionSummary(ctx, s.row.ID, text, now); err != nil {
		return err
	}
	s.row.Summary, s.row.UpdatedAt = text, now
	return nil
}

// load reads or creates the session row and settles its base.
func (s *Session) load(ctx context.Context, head git.Head, opts Options) error {
	kind, branch, spec := identity(head)
	id := sessionID(s.repo.CommonDir(), kind, branch, spec)

	row, found, err := s.db.Session(ctx, id)
	if err != nil {
		return err
	}

	// Truncated because that is the resolution the columns hold. Keeping the
	// nanoseconds here would make the value in hand differ from the one that
	// comes back, and every comparison against a reloaded session would fail for
	// a reason that has nothing to do with the session.
	now := time.Now().UTC().Truncate(time.Second)

	if !found {
		row = store.Session{
			ID:        id,
			RepoPath:  s.repo.CommonDir(),
			Kind:      kind,
			Branch:    branch,
			RangeSpec: spec,
			CreatedAt: now,
		}
	}

	base, err := s.resolveBase(ctx, head, row.BaseRef, opts.BaseRef)
	if err != nil {
		return err
	}

	// A fallback writes no ref, and does not clear the one already there. It is a
	// guess, and overwriting a chosen base would lose what to go back to.
	ref := row.BaseRef
	if base.Fallback == "" {
		ref = base.Ref
	}

	// Written only when something changed. Opening a session to read its status
	// twice should not touch the database the second time.
	if !found || row.BaseRef != ref {
		row.BaseRef = ref
		row.UpdatedAt = now
		if err := s.db.SaveSession(ctx, row); err != nil {
			return err
		}
	}

	s.row, s.base = row, base
	return nil
}

// databasePath is where the review lives: under the common dir, so a worktree
// and the checkout it came from share one database and a throwaway worktree
// does not take the review with it.
func databasePath(repo *git.Repo) string {
	return filepath.Join(repo.CommonDir(), "zen-review", "state.db")
}

// identity is what a session is keyed on, read off HEAD. A detached HEAD has no
// branch to key on and takes the sha instead.
func identity(head git.Head) (store.Kind, string, string) {
	if head.Branch == "" {
		return store.KindDetached, "", head.SHA
	}
	return store.KindBranch, head.Branch, ""
}

// sessionID is the opaque key a session is stored and named by.
//
// It cannot be the branch name. A branch may be called anything a ref may, and
// refs/zen-review/sessions/foo and refs/zen-review/sessions/foo/bar cannot both
// exist, so a session on foo/bar would collide with one on foo. Hex also derives
// without the database, which is what makes `git log` on the session ref usable
// from a shell.
//
// NUL separates the fields, because no ref name and no path may hold one, so no
// two identities can hash the same bytes.
func sessionID(repoPath string, kind store.Kind, branch, spec string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{repoPath, string(kind), branch, spec}, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}
