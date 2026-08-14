package cli_test

import (
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/version"
)

// The startup failures the spec names, each one plain line the reader can act
// on.
func TestStartupFailuresSayWhatToDo(t *testing.T) {
	t.Run("outside a repository", func(t *testing.T) {
		f := newFixture(t)

		_, _, err := f.runFrom(t.TempDir())
		if err == nil {
			t.Fatal("running outside a repository should fail")
		}
		if !strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("err = %v, want it to say this is not a repository", err)
		}
	})

	t.Run("with no origin to fall back on", func(t *testing.T) {
		f := newFixture(t)
		f.Write("a.txt", "one\n")
		f.Commit("first")

		err := f.failure()
		if !strings.Contains(err.Error(), "--base") {
			t.Errorf("err = %v, want it to name the flag", err)
		}
	})

	t.Run("with a base that does not resolve", func(t *testing.T) {
		f := newFixture(t)
		f.Write("a.txt", "one\n")
		f.Commit("first")

		err := f.failure("--base", "no-such-ref")
		if !strings.Contains(err.Error(), "no-such-ref") {
			t.Errorf("err = %v, want it to name the ref", err)
		}
	})

	// The base the session already had is the other path into the same failure,
	// and it is the one a reader hits days later without touching a flag. It has
	// to arrive as the engine's sentence rather than raw git output.
	t.Run("with a stored base that has since gone", func(t *testing.T) {
		f := branched(t)
		f.Git("branch", "tmp", "main")
		f.mustRun("--base", "tmp")
		f.Git("branch", "-q", "-D", "tmp")

		err := f.failure()
		for _, want := range []string{"tmp", "no longer resolves", "--base"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %v, want it to contain %q", err, want)
			}
		}
		if strings.Contains(err.Error(), "fatal:") {
			t.Errorf("err = %v, want the engine's sentence rather than git's", err)
		}
	})
}

// A branch stacked on another local branch is the one refusal that lists things
// to type. Measuring it from origin/main would show the parent branch's commits
// as this branch's work, which is a worse answer than asking.
func TestAStackedBranchRefusesAndNamesTheCandidates(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "parent")
	f.Write("b.txt", "parent work\n")
	f.Commit("on the parent")

	f.Git("checkout", "-q", "-b", "child")
	f.Write("c.txt", "child work\n")
	f.Commit("on the child")

	for _, args := range [][]string{nil, {"status"}, {"refresh"}, {"status", "--json"}} {
		err := f.failure(args...)
		if !strings.Contains(err.Error(), "parent") {
			t.Errorf("zen-review %v: err = %v, want it to name the branch to measure from", args, err)
		}
		if !strings.Contains(err.Error(), "--base") {
			t.Errorf("zen-review %v: err = %v, want it to name the flag", args, err)
		}
	}
}

// A failure writes nothing to stdout even under --json, so a caller parsing the
// stream never has to tell an error apart from a payload.
func TestAFailureLeavesTheJSONStreamEmpty(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	stdout, _, err := f.run("status", "--json")
	if err == nil {
		t.Fatal("a repository with no origin/HEAD should not guess a base")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing but the payload to ever reach it", stdout)
	}
}

// An explicit range is its own session, which arrives with its own ticket.
// Accepting the argument and ignoring it would read as support.
//
// Cobra does not inherit Args, so this runs against every entry point. Left off
// a subcommand, the range walks straight back in through it.
func TestAnExplicitRangeIsRefusedRatherThanIgnored(t *testing.T) {
	for _, args := range [][]string{
		{"HEAD~1..HEAD"},
		{"status", "HEAD~1..HEAD"},
		{"refresh", "HEAD~1..HEAD"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := branched(t)
			f.Write("b.txt", "two\n")

			if err := f.failure(args...); !strings.Contains(err.Error(), "HEAD~1..HEAD") {
				t.Errorf("err = %v, want it to name what it would not take", err)
			}
		})
	}
}

// Both flags are persistent, so every command takes them. A flag that works on
// the bare invocation and not on a subcommand is the kind of thing nobody finds
// until they script it.
//
// The one exception is --base on a write, which is refused rather than
// inherited. That refusal has its own test beside the commands it applies to.
func TestTheSharedFlagsReachEverySubcommand(t *testing.T) {
	for _, args := range [][]string{
		{"--json"},
		{"status", "--json"},
		{"refresh", "--json"},
		{"files", "--json"},
		{"status", "--base", "main"},
		{"refresh", "--base", "main"},
		{"files", "--base", "main"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := edited(t)

			f.mustRun(args...)
		})
	}
}

func TestTheVersionFlagReportsTheBuildVersion(t *testing.T) {
	f := newFixture(t)

	got := f.mustRun("--version")

	if !strings.Contains(got, version.Version) {
		t.Errorf("version output = %q, want it to carry %q", got, version.Version)
	}
}
