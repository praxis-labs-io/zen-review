package cli_test

import (
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/version"
)

// Outside a repository is the one startup that still fails: there is nothing to
// open, so there is no base to fall back to.
func TestRunningOutsideARepositorySaysSo(t *testing.T) {
	f := newFixture(t)

	_, _, err := f.runFrom(t.TempDir())
	if err == nil {
		t.Fatal("running outside a repository should fail")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("err = %v, want it to say this is not a repository", err)
	}
}

// Nothing about the base stops a run. Where it is not the one a reader would
// expect the command says which it took and why, and carries on.
func TestABaseThatCannotBeUsedIsReportedRatherThanRefused(t *testing.T) {
	// remoteless is a repository nobody ever added an origin to.
	remoteless := func(t *testing.T) *fixture {
		f := newFixture(t)
		f.Write("a.txt", "one\n")
		f.Commit("first")
		return f
	}

	tests := []struct {
		name string

		// setup builds the repository and returns the arguments to run with.
		setup func(t *testing.T) (*fixture, []string)

		base string
		says []string
	}{
		{
			name: "with no origin to fall back on",
			setup: func(t *testing.T) (*fixture, []string) {
				return remoteless(t), nil
			},
			base: "HEAD",
			says: []string{"HEAD (", "·  uncommitted"},
		},
		{
			name: "with a base that does not resolve",
			setup: func(t *testing.T) (*fixture, []string) {
				return remoteless(t), []string{"--base", "no-such-ref"}
			},
			base: "HEAD",
			says: []string{"·  not no-such-ref"},
		},
		{
			// The path a reader hits days later without touching a flag. It has to
			// arrive as the engine's sentence rather than raw git output.
			name: "with a stored base that has since gone",
			setup: func(t *testing.T) (*fixture, []string) {
				f := branched(t)
				f.Git("branch", "tmp", "main")
				f.mustRun("--base", "tmp")
				f.Git("branch", "-q", "-D", "tmp")
				return f, nil
			},
			base: "origin/main",
			says: []string{"·  not tmp"},
		},
		{
			// Measuring a stacked branch from origin/main reads the parent's
			// commits as this branch's work.
			name: "on a branch stacked on another",
			setup: func(t *testing.T) (*fixture, []string) {
				f := branched(t)
				f.Write("b.txt", "parent work\n")
				f.Commit("on the parent")

				f.Git("checkout", "-q", "-b", "child")
				f.Write("c.txt", "child work\n")
				f.Commit("on the child")
				return f, nil
			},
			base: "feature",
			says: []string{"·  stacked"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, args := tc.setup(t)

			got := f.mustRun(args...)

			if !strings.Contains(got, "base        "+tc.base+" ") {
				t.Errorf("output = %q, want the base to be %s", got, tc.base)
			}
			for _, want := range tc.says {
				if !strings.Contains(got, want) {
					t.Errorf("output = %q, want it to contain %q", got, want)
				}
			}
			if strings.Contains(got, "fatal:") {
				t.Errorf("output = %q, want the engine's sentence rather than git's", got)
			}
		})
	}
}

// A repository whose first commit has not landed is measured from the empty
// tree, so every file in it reads as new.
func TestARepositoryWithNoCommitsReviewsEverythingAsNew(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")

	got := f.mustRun()

	for _, want := range []string{"empty tree", "A  a.txt"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// The wire shape carries the same sentence the prose does, so a caller reading
// JSON is not the one left guessing which base it got.
func TestTheFallbackReachesTheJSON(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	got, _ := f.decode()

	if got.Base.Ref != "HEAD" {
		t.Errorf("base ref = %q, want HEAD", got.Base.Ref)
	}
	if got.Base.Fallback != "uncommitted" {
		t.Errorf("fallback = %q, want it tagged uncommitted", got.Base.Fallback)
	}
}

// A failure writes nothing to stdout even under --json, so a caller parsing the
// stream never has to tell an error apart from a payload.
func TestAFailureLeavesTheJSONStreamEmpty(t *testing.T) {
	f := branched(t)

	stdout, _, err := f.run("status", "--json", "HEAD~1..HEAD")
	if err == nil {
		t.Fatal("an explicit range should be refused")
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
