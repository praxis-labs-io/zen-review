package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrNoMergeBase means two commits share no history at all, which is what a
	// base force-push that loses the fork point looks like from here.
	ErrNoMergeBase = errors.New("no common ancestor")

	// ErrNoDefaultBranch means origin/HEAD is unset: no remote, or a clone that
	// never learned which branch the remote considers default.
	ErrNoDefaultBranch = errors.New("origin/HEAD is not set")
)

// Head is what HEAD points at. Branch is empty on a detached HEAD, which a
// session keys on the sha instead of a name.
type Head struct {
	Branch string
	SHA    string
}

// Unborn is a HEAD with no commit under it: git init, nothing committed. The
// branch name is still there, so a session keys on it as it always does.
func (h Head) Unborn() bool { return h.SHA == "" }

// Branch is one local branch and the commit it points at.
type Branch struct {
	Name string
	SHA  string
}

// Head resolves HEAD to a branch name and a commit. A repository with no
// commits answers Unborn rather than failing.
func (r *Repo) Head(ctx context.Context) (Head, error) {
	// The only rev HEAD does not resolve to is the one never committed, and
	// --verify --quiet exits 1 for it rather than printing a fatal.
	sha, code, err := runStatus(ctx, r.root, 1, "rev-parse", "--verify", "--quiet", "--end-of-options", "HEAD^{commit}")
	if err != nil {
		return Head{}, fmt.Errorf("resolving HEAD: %w", err)
	}

	var head Head
	if code == 0 {
		head.SHA = trim(sha)
	}

	// --quiet exits 1 on a detached HEAD rather than printing a fatal, which is
	// an answer and not a failure.
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

// RevParse resolves a ref to a full commit sha, peeling a tag to the commit it
// names. It errors on anything that does not resolve, so a caller never gets an
// empty string back for a ref that does not exist.
func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	out, err := run(ctx, r.root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}
	return trim(out), nil
}

// Resolve is RevParse for a caller with an answer for a ref that names nothing.
// Only a git that failed for some other reason is an error.
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

// RefSha is the object a ref points at, and false for a ref that does not
// exist. Absence is an answer here and not a failure: a session that has never
// refreshed has no ref yet, and that is its normal first state.
//
// It does not peel. The value is what a compare-and-swap has to be given, and
// update-ref compares against what the ref holds rather than what it resolves
// to.
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

// MergeBase is the best common ancestor of two commits: the fork point a
// changeset is measured from.
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

// FirstParents lists the commits from base to tip along first parents only,
// newest first and excluding base itself.
//
// The first-parent chain is what separates "branched from" from "merged in". A
// side branch merged with --no-ff arrives as a merge's second parent and is not
// on it, while a branch HEAD was cut from is. Walking every parent instead reads
// both as the same thing, and one of them is a base nobody would measure from.
func (r *Repo) FirstParents(ctx context.Context, base, tip string) ([]string, error) {
	// An empty base walks the whole chain, for a tip with nothing above it to
	// bound the walk.
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

// Ahead is how many commits tip has that base does not.
//
// Sorting stack candidates nearest first is what it is for: of two branches
// HEAD sits on top of, the one fewer commits back is the one it was branched
// from. It errors on a ref that does not resolve rather than answering 0, which
// would read as "no distance" and sort a typo to the front.
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

// DefaultRemoteBranch is what origin/HEAD points at, usually "origin/main". It
// beats local main as a base proposal: in an agentic workflow local main is never
// checked out and goes stale within a day.
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

// LocalBranches lists refs/heads with the commit each one points at.
func (r *Repo) LocalBranches(ctx context.Context) ([]Branch, error) {
	// A branch name can hold neither a NUL nor a newline, so this format parses
	// exactly.
	out, err := run(ctx, r.root, "for-each-ref", "--format=%(refname:short)%00%(objectname)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("listing local branches: %w", err)
	}

	var branches []Branch
	for _, line := range strings.Split(trim(out), "\n") {
		name, sha, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		branches = append(branches, Branch{Name: name, SHA: sha})
	}
	return branches, nil
}
