package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrRefMoved means a ref was not where the caller said it was, which on a
// session ref means another instance advanced it first.
var ErrRefMoved = errors.New("the ref moved")

// unindexable matches how `add --ignore-errors` names a file it gave up on. It
// says so on stderr and nowhere else, and a snapshot that dropped a file without
// saying which is the failure this tool exists to prevent.
var unindexable = regexp.MustCompile(`unable to index file '(.*)'`)

// Signature is who a commit is attributed to. It is passed in because this
// package holds no opinion about what the commit is for.
type Signature struct {
	Name  string
	Email string
	When  time.Time
}

// vars are the six git reads instead of user.name and user.email. They override
// the config, which matters because `commit-tree` with no identity configured
// invents one from the hostname rather than failing.
func (s Signature) vars() []string {
	when := s.When.Format(time.RFC3339)
	return []string{
		"GIT_AUTHOR_NAME=" + s.Name,
		"GIT_AUTHOR_EMAIL=" + s.Email,
		"GIT_AUTHOR_DATE=" + when,
		"GIT_COMMITTER_NAME=" + s.Name,
		"GIT_COMMITTER_EMAIL=" + s.Email,
		"GIT_COMMITTER_DATE=" + when,
	}
}

// Snapshot is a tree of the work tree, and the paths that did not make it in.
type Snapshot struct {
	Tree string

	// Skipped names the files git could not read. A caller that ignores this is
	// presenting an incomplete snapshot as a whole one.
	Skipped []string
}

// SnapshotTree writes a tree holding HEAD overlaid with the work tree and every
// untracked file git is not ignoring, and returns it.
//
// The build runs against a temporary index, so the index the user and their
// agent are both using is never touched. It carries a pid because two instances
// on one repository each need their own.
//
// There is no pathspec. `add -A` from the root finds the same set, and a path
// list is fatal the moment a file the agent was mid-write disappears between the
// listing and the add.
func (r *Repo) SnapshotTree(ctx context.Context) (Snapshot, error) {
	// The index sits beside the database rather than in os.TempDir, because git
	// puts the lock next to it and both need the writable git directory the
	// caller already checked for.
	index := filepath.Join(r.commonDir, "zen-review", fmt.Sprintf("index.%d.tmp", os.Getpid()))
	if err := os.MkdirAll(filepath.Dir(index), 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("preparing the snapshot index: %w", err)
	}
	// A process killed mid-build leaves the lock behind, and git reads an existing
	// one as fatal for every build after it. Clearing on the way in is what makes
	// the next run recover on its own.
	if err := clearIndex(index); err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = clearIndex(index) }()

	in := invocation{extra: []string{"GIT_INDEX_FILE=" + index}}

	// A repository with no commit yet has nothing to read in, and the empty index
	// is left for `add` to fill.
	seed := []string{"read-tree", "--empty"}
	if has, err := r.hasCommits(ctx); err != nil {
		return Snapshot{}, err
	} else if has {
		seed = []string{"read-tree", "HEAD"}
	}
	if _, err := runIn(ctx, r.root, in, seed...); err != nil {
		return Snapshot{}, fmt.Errorf("seeding the snapshot index: %w", err)
	}

	// --ignore-errors turns one unreadable file from a fatal into a status of 1
	// and a usable index, which is the difference between a snapshot that names
	// what it missed and no snapshot at all.
	add := in
	add.allow = 1
	add.allowStderr = true
	res, err := runIn(ctx, r.root, add,
		"-c", "core.quotePath=false",
		"-c", "advice.addEmbeddedRepo=false",
		"--literal-pathspecs",
		"add", "-A", "--ignore-errors",
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshotting the work tree: %w", err)
	}

	tree, err := runIn(ctx, r.root, in, "write-tree")
	if err != nil {
		return Snapshot{}, fmt.Errorf("writing the snapshot tree: %w", err)
	}
	return Snapshot{Tree: trim(tree.stdout), Skipped: skipped(res.stderr)}, nil
}

// CommitTree writes a commit object. An empty parents makes a root commit.
func (r *Repo) CommitTree(ctx context.Context, tree string, parents []string, message string, sig Signature) (string, error) {
	args := []string{"commit-tree", tree}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-m", message)

	out, err := runIn(ctx, r.root, invocation{extra: sig.vars()}, args...)
	if err != nil {
		return "", fmt.Errorf("committing tree %s: %w", tree, err)
	}
	return trim(out.stdout), nil
}

// UpdateRef points ref at sha, and returns ErrRefMoved unless ref is currently
// at old. An empty old requires ref not to exist yet.
//
// The compare-and-swap is what stops two instances on one repository from
// interleaving their writes. git reports a lost race as a fatal carrying a
// message rather than a status of its own, so the message is what gets matched.
func (r *Repo) UpdateRef(ctx context.Context, ref, sha, old string) error {
	if _, err := run(ctx, r.root, "update-ref", "--end-of-options", ref, sha, old); err != nil {
		if strings.Contains(err.Error(), "cannot lock ref") {
			return fmt.Errorf("pointing %s at %s: %w", ref, sha, ErrRefMoved)
		}
		return fmt.Errorf("pointing %s at %s: %w", ref, sha, err)
	}
	return nil
}

// Tree is the tree a commit points at.
func (r *Repo) Tree(ctx context.Context, commit string) (string, error) {
	out, err := run(ctx, r.root, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("resolving the tree of %s: %w", commit, err)
	}
	return trim(out), nil
}

// hasCommits says whether HEAD resolves. --quiet exits 1 for a repository whose
// first commit has not landed, which is an answer rather than a failure.
func (r *Repo) hasCommits(ctx context.Context) (bool, error) {
	_, code, err := runStatus(ctx, r.root, 1, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return false, fmt.Errorf("checking whether HEAD has a commit: %w", err)
	}
	return code == 0, nil
}

// clearIndex removes a temporary index and the lock beside it.
func clearIndex(path string) error {
	for _, p := range []string{path, path + ".lock"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clearing the snapshot index at %s: %w", p, err)
		}
	}
	return nil
}

// skipped reads the paths add gave up on out of its stderr.
func skipped(stderr []byte) []string {
	var paths []string
	for _, m := range unindexable.FindAllStringSubmatch(string(stderr), -1) {
		paths = append(paths, m[1])
	}
	return paths
}
