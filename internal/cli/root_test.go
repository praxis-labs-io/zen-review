package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/cli"
	"github.com/zen-review/zen-review/internal/testrepo"
	"github.com/zen-review/zen-review/internal/version"
)

func TestMain(m *testing.M) { os.Exit(testrepo.Main(m)) }

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
	r := testrepo.New(t)
	r.Write("kept.txt", "one\n")
	r.Write("edited.txt", "before\n")
	r.Write("staged.txt", "before\n")
	r.Write("gone.txt", "doomed\n")
	r.Write("old-name.txt", "renamed content\nsecond line\nthird line\n")
	base := r.Commit("first")
	r.TrackOrigin("HEAD")

	r.Git("checkout", "-b", "feature")
	r.Write("committed.txt", "from a commit\n")
	r.Commit("the agent committed this one")

	r.Write("edited.txt", "after\n")
	r.Write("staged.txt", "after\n")
	r.Git("add", "staged.txt")
	r.Git("rm", "-q", "gone.txt")
	r.Git("mv", "old-name.txt", "new-name.txt")
	r.Write("untracked.txt", "never added\n")

	out := mustRun(t, r.Dir())

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
	r := testrepo.New(t)
	r.Write("a.txt", "one\n")
	r.Commit("first")
	r.TrackOrigin("HEAD")

	out := mustRun(t, r.Dir())

	if !strings.Contains(out, "no changes since origin/main") {
		t.Errorf("output = %q, want it to say the changeset is empty", out)
	}
}

func TestTheBaseFlagOverridesTheDefault(t *testing.T) {
	r := testrepo.New(t)
	r.Write("a.txt", "one\n")
	r.Commit("first")
	r.TrackOrigin("HEAD")
	r.Write("a.txt", "two\n")
	older := r.Commit("second")

	r.Write("a.txt", "three\n")

	out := mustRun(t, r.Dir(), "--base", older)

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
		r := testrepo.New(t)
		r.Write("a.txt", "one\n")
		r.Commit("first")

		_, err := run(t, r.Dir())
		if err == nil {
			t.Fatal("a repository with no origin/HEAD should not guess a base")
		}
		if !strings.Contains(err.Error(), "--base") {
			t.Errorf("err = %v, want it to name the flag", err)
		}
	})

	t.Run("with a base that does not resolve", func(t *testing.T) {
		r := testrepo.New(t)
		r.Write("a.txt", "one\n")
		r.Commit("first")

		_, err := run(t, r.Dir(), "--base", "no-such-ref")
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
	r := testrepo.New(t)
	r.Write("a.txt", "one\n")
	r.Commit("first")
	r.TrackOrigin("HEAD")

	if _, err := run(t, r.Dir(), "HEAD~1..HEAD"); err == nil {
		t.Fatal("a range argument should be refused until sessions exist")
	}
}

func TestTheVersionFlagReportsTheBuildVersion(t *testing.T) {
	got := mustRun(t, t.TempDir(), "--version")

	if !strings.Contains(got, version.Version) {
		t.Errorf("version output = %q, want it to carry %q", got, version.Version)
	}
}

// git lists a repository embedded in the work tree as a single directory entry and
// then refuses to diff it. Dropping it is the failure this tool exists to prevent,
// so it is listed with the reason instead.
func TestAnEmbeddedRepositoryIsListedRatherThanDropped(t *testing.T) {
	r := testrepo.New(t)
	r.Write("a.txt", "one\n")
	r.Commit("first")
	r.TrackOrigin("HEAD")

	r.Write("vendored/f.txt", "inside\n")
	r.Git("init", "-q", "-b", "main", "vendored")
	r.Write("plain.txt", "beside it\n")

	out := mustRun(t, r.Dir())

	if !strings.Contains(out, "vendored/") {
		t.Errorf("the embedded repository is missing from the changeset:\n%s", out)
	}
	if !strings.Contains(out, "a repository of its own") {
		t.Errorf("output does not say why it has no hunks:\n%s", out)
	}
	if !strings.Contains(out, "2 files") {
		t.Errorf("output does not count both entries:\n%s", out)
	}
}

// A symlink is diffed rather than skipped. git stores the target path as the
// content and hands back the blob it would record on commit, so one added line
// naming the target is the whole change.
func TestAnUntrackedSymlinkIsDiffedAsItsTargetPath(t *testing.T) {
	r := testrepo.New(t)
	r.Write("target.txt", "one\ntwo\nthree\n")
	r.Commit("first")
	r.TrackOrigin("HEAD")

	if err := os.Symlink("target.txt", filepath.Join(r.Dir(), "link.txt")); err != nil {
		t.Fatalf("creating the symlink: %v", err)
	}

	out := mustRun(t, r.Dir())

	if !strings.Contains(out, "A  link.txt") {
		t.Errorf("the symlink is missing from the changeset:\n%s", out)
	}
	if !strings.Contains(out, "+1 -0") {
		t.Errorf("output = %q, want one added line, the target path rather than its contents", out)
	}
}
