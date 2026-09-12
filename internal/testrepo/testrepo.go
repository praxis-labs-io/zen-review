// Package testrepo builds real git repositories for tests. It imports nothing of this module,
// so internal/git can use it.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type Repo struct {
	t   *testing.T
	dir string
}

// New initialises a repository on branch main with an identity, removed when the test ends.
func New(t *testing.T) *Repo {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}

	r := &Repo{t: t, dir: filepath.Join(root, "repo")}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatalf("creating the repository directory: %v", err)
	}

	r.Git("init", "-b", "main")
	r.Git("config", "user.name", "Test")
	r.Git("config", "user.email", "test@example.com")
	return r
}

func (r *Repo) Dir() string { return r.dir }

// Git runs git in the repository and fails the test on error. Dates are pinned, so a commit's
// sha depends only on its content.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// Write creates or replaces a file, making any directory it needs.
func (r *Repo) Write(path, content string) {
	r.t.Helper()

	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("creating the directory for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("writing %s: %v", path, err)
	}
}

// Commit stages everything and commits it, returning the new sha.
func (r *Repo) Commit(message string) string {
	r.t.Helper()

	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", message)
	return r.Git("rev-parse", "HEAD")
}

// TrackOrigin points origin/main and origin/HEAD at ref, as a clone would.
func (r *Repo) TrackOrigin(ref string) {
	r.t.Helper()

	r.Git("update-ref", "refs/remotes/origin/main", r.Git("rev-parse", ref))
	r.Git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
}

// Main runs m with git's global and system config pointed at os.DevNull.
// Call it as os.Exit(testrepo.Main(m)).
func Main(m *testing.M) int {
	for _, key := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(key, os.DevNull); err != nil {
			panic(err)
		}
	}
	return m.Run()
}
