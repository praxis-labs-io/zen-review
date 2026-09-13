package cli_test

import (
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/version"
)

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

func TestABaseThatCannotBeUsedIsReportedRatherThanRefused(t *testing.T) {
	remoteless := func(t *testing.T) *fixture {
		f := newFixture(t)
		f.Write("a.txt", "one\n")
		f.Commit("first")
		return f
	}

	tests := []struct {
		name string

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
