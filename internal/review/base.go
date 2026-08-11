package review

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zen-review/zen-review/internal/git"
)

// Candidate is a local branch HEAD sits on top of, and how far back it is.
type Candidate struct {
	Branch string
	SHA    string

	// Ahead is how many commits HEAD has that the candidate does not.
	Ahead int
}

// StackedError means HEAD sits on top of another local branch and no base has
// been chosen yet.
//
// It is typed because the CLI prints it and the base picker on `b` reads the
// same list. Measuring a stacked branch from origin/main shows the parent
// branch's commits as this branch's work, which is a worse answer than asking.
type StackedError struct {
	// Detected is what auto-detection would otherwise have taken.
	Detected string

	// Candidates are nearest first.
	Candidates []Candidate
}

func (e *StackedError) Error() string {
	names := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		names = append(names, c.Branch)
	}
	return fmt.Sprintf("this branch sits on top of %s, so %s is not the fork point: pass --base with one of them",
		strings.Join(names, ", "), e.Detected)
}

// NoMergeBaseError means a base and HEAD share no history, which is what a base
// force-push that loses the fork point looks like from here.
type NoMergeBaseError struct{ Ref string }

func (e *NoMergeBaseError) Error() string {
	return fmt.Sprintf("%s and HEAD share no history, so the fork point is gone: pass --base with a ref this branch still grows from", e.Ref)
}

// UnresolvableBaseError means the base this session was measured from no longer
// names anything.
type UnresolvableBaseError struct{ Ref string }

func (e *UnresolvableBaseError) Error() string {
	return fmt.Sprintf("this session is measured from %s, which no longer resolves: pass --base to name another", e.Ref)
}

// resolveBase settles what the changeset is measured from: the flag, then what
// the session already had, then detection.
//
// Detection is the only step that can refuse, and it runs only for a session
// with nothing stored. A base chosen once is used again even where detection
// would now say something else, because a base moving under a half-finished
// review changes what every range already reviewed was measured against.
func (s *Session) resolveBase(ctx context.Context, head git.Head, stored, flag string) (Base, error) {
	ref, fromStore := flag, false
	if ref == "" && stored != "" {
		ref, fromStore = stored, true
	}
	if ref == "" {
		found, err := s.detect(ctx, head)
		if err != nil {
			return Base{}, err
		}
		ref = found
	}

	return s.mergeBase(ctx, ref, head.SHA, fromStore)
}

// mergeBase turns a ref into a fork point, and the two ways that goes wrong
// into errors the command above can print.
//
// stored says the ref came from the session rather than from the reader. A
// stored ref that no longer names anything is said out loud rather than
// replaced by a fresh detection: falling back silently changes what the review
// is measuring, and everything already reviewed was measured from the ref that
// just went away.
func (s *Session) mergeBase(ctx context.Context, ref, headSHA string, stored bool) (Base, error) {
	tip, err := s.repo.RevParse(ctx, ref)
	if err != nil {
		if stored {
			return Base{}, &UnresolvableBaseError{Ref: ref}
		}
		return Base{}, err
	}

	// The merge base runs against the commit that just resolved, not the ref
	// again. A ref is mutable and an agent in another worktree is entitled to
	// move it, which would leave the changeset measured from a commit nothing
	// here ever checked. The name stays only as what the session stores and
	// prints.
	sha, err := s.repo.MergeBase(ctx, tip, headSHA)
	if err != nil {
		if errors.Is(err, git.ErrNoMergeBase) {
			return Base{}, &NoMergeBaseError{Ref: ref}
		}
		return Base{}, err
	}
	return Base{Ref: ref, SHA: sha}, nil
}

// rebase re-derives the fork point from the ref the session already settled on.
//
// base_ref is what sticks; base_sha follows the branch. A rebase onto a newer
// origin/main moves the fork point, and measuring from the old one shows every
// commit the rebase brought in as this branch's work.
func (s *Session) rebase(ctx context.Context, head git.Head) error {
	base, err := s.mergeBase(ctx, s.base.Ref, head.SHA, true)
	if err != nil {
		return err
	}
	s.base = base
	return nil
}

// detect proposes a base, and refuses rather than guess on a stacked branch.
func (s *Session) detect(ctx context.Context, head git.Head) (string, error) {
	detected, err := s.repo.DefaultRemoteBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrNoDefaultBranch) {
			return "", errors.New("no origin/HEAD to measure from, so pass --base <ref>")
		}
		return "", err
	}

	// origin/HEAD is a symbolic ref and outlives what it points at: rename the
	// remote's default branch and it names a ref that is gone. Checking here
	// rather than after the stack walk is what keeps that arriving as guidance
	// instead of a merge-base fatal about an object name.
	if _, err := s.repo.RevParse(ctx, detected); err != nil {
		return "", fmt.Errorf("origin/HEAD points at %s, which no longer exists, so pass --base <ref>", detected)
	}

	candidates, err := s.stack(ctx, head, detected)
	if err != nil {
		return "", err
	}
	if len(candidates) > 0 {
		return "", &StackedError{Detected: detected, Candidates: candidates}
	}
	return detected, nil
}

// stack is every local branch HEAD was branched from, nearest first.
//
// A tip qualifies when it sits on HEAD's first-parent chain above the detected
// base, which is one `rev-list` for the whole question rather than two
// ancestry calls per branch. A checkout in an agentic workflow accumulates
// branches, and 200 of them was 600 processes before the first frame.
//
// The chain is doing two jobs. Being above the base is what keeps a local main
// left behind origin/main out: it is an ancestor of HEAD and nothing anyone
// stacked on, and where local main is never checked out that is the common case
// rather than the odd one. Being on the *first-parent* chain is what keeps a
// branch merged into HEAD out: `git merge --no-ff side` leaves side an ancestor
// of HEAD and not of the base, so ancestry alone reads it as a stack and
// measuring from it gives a changeset nobody asked for.
//
// The current branch is excluded because HEAD is trivially on top of itself, and
// a tip sitting exactly at HEAD is excluded because measuring from it leaves an
// empty changeset.
func (s *Session) stack(ctx context.Context, head git.Head, detected string) ([]Candidate, error) {
	chain, err := s.repo.FirstParents(ctx, detected, head.SHA)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, nil
	}

	mainline := make(map[string]bool, len(chain))
	for _, sha := range chain {
		mainline[sha] = true
	}

	branches, err := s.repo.LocalBranches(ctx)
	if err != nil {
		return nil, err
	}

	var candidates []Candidate
	for _, b := range branches {
		if b.Name == head.Branch || b.SHA == head.SHA || !mainline[b.SHA] {
			continue
		}

		ahead, err := s.repo.Ahead(ctx, b.SHA, head.SHA)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, Candidate{Branch: b.Name, SHA: b.SHA, Ahead: ahead})
	}

	// Nearest first: of two branches HEAD sits on, the one fewer commits back is
	// the one it was branched from. Ties break on name, so the order a reader
	// sees does not depend on how git happened to list the refs.
	slices.SortFunc(candidates, func(a, b Candidate) int {
		if a.Ahead != b.Ahead {
			return a.Ahead - b.Ahead
		}
		return strings.Compare(a.Branch, b.Branch)
	})
	return candidates, nil
}
