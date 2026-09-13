package review

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/praxis-labs-io/zen-review/internal/git"
)

const headRef = "HEAD"

var defaultNames = []string{"main", "master"}

const (
	tagNoRemote    = "no remote"
	tagUncommitted = "uncommitted"
	tagStacked     = "stacked"
	tagDanglingFmt = "%s gone"
	tagNotFmt      = "not %s"
)

type Candidate struct {
	Branch string
	SHA    string

	// Ahead is how many commits HEAD has that the candidate does not.
	Ahead int
}

// BaseCandidates keeps local and remote apart because one name can exist in both.
type BaseCandidates struct {
	Local  []Candidate
	Remote []Candidate
}

// Candidates is every local branch but HEAD's, nearest first, plus the session's remote base and
// origin/HEAD. A branch with nothing behind HEAD is left out unless it is the base.
func (s *Session) Candidates(ctx context.Context) (BaseCandidates, error) {
	head, err := s.repo.Head(ctx)
	if err != nil {
		return BaseCandidates{}, err
	}
	if head.Unborn() {
		return BaseCandidates{}, nil
	}

	branches, err := s.repo.LocalBranches(ctx)
	if err != nil {
		return BaseCandidates{}, err
	}

	offered := make([]git.Branch, 0, len(branches))
	for _, b := range branches {
		if b.Name == head.Branch {
			continue
		}
		offered = append(offered, b)
	}

	locals, err := s.rank(ctx, head, offered)
	if err != nil {
		return BaseCandidates{}, err
	}

	offeredRemotes, err := s.remotes(ctx)
	if err != nil {
		return BaseCandidates{}, err
	}
	remotes, err := s.rank(ctx, head, offeredRemotes)
	if err != nil {
		return BaseCandidates{}, err
	}
	return BaseCandidates{Local: locals, Remote: remotes}, nil
}

func (s *Session) remotes(ctx context.Context) ([]git.Branch, error) {
	wanted := make([]string, 0, 2)
	if s.base.Ref != "" && s.base.Ref != headRef {
		wanted = append(wanted, s.base.Ref)
	}

	def, err := s.repo.DefaultRemoteBranch(ctx)
	if err != nil && !errors.Is(err, git.ErrNoDefaultBranch) {
		return nil, err
	}
	if def != "" && !slices.Contains(wanted, def) {
		wanted = append(wanted, def)
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	branches, err := s.repo.RemoteBranches(ctx)
	if err != nil {
		return nil, err
	}

	var offered []git.Branch
	for _, b := range branches {
		if slices.Contains(wanted, b.Name) {
			offered = append(offered, b)
		}
	}
	return offered, nil
}

func (s *Session) resolveBase(ctx context.Context, head git.Head, stored, flag string) (Base, error) {
	if head.Unborn() {
		tree, err := s.repo.EmptyTree(ctx)
		if err != nil {
			return Base{}, err
		}
		return Base{SHA: tree}, nil
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

func (s *Session) tryBase(ctx context.Context, ref, headSHA string) (Base, string, error) {
	tip, ok, err := s.repo.Resolve(ctx, ref)
	if err != nil {
		return Base{}, "", err
	}
	if !ok {
		return Base{}, fmt.Sprintf(tagNotFmt, ref), nil
	}

	sha, err := s.repo.MergeBase(ctx, tip, headSHA)
	if err != nil {
		if errors.Is(err, git.ErrNoMergeBase) {
			return Base{}, fmt.Sprintf(tagNotFmt, ref), nil
		}
		return Base{}, "", err
	}
	return Base{Ref: ref, SHA: sha}, "", nil
}

func (s *Session) rebase(ctx context.Context, head git.Head) error {
	was := s.base
	base, err := s.resolveBase(ctx, head, was.Ref, "")
	if err != nil {
		return err
	}

	if base.Ref == was.Ref && base.Fallback == "" {
		base.Fallback = was.Fallback
	}
	s.base = base
	return nil
}

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
		base.Fallback = tagOf(why, ref)
		return base, nil
	}

	return Base{}, fmt.Errorf("nothing in this repository to measure %s from", head.Branch)
}

func tagOf(why, ref string) string {
	if why == tagNoRemote && ref == headRef {
		return tagUncommitted
	}
	return why
}

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

	bound := rungs[0]
	if bound == headRef {
		if slices.Contains(defaultNames, head.Branch) {
			return rungs, why, nil
		}
		bound = ""
	}

	candidates, err := s.stack(ctx, head, bound)
	if err != nil {
		return nil, "", err
	}

	if len(candidates) > 0 {
		why = tagStacked
		rungs = append([]string{candidates[0].Branch}, rungs...)
	}
	return rungs, why, nil
}

func (s *Session) remoteDefault(ctx context.Context) (string, string, error) {
	detected, err := s.repo.DefaultRemoteBranch(ctx)
	if errors.Is(err, git.ErrNoDefaultBranch) {
		return "", tagNoRemote, nil
	}
	if err != nil {
		return "", "", err
	}

	_, ok, err := s.repo.Resolve(ctx, detected)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", fmt.Sprintf(tagDanglingFmt, detected), nil
	}
	return detected, "", nil
}

func (s *Session) localDefault(ctx context.Context, head git.Head) (string, error) {
	branches, err := s.repo.LocalBranches(ctx)
	if err != nil {
		return "", err
	}

	for _, name := range defaultNames {
		for _, b := range branches {
			if b.Name == name && b.Name != head.Branch {
				return name, nil
			}
		}
	}
	return "", nil
}

// stack walks HEAD's first-parent chain, so a branch merged into HEAD is not read as one it was cut from.
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

	offered := make([]git.Branch, 0, len(branches))
	for _, b := range branches {
		active := b.Name == s.base.Ref
		if b.Name == head.Branch || (!active && !mainline[b.SHA]) {
			continue
		}
		offered = append(offered, b)
	}
	return s.rank(ctx, head, offered)
}

func (s *Session) rank(ctx context.Context, head git.Head, branches []git.Branch) ([]Candidate, error) {
	var candidates []Candidate
	for _, b := range branches {
		ahead, err := s.repo.Ahead(ctx, b.SHA, head.SHA)
		if err != nil {
			return nil, err
		}

		if ahead == 0 && b.Name != s.base.Ref {
			continue
		}
		candidates = append(candidates, Candidate{Branch: b.Name, SHA: b.SHA, Ahead: ahead})
	}

	slices.SortFunc(candidates, func(a, b Candidate) int {
		if a.Ahead != b.Ahead {
			return a.Ahead - b.Ahead
		}
		return strings.Compare(a.Branch, b.Branch)
	})
	return candidates, nil
}
