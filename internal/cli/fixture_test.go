package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/cli"
	"github.com/praxis-labs-io/zen-review/internal/testrepo"
)

func TestMain(m *testing.M) { os.Exit(testrepo.Main(m)) }

type fixture struct {
	*testrepo.Repo
	t *testing.T

	stdin io.Reader
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{Repo: testrepo.New(t), t: t}
}

func branched(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	return f
}

func edited(t *testing.T) *fixture {
	t.Helper()

	f := branched(t)
	f.Write("a.txt", "two\n")
	return f
}

func (f *fixture) run(args ...string) (stdout, stderr string, err error) {
	f.t.Helper()
	return f.runFrom(f.Dir(), args...)
}

func (f *fixture) runFrom(dir string, args ...string) (stdout, stderr string, err error) {
	f.t.Helper()
	f.t.Chdir(dir)

	cmd := cli.NewRoot()
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetArgs(append([]string{}, args...))
	if f.stdin != nil {
		cmd.SetIn(f.stdin)
	}

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

func (f *fixture) failure(args ...string) error {
	f.t.Helper()

	out, _, err := f.run(args...)
	if err == nil {
		f.t.Fatalf("zen-review %v should have failed, and wrote:\n%s", args, out)
	}
	return err
}

// Declared apart from the payload types, since a test reading the struct it checks cannot catch a renamed key.
type wireHeader struct {
	Session string `json:"session"`
	Ref     string `json:"ref"`
	Kind    string `json:"kind"`
	Branch  string `json:"branch"`

	Base struct {
		Ref      string `json:"ref"`
		SHA      string `json:"sha"`
		Fallback string `json:"fallback"`
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
}

type wire struct {
	wireHeader

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

	Candidates *struct {
		Local  []candidateWire `json:"local"`
		Remote []candidateWire `json:"remote"`
	} `json:"candidates"`
}

type candidateWire struct {
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
	Ahead int    `json:"ahead"`
}

type stateWire struct {
	wireHeader

	Files []stateFile `json:"files"`

	Totals struct {
		Files     int `json:"files"`
		Reviewed  int `json:"reviewed"`
		Items     int `json:"items"`
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"totals"`
}

type stateFile struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath"`
	Status  string `json:"status"`
	Omitted string `json:"omitted"`

	State    string `json:"state"`
	Changed  bool   `json:"changed"`
	Reviewed int    `json:"reviewed"`
	Items    int    `json:"items"`

	Additions int `json:"additions"`
	Deletions int `json:"deletions"`

	Hunks []struct {
		Side  string `json:"side"`
		Line  int    `json:"line"`
		State string `json:"state"`

		Anchors []struct {
			Side  string `json:"side"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		} `json:"anchors"`
	} `json:"hunks"`
}

type commentWire struct {
	wireHeader

	Comments []commentEntry `json:"comments"`

	Totals struct {
		Comments   int `json:"comments"`
		Open       int `json:"open"`
		Addressed  int `json:"addressed"`
		Resolved   int `json:"resolved"`
		Orphaned   int `json:"orphaned"`
		Unresolved int `json:"unresolved"`
	} `json:"totals"`
}

type commentEntry struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Side  string `json:"side"`
	Scope string `json:"scope"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	State string `json:"state"`
	Body  string `json:"body"`

	Response string   `json:"response"`
	Replaced []string `json:"replaced"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (f *fixture) decode(args ...string) (wire, string) {
	f.t.Helper()
	return f.decodeFrom(f.Dir(), args...)
}

func (f *fixture) decodeFrom(dir string, args ...string) (wire, string) {
	f.t.Helper()

	var w wire
	raw := f.jsonFrom(dir, &w, args...)
	return w, raw
}

func (f *fixture) decodeState(args ...string) (stateWire, string) {
	f.t.Helper()

	var w stateWire
	raw := f.jsonFrom(f.Dir(), &w, args...)
	return w, raw
}

func (f *fixture) decodeComments(args ...string) (commentWire, string) {
	f.t.Helper()

	var w commentWire
	raw := f.jsonFrom(f.Dir(), &w, args...)
	return w, raw
}

func (f *fixture) jsonFrom(dir string, into any, args ...string) string {
	f.t.Helper()

	stdout, stderr, err := f.runFrom(dir, append(args, "--json")...)
	if err != nil {
		f.t.Fatalf("zen-review %v --json: %v\n%s%s", args, err, stdout, stderr)
	}
	if stderr != "" {
		f.t.Errorf("zen-review %v --json wrote to stderr on success: %q", args, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), into); err != nil {
		f.t.Fatalf("zen-review %v --json did not write JSON: %v\n%s", args, err, stdout)
	}
	return stdout
}

func (w wire) files() map[string]string {
	got := map[string]string{}
	for _, f := range w.Files {
		got[f.Path] = f.Status
	}
	return got
}

func (f *fixture) sessionRefs() []string {
	f.t.Helper()

	out := f.Git("for-each-ref", "--format=%(refname)", "refs/zen-review/")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
