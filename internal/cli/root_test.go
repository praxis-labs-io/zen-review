package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/cli"
	"github.com/zen-review/zen-review/internal/version"
)

// TestMain points git's global and system config at os.DevNull, so a developer's
// commit.gpgsign or diff settings cannot decide whether the suite passes.
func TestMain(m *testing.M) {
	for _, key := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(key, os.DevNull); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

// repo is a real repository the command is run inside. It is deliberately not the
// helper internal/git tests with: the two need different things, and a shared
// fixture package earns its place once a third caller wants one.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()

	// rev-parse answers with the physical path, and macOS hands out /var pointing
	// at /private/var.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}

	r := &repo{t: t, dir: dir}
	r.git("init", "-b", "main")
	r.git("config", "user.name", "Test")
	r.git("config", "user.email", "test@example.com")
	return r
}

func (r *repo) git(args ...string) string {
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

func (r *repo) write(path, content string) {
	r.t.Helper()

	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("creating the directory for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("writing %s: %v", path, err)
	}
}

func (r *repo) commit(message string) string {
	r.t.Helper()

	r.git("add", "-A")
	r.git("commit", "-m", message)
	return r.git("rev-parse", "HEAD")
}

// trackOrigin writes the refs a clone leaves behind, so the default base has
// something to resolve without a network.
func (r *repo) trackOrigin() {
	r.t.Helper()

	r.git("update-ref", "refs/remotes/origin/main", r.git("rev-parse", "HEAD"))
	r.git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
}

// run drives the command the way a shell does, from inside dir.
//
// SetArgs takes an empty slice rather than nil, because nil means "read os.Args",
// which under `go test` is the test binary's own flags.
func run(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)

	cmd := cli.NewRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{}, args...))

	err := cmd.Execute()
	return out.String(), err
}

func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()

	out, err := run(t, dir, args...)
	if err != nil {
		t.Fatalf("zen-review %v: %v\n%s", args, err, out)
	}
	return out
}

// The changeset is the merge base through the working tree, and every way a file
// can arrive in it has to show up: committed, staged, unstaged, and never added at
// all.
func TestTheOverviewListsEveryChangedFile(t *testing.T) {
	r := newRepo(t)
	r.write("kept.txt", "one\n")
	r.write("edited.txt", "before\n")
	r.write("staged.txt", "before\n")
	r.write("gone.txt", "doomed\n")
	r.write("old-name.txt", "renamed content\nsecond line\nthird line\n")
	base := r.commit("first")
	r.trackOrigin()

	r.git("checkout", "-b", "feature")
	r.write("committed.txt", "from a commit\n")
	r.commit("the agent committed this one")

	r.write("edited.txt", "after\n")
	r.write("staged.txt", "after\n")
	r.git("add", "staged.txt")
	r.git("rm", "-q", "gone.txt")
	r.git("mv", "old-name.txt", "new-name.txt")
	r.write("untracked.txt", "never added\n")

	out := mustRun(t, r.dir)

	if !strings.Contains(out, "base  origin/main ("+base[:7]+")") {
		t.Errorf("the base line is wrong:\n%s", out)
	}

	want := []string{
		"M  edited.txt",
		"M  staged.txt",
		"A  committed.txt",
		"A  untracked.txt",
		"D  gone.txt",
		"R  old-name.txt -> new-name.txt",
		"6 files",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output does not contain %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "kept.txt") {
		t.Errorf("an unchanged file is in the changeset:\n%s", out)
	}

	// tabwriter pads every cell but the last, so a row that leaves one empty ends
	// in the padding for it.
	for i, line := range strings.Split(out, "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("line %d ends in whitespace: %q", i+1, line)
		}
	}
}

// An empty changeset is an empty state, not an error.
func TestNothingChangedIsNotAnError(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "one\n")
	r.commit("first")
	r.trackOrigin()

	out := mustRun(t, r.dir)

	if !strings.Contains(out, "no changes since origin/main") {
		t.Errorf("output = %q, want it to say the changeset is empty", out)
	}
}

func TestTheBaseFlagOverridesTheDefault(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "one\n")
	r.commit("first")
	r.trackOrigin()
	r.write("a.txt", "two\n")
	older := r.commit("second")

	r.write("a.txt", "three\n")

	out := mustRun(t, r.dir, "--base", older)

	if !strings.Contains(out, "base  "+older+" ("+older[:7]+")") {
		t.Errorf("the base line does not name the flag's ref:\n%s", out)
	}
	if !strings.Contains(out, "1 file, 1 hunk") {
		t.Errorf("output does not count the one change:\n%s", out)
	}
}

// The two startup failures the spec names, each one plain line the reader can act
// on.
func TestStartupFailuresSayWhatToDo(t *testing.T) {
	t.Run("outside a repository", func(t *testing.T) {
		_, err := run(t, t.TempDir())
		if err == nil {
			t.Fatal("running outside a repository should fail")
		}
		if !strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("err = %v, want it to say this is not a repository", err)
		}
	})

	t.Run("with no origin to fall back on", func(t *testing.T) {
		r := newRepo(t)
		r.write("a.txt", "one\n")
		r.commit("first")

		_, err := run(t, r.dir)
		if err == nil {
			t.Fatal("a repository with no origin/HEAD should not guess a base")
		}
		if !strings.Contains(err.Error(), "--base") {
			t.Errorf("err = %v, want it to name the flag", err)
		}
	})

	t.Run("with a base that does not resolve", func(t *testing.T) {
		r := newRepo(t)
		r.write("a.txt", "one\n")
		r.commit("first")

		_, err := run(t, r.dir, "--base", "no-such-ref")
		if err == nil {
			t.Fatal("a base that does not resolve should fail")
		}
		if !strings.Contains(err.Error(), "no-such-ref") {
			t.Errorf("err = %v, want it to name the ref", err)
		}
	})
}

// An explicit range is its own session, which arrives with sessions. Accepting the
// argument and ignoring it would read as support.
func TestAnExplicitRangeIsRefusedRatherThanIgnored(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "one\n")
	r.commit("first")
	r.trackOrigin()

	if _, err := run(t, r.dir, "HEAD~1..HEAD"); err == nil {
		t.Fatal("a range argument should be refused until sessions exist")
	}
}

func TestTheVersionFlagReportsTheBuildVersion(t *testing.T) {
	got := mustRun(t, t.TempDir(), "--version")

	if !strings.Contains(got, version.Version) {
		t.Errorf("version output = %q, want it to carry %q", got, version.Version)
	}
}
