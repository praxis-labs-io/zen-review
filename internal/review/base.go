package review

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zen-review/zen-review/internal/git"
)

// headRef is the bottom of the ladder: nothing above HEAD to measure from, so
// the changeset is whatever has not been committed.
const headRef = "HEAD"

// defaultNames are the branches a repository with no origin/HEAD falls back to,
// best first.
var defaultNames = []string{"main", "master"}

// Candidate is a local branch HEAD sits on top of, and how far back it is.
type Candidate struct {
	Branch string
	SHA    string

	// Ahead is how many commits HEAD has that the candidate does not.
	Ahead int
}

// resolveBase settles what the changeset is measured from: the flag, then what
// the session had, then the ladder. Nothing here refuses.
func (s *Session) resolveBase(ctx context.Context, head git.Head, stored, flag string) (Base, error) {
	// No commit to measure from, so the other side is the empty tree and every
	// file in the work tree reads as new. A ref cannot mean anything here.
	if head.Unborn() {
		tree, err := s.repo.EmptyTree(ctx)
		if err != nil {
			return Base{}, err
		}
		return Base{SHA: tree, Fallback: "this repository has no commits, so every file reads as new"}, nil
	}

	ref := flag
	if ref == "" {
		ref = stored
	}
	if ref == "" {
		return s.detect(ctx, head, "")
	}

	base, why, err := s.tryBase(ctx, ref, head.SHA)
	if err != nil {
		return Base{}, err
	}
	if why == "" {
		return base, nil
	}
	return s.detect(ctx, head, why)
}

// tryBase turns a ref into a fork point, or into the sentence saying why it is
// not one. Only a git that broke under us is an error.
func (s *Session) tryBase(ctx context.Context, ref, headSHA string) (Base, string, error) {
	tip, err := s.repo.RevParse(ctx, ref)
	if err != nil {
		return Base{}, fmt.Sprintf("%s does not resolve", ref), nil
	}

	// Against the commit that just resolved rather than the ref again. A ref is
	// mutable, and an agent in another worktree is entitled to move it.
	sha, err := s.repo.MergeBase(ctx, tip, headSHA)
	if err != nil {
		if errors.Is(err, git.ErrNoMergeBase) {
			return Base{}, fmt.Sprintf("%s shares no history with this branch", ref), nil
		}
		return Base{}, "", err
	}
	return Base{Ref: ref, SHA: sha}, "", nil
}

// rebase re-derives the fork point. base_ref is what sticks; base_sha follows
// the branch, or a rebase reads what it brought in as this branch's work.
func (s *Session) rebase(ctx context.Context, head git.Head) error {
	was := s.base
	base, err := s.resolveBase(ctx, head, was.Ref, "")
	if err != nil {
		return err
	}

	// The sentence belongs to the ref, and this only moved the sha under it.
	// Open stepped off a ref a second walk of the ladder cannot see.
	if base.Ref == was.Ref && base.Fallback == "" {
		base.Fallback = was.Fallback
	}
	s.base = base
	return nil
}

// detect walks the ladder and always reaches the bottom of it. why is what went
// wrong above it, and the first reason recorded is the one a reader can act on.
func (s *Session) detect(ctx context.Context, head git.Head, why string) (Base, error) {
	rungs, skipped, err := s.ladder(ctx, head)
	if err != nil {
		return Base{}, err
	}
	if why == "" {
		why = skipped
	}

	for _, ref := range rungs {
		base, fell, err := s.tryBase(ctx, ref, head.SHA)
		if err != nil {
			return Base{}, err
		}
		if fell != "" {
			if why == "" {
				why = fell
			}
			continue
		}
		base.Fallback = fallbackOf(why, ref)
		return base, nil
	}

	// Unreachable: HEAD ends every ladder, and a HEAD that is not unborn
	// resolves and is its own merge base.
	return Base{}, fmt.Errorf("nothing in this repository to measure %s from", head.Branch)
}

// ladder is every ref detection will try, best first, and why the ones missing
// from it were passed over.
func (s *Session) ladder(ctx context.Context, head git.Head) ([]string, string, error) {
	remote, why, err := s.remoteDefault(ctx)
	if err != nil {
		return nil, "", err
	}

	rungs := make([]string, 0, 3)
	if remote != "" {
		rungs = append(rungs, remote)
	}

	local, err := s.localDefault(ctx, head)
	if err != nil {
		return nil, "", err
	}
	if local != "" {
		rungs = append(rungs, local)
	}
	rungs = append(rungs, headRef)

	// A branch stacked on another local branch is not measured from what sits
	// under both: that reads the parent's commits as this branch's work.
	if rungs[0] != headRef {
		candidates, err := s.stack(ctx, head, rungs[0])
		if err != nil {
			return nil, "", err
		}
		if len(candidates) > 0 {
			// Names only what it passed over. fallbackOf names what it took.
			if why == "" {
				why = fmt.Sprintf("%s is not the fork point of a branch stacked on %s",
					rungs[0], candidates[0].Branch)
			}
			rungs = append([]string{candidates[0].Branch}, rungs...)
		}
	}
	return rungs, why, nil
}

// remoteDefault is origin/HEAD where it is set and still names something, and
// the reason it is not otherwise.
func (s *Session) remoteDefault(ctx context.Context) (string, string, error) {
	detected, err := s.repo.DefaultRemoteBranch(ctx)
	if errors.Is(err, git.ErrNoDefaultBranch) {
		return "", "this repository has no origin/HEAD", nil
	}
	if err != nil {
		return "", "", err
	}

	// origin/HEAD is symbolic and outlives what it points at: rename the
	// remote's default branch and it names a ref that is gone.
	if _, err := s.repo.RevParse(ctx, detected); err != nil {
		return "", fmt.Sprintf("origin/HEAD points at %s, which is gone", detected), nil
	}
	return detected, "", nil
}

// localDefault is a local main or master to fall back to, and empty when there
// is none or it is the branch HEAD is already on.
func (s *Session) localDefault(ctx context.Context, head git.Head) (string, error) {
	branches, err := s.repo.LocalBranches(ctx)
	if err != nil {
		return "", err
	}

	for _, name := range defaultNames {
		for _, b := range branches {
			// The branch HEAD is on gives HEAD back, which the bottom rung
			// already says more plainly.
			if b.Name == name && b.Name != head.Branch {
				return name, nil
			}
		}
	}
	return "", nil
}

// fallbackOf is the sentence a base carries when it is not the one a reader
// would expect, and empty when nothing was passed over.
func fallbackOf(why, ref string) string {
	if why == "" {
		return ""
	}
	if ref == headRef {
		return why + ", so the changeset is what has not been committed"
	}
	return fmt.Sprintf("%s, so the base is %s", why, ref)
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
