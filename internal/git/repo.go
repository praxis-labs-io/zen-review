// Package git runs the git binary and returns bytes and structs, never opinions.
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

var ErrNotARepo = errors.New("not a git repository")

// Repo is safe for concurrent use.
type Repo struct {
	root      string
	commonDir string
}

// Open resolves the repository containing path. Returns ErrNotARepo outside a work tree.
func Open(ctx context.Context, path string) (*Repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is not on PATH")
	}

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

func (r *Repo) Root() string { return r.root }

// CommonDir is the absolute git directory shared with linked worktrees.
func (r *Repo) CommonDir() string { return r.commonDir }

type invocation struct {
	extra []string

	allow int

	allowStderr bool

	stdin []byte
}

type result struct {
	stdout []byte
	stderr []byte
	code   int
}

func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	res, err := runIn(ctx, dir, invocation{}, args...)
	return res.stdout, err
}

func runStatus(ctx context.Context, dir string, allow int, args ...string) ([]byte, int, error) {
	res, err := runIn(ctx, dir, invocation{allow: allow}, args...)
	return res.stdout, res.code, err
}

func runIn(ctx context.Context, dir string, in invocation, args ...string) (result, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(env(), in.extra...)
	if in.stdin != nil {
		cmd.Stdin = bytes.NewReader(in.stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return result{stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
	}

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return result{code: -1}, fmt.Errorf("running git %s: %w", strings.Join(args, " "), err)
	}
	if code := exit.ExitCode(); code == in.allow && (in.allowStderr || stderr.Len() == 0) {
		return result{stdout: stdout.Bytes(), stderr: stderr.Bytes(), code: code}, nil
	}
	return result{code: exit.ExitCode()}, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderrOf(stderr.Bytes()))
}

// env drops the variables that beat cmd.Dir, pins LC_ALL for Open's message match, and stops reads taking the index lock.
func env() []string {
	out := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		switch key, _, _ := strings.Cut(kv, "="); key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE":
		default:
			out = append(out, kv)
		}
	}
	return append(out, "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
}

func stderrOf(stderr []byte) string {
	s := strings.TrimSpace(string(stderr))
	if s == "" {
		return "no stderr"
	}
	return strings.ReplaceAll(s, "\n", "; ")
}

func trim(out []byte) string { return strings.TrimRight(string(out), "\n") }

func nulFields(out []byte) []string {
	var fields []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			fields = append(fields, f)
		}
	}
	return fields
}
