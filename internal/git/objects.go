package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// ErrRefMoved means a ref was not where the caller said it was, which on a
// session ref means another instance advanced it first.
var ErrRefMoved = errors.New("the ref moved")

// unreadable matches the three ways `add` says it gave up on something. It says
// so on stderr and nowhere else, and a snapshot that dropped a file without
// saying which is the failure this tool exists to prevent.
//
// The directory case is the quieter one: it is a warning and the status stays 0,
// so every file underneath disappears from the tree with nothing else to show
// for it.
//
// The third puts the path first, which is why it is a second alternative with
// its own group rather than another name in the first. It is a directory that is
// a git repository with an unborn HEAD, which an agent leaves behind every time
// it runs `git init` and has not committed yet. Without it that stderr matches
// nothing, the status of 1 reads as a failure with no path named, and every
// command refuses to run at all.
var unreadable = regexp.MustCompile(`(?:unable to index file|could not open directory) '([^\n]*)'` +
	`|'([^\n]*)' does not have a commit checked out`)

// refMismatch is how git says a ref was not where the caller said it was. The
// wider "cannot lock ref" it comes wrapped in also covers a lock left by a
// crashed process, which is not a race and will not clear on a retry.
var refMismatch = regexp.MustCompile(`is at [0-9a-f]+ but expected|reference already exists`)

// staleIndex is how long a temporary index sits untouched before another build
// reads it as abandoned. Well past the slowest snapshot, because the cost of
// guessing early is deleting an index a live build is still writing.
const staleIndex = time.Hour

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
	dir := filepath.Join(r.commonDir, "zen-review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("preparing the snapshot index: %w", err)
	}
	sweepIndexes(dir)

	// The name is unique per call, not per process. A Repo value is safe to call
	// from more than one goroutine, and two builds sharing a path would clear each
	// other's index halfway through and write a tree neither of them meant.
	f, err := os.CreateTemp(dir, "index-*.tmp")
	if err != nil {
		return Snapshot{}, fmt.Errorf("preparing the snapshot index: %w", err)
	}
	index := f.Name()
	if err := f.Close(); err != nil {
		return Snapshot{}, fmt.Errorf("preparing the snapshot index: %w", err)
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

	// A status of 1 is add saying it gave up on something, and it names what on
	// the same breath. One with nothing named is a different failure, and handing
	// it back as a snapshot would hide a missing file behind an empty Skipped.
	skipped := skipped(res.stderr)
	if res.code == 1 && len(skipped) == 0 {
		return Snapshot{}, fmt.Errorf("snapshotting the work tree: %s", stderrOf(res.stderr))
	}

	tree, err := runIn(ctx, r.root, in, "write-tree")
	if err != nil {
		return Snapshot{}, fmt.Errorf("writing the snapshot tree: %w", err)
	}
	return Snapshot{Tree: trim(tree.stdout), Skipped: skipped}, nil
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
		if refMismatch.MatchString(err.Error()) {
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

// sweepIndexes removes what processes killed mid-build left behind. Nothing else
// ever comes back for those files, and each one is the size of the work tree.
//
// It is deliberately quiet: a file a live build is still using is too young to
// match, and one that will not delete is not worth failing a snapshot over.
func sweepIndexes(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "index-*.tmp*"))
	if err != nil {
		return
	}
	for _, path := range matches {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > staleIndex {
			_ = os.Remove(path)
		}
	}
}

// skipped reads the paths add gave up on out of its stderr, in the order git
// reported them.
func skipped(stderr []byte) []string {
	var paths []string
	for _, m := range unreadable.FindAllStringSubmatch(string(stderr), -1) {
		// One alternative matched, so one of the two groups holds the path and the
		// other is empty. A path cannot be, so this tells them apart.
		if m[1] != "" {
			paths = append(paths, m[1])
			continue
		}
		paths = append(paths, m[2])
	}
	return paths
}
