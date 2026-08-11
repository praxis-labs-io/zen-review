package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points git's global and system config at os.DevNull for the whole test
// process, so a developer's commit.gpgsign, core.hooksPath or diff settings
// cannot decide whether the suite passes. It has to be the process environment
// rather than the fixture's, because the code under test builds its own env from
// os.Environ.
//
// Production deliberately keeps reading real config: .gitignore and
// .gitattributes behaviour depends on it.
func TestMain(m *testing.M) {
	for _, key := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(key, os.DevNull); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

// fixture is a real repository in a temporary directory. Nothing here is mocked:
// what these tests check is whether git is being called correctly.
type fixture struct {
	t   *testing.T
	dir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	// The temporary root is resolved through its symlinks, because rev-parse
	// answers with the physical path and macOS hands out /var pointing at
	// /private/var. Comparing against the unresolved path fails for a reason that
	// has nothing to do with the code.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}

	f := &fixture{t: t, dir: filepath.Join(root, "repo")}
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		t.Fatalf("creating the fixture directory: %v", err)
	}

	f.git("init", "-b", "main")
	f.git("config", "user.name", "Test")
	f.git("config", "user.email", "test@example.com")
	return f
}

// git runs a git command in the fixture and fails the test unless it succeeds.
func (f *fixture) git(args ...string) string {
	f.t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// write creates or replaces a file, making any directory it needs.
func (f *fixture) write(path, content string) {
	f.t.Helper()

	full := filepath.Join(f.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatalf("creating the directory for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		f.t.Fatalf("writing %s: %v", path, err)
	}
}

// commit stages everything and commits it.
func (f *fixture) commit(message string) string {
	f.t.Helper()

	f.git("add", "-A")
	f.git("commit", "-m", message)
	return f.git("rev-parse", "HEAD")
}

// open is the Repo under test, opened on the fixture.
func (f *fixture) open() *Repo {
	f.t.Helper()

	repo, err := Open(f.t.Context(), f.dir)
	if err != nil {
		f.t.Fatalf("opening the fixture: %v", err)
	}
	return repo
}
