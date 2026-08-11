// Package git runs git and returns bytes and structs. It holds no opinion about
// what a changeset is or what has been reviewed, and nothing above it shells out
// to git.
//
// Every call is a process. Reimplementing git's rules in Go would only be a
// second thing to be wrong about them, so the tests here run against real
// temporary repositories: what is under test is whether git is being called
// correctly.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNotARepo is returned by Open for a path that is not inside a work tree.
var ErrNotARepo = errors.New("not a git repository")

// Repo is one git work tree. It holds no process and no lock, so a value is
// reusable and safe to call from more than one goroutine.
type Repo struct {
	root      string
	commonDir string
}

// Open resolves the repository containing path.
//
// The two startup failures are told apart because the fix differs: git missing
// from PATH is an installation to repair, ErrNotARepo is a directory to leave.
func Open(ctx context.Context, path string) (*Repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is not on PATH")
	}

	// --path-format=absolute settles both answers. --git-common-dir otherwise
	// comes back relative to the process's directory, so a call from a
	// subdirectory returns ../../.git and the database lands somewhere else.
	out, err := run(ctx, path, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return nil, ErrNotARepo
		}
		return nil, err
	}

	lines := strings.Split(trim(out), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
		return nil, fmt.Errorf("resolving the repository at %s: rev-parse answered %q", path, out)
	}
	return &Repo{root: lines[0], commonDir: lines[1]}, nil
}

// Root is the top of the work tree, and the directory every command runs in.
func (r *Repo) Root() string { return r.root }

// CommonDir is the git directory shared by a checkout and its linked worktrees,
// absolute. The review database lives under it, so a throwaway worktree and its
// parent agree about which repository they are in.
func (r *Repo) CommonDir() string { return r.commonDir }

// run executes git in dir and returns stdout, treating any non-zero exit as a
// failure.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, _, err := runStatus(ctx, dir, 0, args...)
	return out, err
}

// runStatus is run for a command whose non-zero exit is an answer rather than a
// failure, reporting the status alongside stdout. allow names the one status
// that means something: `merge-base --is-ancestor` says no with 1, and
// `--no-index` implies `--exit-code`.
func runStatus(ctx context.Context, dir string, allow int, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), 0, nil
	}

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return nil, -1, fmt.Errorf("running git %s: %w", strings.Join(args, " "), err)
	}
	if code := exit.ExitCode(); code == allow {
		return stdout.Bytes(), code, nil
	}
	return nil, exit.ExitCode(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderrOf(&stderr))
}

// env pins the two variables that decide whether git can be read and whether it
// gets in the way.
//
// LC_ALL keeps git's fatal messages in the wording Open matches on.
// GIT_OPTIONAL_LOCKS stops a read-only diff from taking the index lock, which an
// agent running git in the same repository is entitled to be holding.
func env() []string {
	return append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
}

// stderrOf is git's own complaint, on one line and never empty: an error message
// with nothing after the colon reads like a bug in this package.
func stderrOf(stderr *bytes.Buffer) string {
	s := strings.TrimSpace(stderr.String())
	if s == "" {
		return "no stderr"
	}
	return strings.ReplaceAll(s, "\n", "; ")
}

// trim drops the newline git ends its output with.
func trim(out []byte) string { return strings.TrimRight(string(out), "\n") }

// nulFields splits -z output. A path can hold anything but a NUL, which is the
// whole reason for asking git to separate them with one.
func nulFields(out []byte) []string {
	var fields []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			fields = append(fields, f)
		}
	}
	return fields
}
