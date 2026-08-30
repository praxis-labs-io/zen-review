// Package plugin tests the artifacts the Claude Code plugin ships.
//
// What is under test is a shell script, not Go, so the tests drive it the way a
// hook runner does: a real repository, a real zen-review binary on PATH, and an
// assertion on the status it left with. The status is the whole contract, and it
// has already been written backwards twice.
package plugin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/testrepo"
)

func TestMain(m *testing.M) { os.Exit(testrepo.Main(m)) }

// hook is the script the plugin ships, found relative to this package.
const hook = "../../plugin/hooks/unresolved.sh"

// binDir builds zen-review into a directory to be put at the front of PATH.
//
// Built once per run rather than per case: it is the same binary every time, and
// a compile per case is most of the suite's wall clock.
func binDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "zen-review"),
		"github.com/praxis-labs-io/zen-review/cmd/zen-review")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building zen-review: %v\n%s", err, out)
	}
	return dir
}

// run executes the hook in dir and reports its status and what it wrote to
// stderr. withBin says whether zen-review can be found, which is the difference
// between a queue and a broken call.
func run(t *testing.T, dir, bin string, withBin bool) (int, string) {
	t.Helper()

	path := "/usr/bin:/bin:/usr/sbin:/sbin"
	if withBin {
		path = bin + ":" + path
	}

	script, err := filepath.Abs(hook)
	if err != nil {
		t.Fatalf("resolving the hook: %v", err)
	}

	cmd := exec.Command("sh", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+path)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = nil

	err = cmd.Run()
	var status int
	if exit, ok := err.(*exec.ExitError); ok {
		status = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	return status, stderr.String()
}

// zen runs a zen-review command in the repository.
func zen(t *testing.T, dir, bin string, args ...string) string {
	t.Helper()

	cmd := exec.Command(filepath.Join(bin, "zen-review"), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")

	out, err := cmd.CombinedOutput()
	// comments --exit-code leaves 1 on a match, which is an answer and not a
	// failure. Only a 2 is worth failing the test over.
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return string(out)
	}
	if err != nil {
		t.Fatalf("zen-review %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// openID is the id of the one open comment, read from the JSON rather than the
// prose: the listing is written for a person and its shape is not a contract.
func openID(t *testing.T, dir, bin string) string {
	t.Helper()

	var listing struct {
		Comments []struct {
			ID string `json:"id"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(zen(t, dir, bin, "comments", "--state", "open", "--json")), &listing); err != nil {
		t.Fatalf("reading the comment listing: %v", err)
	}
	if len(listing.Comments) != 1 {
		t.Fatalf("got %d open comments, want 1", len(listing.Comments))
	}
	return listing.Comments[0].ID
}

// changed is a repository on a branch with one unreviewed hunk on it.
func changed(t *testing.T) *testrepo.Repo {
	t.Helper()

	r := testrepo.New(t)
	r.Write("main.go", "package main\n\nfunc main() {}\n")
	r.Commit("base")
	r.TrackOrigin("HEAD")

	r.Git("checkout", "-q", "-b", "work")
	r.Write("main.go", "package main\n\nfunc mean(xs []int) int {\n\treturn 0\n}\n\nfunc main() {}\n")
	r.Commit("add mean")
	return r
}

// TestHookLeavesUninvolvedDirectoriesAlone covers the two ways the hook is not
// the thing that should be answering.
//
// The second case is the one that matters: resolving a session creates it, so a
// hook without the state.db test would open a review database in every
// repository an agent ever stops in.
func TestHookLeavesUninvolvedDirectoriesAlone(t *testing.T) {
	bin := binDir(t)

	t.Run("not a repository", func(t *testing.T) {
		status, stderr := run(t, t.TempDir(), bin, true)
		if status != 0 {
			t.Errorf("status = %d, want 0 outside a repository", status)
		}
		if stderr != "" {
			t.Errorf("wrote %q, want nothing", stderr)
		}
	})

	t.Run("no session", func(t *testing.T) {
		r := changed(t)

		status, stderr := run(t, r.Dir(), bin, true)
		if status != 0 {
			t.Errorf("status = %d, want 0 where no review was opened", status)
		}
		if stderr != "" {
			t.Errorf("wrote %q, want nothing", stderr)
		}

		db := filepath.Join(r.Dir(), ".git", "zen-review")
		if _, err := os.Stat(db); !os.IsNotExist(err) {
			t.Errorf("%s exists: the hook opened a session it was never asked for", db)
		}
	})
}

// TestHookHoldsTheTurnOnOpenComments is the translation, and it is the whole
// point of the script.
//
// zen-review answers 1 for a match, and Claude Code honours only 2 as a block.
// A hook passing the status straight through lets an agent stop on a queue.
func TestHookHoldsTheTurnOnOpenComments(t *testing.T) {
	bin := binDir(t)
	r := changed(t)

	zen(t, r.Dir(), bin, "refresh")
	zen(t, r.Dir(), bin, "comment", "main.go", "--lines", "3", "--body", "Why zero?")

	status, stderr := run(t, r.Dir(), bin, true)
	if status != 2 {
		t.Fatalf("status = %d, want 2 so the turn is held open", status)
	}
	if !strings.Contains(stderr, "Why zero?") {
		t.Errorf("stderr does not carry the comment, so the agent is held with no reason why:\n%s", stderr)
	}
	if !strings.Contains(stderr, "zen-review address") {
		t.Errorf("stderr does not say how to answer:\n%s", stderr)
	}
}

// TestHookReleasesOnceAddressed is the case the documented filter got wrong.
//
// address is the agent's verb and resolve is the reader's, so a gate on
// unresolved is one the agent cannot clear: it would answer everything, stay
// blocked, and loop until the client gave up.
func TestHookReleasesOnceAddressed(t *testing.T) {
	bin := binDir(t)
	r := changed(t)

	zen(t, r.Dir(), bin, "refresh")
	zen(t, r.Dir(), bin, "comment", "main.go", "--lines", "3", "--body", "Why zero?")

	zen(t, r.Dir(), bin, "address", openID(t, r.Dir(), bin), "--body", "Placeholder, filled in now.")

	status, stderr := run(t, r.Dir(), bin, true)
	if status != 0 {
		t.Fatalf("status = %d, want 0 once every comment is answered; stderr:\n%s", status, stderr)
	}

	// The comment is addressed and not resolved, so the filter the docs used
	// would still match here. Asserting it keeps the distinction from quietly
	// going away.
	if unresolved := zen(t, r.Dir(), bin, "comments", "--state", "unresolved", "--exit-code"); unresolved == "" {
		t.Error("expected the addressed comment to still be unresolved")
	}
}

// TestHookNeverBlocksOnFailure keeps a broken call from becoming a trap.
//
// Exit 2 is how a Stop hook says keep going, so answering a failure with 2 would
// hold the agent against an error it cannot act on.
func TestHookNeverBlocksOnFailure(t *testing.T) {
	bin := binDir(t)
	r := changed(t)

	zen(t, r.Dir(), bin, "refresh")

	status, stderr := run(t, r.Dir(), bin, false)
	if status == 2 {
		t.Fatalf("status = 2 with no zen-review on PATH: a failure that blocks is one the agent cannot get past")
	}
	if status != 1 {
		t.Errorf("status = %d, want 1 so the failure is visible without blocking", status)
	}
	if !strings.Contains(stderr, "could not read the review queue") {
		t.Errorf("stderr does not name the failure:\n%s", stderr)
	}
}
