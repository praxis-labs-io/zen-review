// Package testrepo builds real git repositories for tests.
//
// Nothing here is mocked. What the packages above test is whether git is being
// called correctly, and a fake would answer a different question.
//
// It is test-only and imported by test files alone, so it never reaches the
// binary. It imports nothing of this module, which is what lets internal/git's
// own tests use it without a cycle.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a git repository in a temporary directory, removed when the test ends.
type Repo struct {
	t   *testing.T
	dir string
}

// New initialises a repository on branch main with an identity configured.
func New(t *testing.T) *Repo {
	t.Helper()

	// The temporary root is resolved through its symlinks, because rev-parse
	// answers with the physical path and macOS hands out /var pointing at
	// /private/var. Comparing against the unresolved path fails for a reason that
	// has nothing to do with the code under test.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}

	// The repository sits in a subdirectory, so a test that needs somewhere to put
	// a linked worktree has the room beside it.
	r := &Repo{t: t, dir: filepath.Join(root, "repo")}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatalf("creating the repository directory: %v", err)
	}

	r.Git("init", "-b", "main")
	r.Git("config", "user.name", "Test")
	r.Git("config", "user.email", "test@example.com")
	return r
}

// Dir is the work tree root.
func (r *Repo) Dir() string { return r.dir }

// Git runs a command in the repository and fails the test unless it succeeds.
//
// The dates are pinned so a commit's sha depends only on its content, which is
// what lets a test assert on one.
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

// TrackOrigin writes the refs a clone leaves behind, pinning origin/main to ref
// and pointing origin/HEAD at it. Base detection has something to resolve
// without a network.
func (r *Repo) TrackOrigin(ref string) {
	r.t.Helper()

	r.Git("update-ref", "refs/remotes/origin/main", r.Git("rev-parse", ref))
	r.Git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
}

// Main runs a package's tests with git's global and system config pointed at
// os.DevNull, so a developer's commit.gpgsign, core.hooksPath or diff settings
// cannot decide whether the suite passes.
//
// It has to be the process environment rather than a command's, because the code
// under test builds its own environment from os.Environ. Production deliberately
// keeps reading real config: .gitignore and .gitattributes depend on it.
//
// Call it from TestMain: os.Exit(testrepo.Main(m)).
func Main(m *testing.M) int {
	for _, key := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(key, os.DevNull); err != nil {
			panic(err)
		}
	}
	return m.Run()
}
