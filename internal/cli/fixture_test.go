package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/cli"
	"github.com/zen-review/zen-review/internal/testrepo"
)

func TestMain(m *testing.M) { os.Exit(testrepo.Main(m)) }

// fixture is a real repository with a real database under it. Nothing here is
// mocked: what these tests check is what the commands do to a repository, and a
// fake would answer a different question.
type fixture struct {
	*testrepo.Repo
	t *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{Repo: testrepo.New(t), t: t}
}

// branched is the common shape: a commit on main tracked as origin/main, then a
// feature branch to do the work on.
func branched(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	return f
}

// edited is branched with one uncommitted change, which is the state a review
// actually opens in.
func edited(t *testing.T) *fixture {
	t.Helper()

	f := branched(t)
	f.Write("a.txt", "two\n")
	return f
}

// run drives the command the way a shell does, from inside the repository.
//
// stdout and stderr are separate buffers. Merged, a test cannot prove an error
// stayed out of the JSON stream, which is the one thing --json has to promise
// whatever is parsing it.
//
// SetArgs takes an empty slice rather than nil, because nil means "read
// os.Args", which under `go test` is the test binary's own flags.
func (f *fixture) run(args ...string) (stdout, stderr string, err error) {
	f.t.Helper()
	return f.runFrom(f.Dir(), args...)
}

// runFrom is run from somewhere other than the repository root, which is where
// most invocations actually happen.
func (f *fixture) runFrom(dir string, args ...string) (stdout, stderr string, err error) {
	f.t.Helper()
	f.t.Chdir(dir)

	cmd := cli.NewRoot()
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetArgs(append([]string{}, args...))

	err = cmd.Execute()
	return out.String(), errs.String(), err
}

func (f *fixture) mustRun(args ...string) string {
	f.t.Helper()

	out, errs, err := f.run(args...)
	if err != nil {
		f.t.Fatalf("zen-review %v: %v\n%s%s", args, err, out, errs)
	}
	return out
}

// failure runs a command that has to fail and hands back the error, so a test
// asserting on a message cannot pass by the command quietly succeeding.
func (f *fixture) failure(args ...string) error {
	f.t.Helper()

	out, _, err := f.run(args...)
	if err == nil {
		f.t.Fatalf("zen-review %v should have failed, and wrote:\n%s", args, out)
	}
	return err
}

// wire is the payload as something parsing it sees, declared here rather than
// shared with the code that produces it. A test reading the same struct it is
// checking cannot catch a renamed key, and the key names are most of what this
// output promises.
type wire struct {
	Session string `json:"session"`
	Ref     string `json:"ref"`
	Kind    string `json:"kind"`
	Branch  string `json:"branch"`

	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`

	Generation *struct {
		Seq       int    `json:"seq"`
		Commit    string `json:"commit"`
		BaseSha   string `json:"baseSha"`
		HeadSha   string `json:"headSha"`
		CreatedAt string `json:"createdAt"`
	} `json:"generation"`

	Stale       bool     `json:"stale"`
	StaleReason string   `json:"staleReason"`
	Skipped     []string `json:"skipped"`

	Files []struct {
		Path      string `json:"path"`
		OldPath   string `json:"oldPath"`
		Status    string `json:"status"`
		Omitted   string `json:"omitted"`
		Hunks     int    `json:"hunks"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	} `json:"files"`

	Totals struct {
		Files     int `json:"files"`
		Hunks     int `json:"hunks"`
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"totals"`
}

// decode runs a command with --json and parses what it wrote to stdout. It also
// checks stderr stayed empty, because a warning landing in the stream is what
// breaks the caller rather than the command.
func (f *fixture) decode(args ...string) (wire, string) {
	f.t.Helper()
	return f.decodeFrom(f.Dir(), args...)
}

func (f *fixture) decodeFrom(dir string, args ...string) (wire, string) {
	f.t.Helper()

	stdout, stderr, err := f.runFrom(dir, append(args, "--json")...)
	if err != nil {
		f.t.Fatalf("zen-review %v --json: %v\n%s%s", args, err, stdout, stderr)
	}
	if stderr != "" {
		f.t.Errorf("zen-review %v --json wrote to stderr on success: %q", args, stderr)
	}

	var w wire
	if err := json.Unmarshal([]byte(stdout), &w); err != nil {
		f.t.Fatalf("zen-review %v --json did not write JSON: %v\n%s", args, err, stdout)
	}
	return w, stdout
}

// files keys the rows by path, so an assertion does not depend on the order git
// happened to report them in.
func (w wire) files() map[string]string {
	got := map[string]string{}
	for _, f := range w.Files {
		got[f.Path] = f.Status
	}
	return got
}

// sessionRefs is every ref this tool has written, which is how a test proves a
// read path wrote nothing at all.
func (f *fixture) sessionRefs() []string {
	f.t.Helper()

	out := f.Git("for-each-ref", "--format=%(refname)", "refs/zen-review/")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
