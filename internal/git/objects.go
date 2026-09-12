package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrRefMoved = errors.New("the ref moved")

var unreadable = regexp.MustCompile(`(?:unable to index file|could not open directory) '([^\n]*)'` +
	`|'([^\n]*)' does not have a commit checked out`)

// refMismatch skips a bare "cannot lock ref", which a crashed process's lock also produces and no retry clears.
var refMismatch = regexp.MustCompile(`is at [0-9a-f]+ but expected|reference already exists`)

// staleIndex is far past the slowest snapshot, because sweeping early deletes a live build's index.
const staleIndex = time.Hour

type Signature struct {
	Name  string
	Email string
	When  time.Time
}

// vars override config because commit-tree with no identity configured invents one from the hostname.
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

type Snapshot struct {
	Tree string

	// Skipped names the files git could not read. A tracked one keeps its HEAD content in Tree.
	Skipped []string
}

// SnapshotTree writes a tree of HEAD overlaid with the work tree and untracked files, leaving the real index untouched.
func (r *Repo) SnapshotTree(ctx context.Context) (Snapshot, error) {
	dir := filepath.Join(r.commonDir, "zen-review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("preparing the snapshot index: %w", err)
	}
	sweepIndexes(dir)

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

	seed := []string{"read-tree", "--empty"}
	if has, err := r.hasCommits(ctx); err != nil {
		return Snapshot{}, err
	} else if has {
		seed = []string{"read-tree", "HEAD"}
	}
	if _, err := runIn(ctx, r.root, in, seed...); err != nil {
		return Snapshot{}, fmt.Errorf("seeding the snapshot index: %w", err)
	}

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

func (r *Repo) EmptyTree(ctx context.Context) (string, error) {
	out, err := run(ctx, r.root, "mktree")
	if err != nil {
		return "", fmt.Errorf("writing the empty tree: %w", err)
	}
	return trim(out), nil
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

// UpdateRef points ref at sha, ErrRefMoved unless ref is at old. An empty old
// requires no ref yet; an empty sha deletes, so a swap can be put back.
func (r *Repo) UpdateRef(ctx context.Context, ref, sha, old string) error {
	what := "pointing " + ref + " at " + sha
	args := []string{"update-ref", "--end-of-options", ref, sha, old}
	if sha == "" {
		what = "deleting " + ref
		args = []string{"update-ref", "-d", "--end-of-options", ref, old}
	}

	if _, err := run(ctx, r.root, args...); err != nil {
		if refMismatch.MatchString(err.Error()) {
			return fmt.Errorf("%s: %w", what, ErrRefMoved)
		}
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func (r *Repo) Tree(ctx context.Context, commit string) (string, error) {
	out, err := run(ctx, r.root, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("resolving the tree of %s: %w", commit, err)
	}
	return trim(out), nil
}

// Blobs reads several blobs in one git process, keyed by sha. A sha the
// repository does not have, or that names anything else, is left out.
func (r *Repo) Blobs(ctx context.Context, shas []string) (map[string][]byte, error) {
	seen := make(map[string]bool, len(shas))
	want := make([]string, 0, len(shas))
	for _, sha := range shas {
		if sha == "" || seen[sha] {
			continue
		}
		seen[sha] = true
		want = append(want, sha)
	}
	if len(want) == 0 {
		return map[string][]byte{}, nil
	}

	in := invocation{stdin: []byte(strings.Join(want, "\n") + "\n")}
	res, err := runIn(ctx, r.root, in, "cat-file", "--batch")
	if err != nil {
		return nil, fmt.Errorf("reading %d blobs: %w", len(want), err)
	}
	blobs, err := batched(res.stdout, len(want))
	if err != nil {
		return nil, fmt.Errorf("reading %d blobs: %w", len(want), err)
	}
	return blobs, nil
}

func batched(out []byte, want int) (map[string][]byte, error) {
	blobs := make(map[string][]byte, want)
	for len(out) > 0 {
		nl := bytes.IndexByte(out, '\n')
		if nl < 0 {
			return nil, fmt.Errorf("a header with no newline after it: %q", out)
		}
		header := string(out[:nl])
		out = out[nl+1:]

		f := strings.Fields(header)
		switch {
		case len(f) == 2:
			continue
		case len(f) != 3:
			return nil, fmt.Errorf("%q is neither an object nor a miss", header)
		}

		size, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fmt.Errorf("%s is sized %q: %w", f[0], f[2], err)
		}
		if size < 0 || size+1 > len(out) {
			return nil, fmt.Errorf("%s is sized %d and %d bytes followed it", f[0], size, len(out))
		}
		if f[1] == "blob" {
			blobs[f[0]] = out[:size]
		}
		out = out[size+1:]
	}
	return blobs, nil
}

func (r *Repo) hasCommits(ctx context.Context) (bool, error) {
	_, code, err := runStatus(ctx, r.root, 1, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return false, fmt.Errorf("checking whether HEAD has a commit: %w", err)
	}
	return code == 0, nil
}

func clearIndex(path string) error {
	for _, p := range []string{path, path + ".lock"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clearing the snapshot index at %s: %w", p, err)
		}
	}
	return nil
}

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

func skipped(stderr []byte) []string {
	var paths []string
	seen := map[string]bool{}

	for _, m := range unreadable.FindAllStringSubmatch(string(stderr), -1) {
		path := m[1]
		if path == "" {
			path = m[2]
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}
