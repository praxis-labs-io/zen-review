package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrNoMergeBase = errors.New("no common ancestor")

	ErrNoDefaultBranch = errors.New("origin/HEAD is not set")
)

type Head struct {
	// Branch is empty on a detached HEAD.
	Branch string
	SHA    string
}

func (h Head) Unborn() bool { return h.SHA == "" }

type Branch struct {
	Name string
	SHA  string
}

// Head resolves HEAD. A repository with no commits answers Unborn rather than failing.
func (r *Repo) Head(ctx context.Context) (Head, error) {
	sha, code, err := runStatus(ctx, r.root, 1, "rev-parse", "--verify", "--quiet", "--end-of-options", "HEAD^{commit}")
	if err != nil {
		return Head{}, fmt.Errorf("resolving HEAD: %w", err)
	}

	var head Head
	if code == 0 {
		head.SHA = trim(sha)
	}

	out, code, err := runStatus(ctx, r.root, 1, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return Head{}, err
	}
	if code == 1 {
		return head, nil
	}
	head.Branch = trim(out)
	return head, nil
}

// RevParse resolves ref to a commit sha, peeling tags, and errors when it does not resolve.
func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	out, err := run(ctx, r.root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}
	return trim(out), nil
}

// Resolve is RevParse returning false for a ref that names nothing.
func (r *Repo) Resolve(ctx context.Context, ref string) (string, bool, error) {
	out, code, err := runStatus(ctx, r.root, 1,
		"rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", false, fmt.Errorf("resolving %s: %w", ref, err)
	}
	if code == 1 {
		return "", false, nil
	}
	return trim(out), true, nil
}

// RefSha is the unpeeled object ref points at, or false when the ref does not exist.
func (r *Repo) RefSha(ctx context.Context, ref string) (string, bool, error) {
	out, code, err := runStatus(ctx, r.root, 1, "rev-parse", "--verify", "--quiet", "--end-of-options", ref)
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", ref, err)
	}
	if code == 1 {
		return "", false, nil
	}
	return trim(out), true, nil
}

// MergeBase wraps ErrNoMergeBase when a and b share no history.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, code, err := runStatus(ctx, r.root, 1, "merge-base", "--end-of-options", a, b)
	if err != nil {
		return "", fmt.Errorf("finding the merge base of %s and %s: %w", a, b, err)
	}
	if code == 1 {
		return "", fmt.Errorf("finding the merge base of %s and %s: %w", a, b, ErrNoMergeBase)
	}
	return trim(out), nil
}

// FirstParents lists the first-parent commits from base to tip, newest first, excluding base. An empty base walks the whole chain.
func (r *Repo) FirstParents(ctx context.Context, base, tip string) ([]string, error) {
	rev := tip
	if base != "" {
		rev = base + ".." + tip
	}

	out, err := run(ctx, r.root, "rev-list", "--first-parent", "--end-of-options", rev)
	if err != nil {
		return nil, fmt.Errorf("walking the first parents to %s: %w", tip, err)
	}

	line := trim(out)
	if line == "" {
		return nil, nil
	}
	return strings.Split(line, "\n"), nil
}

func (r *Repo) Ahead(ctx context.Context, base, tip string) (int, error) {
	out, err := run(ctx, r.root, "rev-list", "--count", "--end-of-options", base+".."+tip)
	if err != nil {
		return 0, fmt.Errorf("counting the commits %s has beyond %s: %w", tip, base, err)
	}

	n, err := strconv.Atoi(trim(out))
	if err != nil {
		return 0, fmt.Errorf("counting the commits %s has beyond %s: rev-list answered %q", tip, base, trim(out))
	}
	return n, nil
}

// DefaultRemoteBranch is what origin/HEAD points at, such as "origin/main", or ErrNoDefaultBranch.
func (r *Repo) DefaultRemoteBranch(ctx context.Context) (string, error) {
	out, code, err := runStatus(ctx, r.root, 1, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", fmt.Errorf("reading origin/HEAD: %w", err)
	}
	if code == 1 {
		return "", ErrNoDefaultBranch
	}
	return trim(out), nil
}

func (r *Repo) LocalBranches(ctx context.Context) ([]Branch, error) {
	return r.branches(ctx, "local", "refs/heads")
}

// RemoteBranches lists remote-tracking branches, leaving out symbolic refs such as origin/HEAD.
func (r *Repo) RemoteBranches(ctx context.Context) ([]Branch, error) {
	return r.branches(ctx, "remote", "refs/remotes")
}

func (r *Repo) branches(ctx context.Context, kind, namespace string) ([]Branch, error) {
	out, err := run(ctx, r.root, "for-each-ref", "--format=%(refname:short)%00%(objectname)%00%(symref)", namespace)
	if err != nil {
		return nil, fmt.Errorf("listing %s branches: %w", kind, err)
	}

	var branches []Branch
	for _, line := range strings.Split(trim(out), "\n") {
		name, rest, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		sha, symbolic, ok := strings.Cut(rest, "\x00")
		if !ok || symbolic != "" {
			continue
		}
		branches = append(branches, Branch{Name: name, SHA: sha})
	}
	return branches, nil
}
