// Package review tracks what has been reviewed across generations of a changeset.
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

type Options struct {
	// BaseRef, when set, replaces the stored base. Empty keeps it, or detects one.
	BaseRef string
}

type Base struct {
	Ref string
	SHA string

	// Fallback tags a base nobody asked for, in a word or two. Empty for the one asked for.
	Fallback string
}

// EmptyTree reports a base with no commit under it, which is what an unborn HEAD measures from.
func (b Base) EmptyTree() bool { return b.Ref == "" }

// Name is the ref, or "empty tree" for a base with none.
func (b Base) Name() string {
	if b.EmptyTree() {
		return "empty tree"
	}
	return b.Ref
}

type Session struct {
	repo *git.Repo
	db   *store.DB
	row  store.Session
	base Base

	duringRefresh func()
	afterSwap     func()
	beforeFreeze  func()
}

// Open resolves the session for the repository containing path, creating it on first use.
// The caller closes it.
func Open(ctx context.Context, path string, opts Options) (*Session, error) {
	repo, err := git.Open(ctx, path)
	if err != nil {
		return nil, err
	}

	head, err := repo.Head(ctx)
	if err != nil {
		return nil, err
	}

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

func (s *Session) Close() error { return s.db.Close() }

func (s *Session) ID() string { return s.row.ID }

func (s *Session) Kind() store.Kind { return s.row.Kind }

// Branch is empty on a detached session.
func (s *Session) Branch() string { return s.row.Branch }

// Repo is the repository's name, the same from a linked worktree as from its checkout.
func (s *Session) Repo() string {
	dir := s.repo.CommonDir()
	if filepath.Base(dir) == ".git" {
		dir = filepath.Dir(dir)
	}
	return strings.TrimSuffix(filepath.Base(dir), ".git")
}

func (s *Session) Base() Base { return s.base }

// SetBase validates ref against HEAD and stores it. A chosen ref carries no fallback tag.
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

// Summary reads the session-level note from the database, not the row the session opened with.
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

// SetSummary replaces the session-level note. Empty clears it.
func (s *Session) SetSummary(ctx context.Context, text string) error {
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.db.SetSessionSummary(ctx, s.row.ID, text, now); err != nil {
		return err
	}
	s.row.Summary, s.row.UpdatedAt = text, now
	return nil
}

func (s *Session) load(ctx context.Context, head git.Head, opts Options) error {
	kind, branch, spec := identity(head)
	id := sessionID(s.repo.CommonDir(), kind, branch, spec)

	row, found, err := s.db.Session(ctx, id)
	if err != nil {
		return err
	}

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

	ref := row.BaseRef
	if base.Fallback == "" {
		ref = base.Ref
	}

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

// databasePath sits under the common dir so a worktree and its checkout share one database.
func databasePath(repo *git.Repo) string {
	return filepath.Join(repo.CommonDir(), "zen-review", "state.db")
}

func identity(head git.Head) (store.Kind, string, string) {
	if head.Branch == "" {
		return store.KindDetached, "", head.SHA
	}
	return store.KindBranch, head.Branch, ""
}

// sessionID is a hash, not the branch name, because sessions/foo and sessions/foo/bar cannot both be refs.
func sessionID(repoPath string, kind store.Kind, branch, spec string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{repoPath, string(kind), branch, spec}, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}
