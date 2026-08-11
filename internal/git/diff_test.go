package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twenty lines, so a hunk in the middle of it has context to spare on both sides.
func twentyLines(marker string) string {
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		if i == 10 {
			b.WriteString(marker + "\n")
			continue
		}
		b.WriteString("line\n")
	}
	return b.String()
}

// Whether the agent committed is an accident of its behaviour. A file it staged
// and a file it left in the working tree are one changeset.
func TestDiffCombinesStagedAndUnstagedChanges(t *testing.T) {
	f := newFixture(t)
	f.Write("staged.txt", "before\n")
	f.Write("loose.txt", "before\n")
	base := f.Commit("first")

	f.Write("staged.txt", "after\n")
	f.Git("add", "staged.txt")
	f.Write("loose.txt", "after\n")

	out, err := f.open().Diff(t.Context(), base)
	if err != nil {
		t.Fatalf("diffing against the base: %v", err)
	}

	for _, want := range []string{"b/staged.txt", "b/loose.txt"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("diff does not mention %s:\n%s", want, out)
		}
	}
}

// A file committed and then edited again is one set of hunks, not two.
func TestDiffAcrossACommitAndAFurtherEditIsOneChange(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	base := f.Commit("first")

	f.Write("a.txt", "two\n")
	f.Commit("committed by the agent")
	f.Write("a.txt", "three\n")

	out, err := f.open().Diff(t.Context(), base)
	if err != nil {
		t.Fatalf("diffing against the base: %v", err)
	}

	if got := strings.Count(string(out), "@@ "); got != 1 {
		t.Errorf("got %d hunks, want 1:\n%s", got, out)
	}
	if !strings.Contains(string(out), "+three") {
		t.Errorf("diff does not reach the working tree:\n%s", out)
	}
	if strings.Contains(string(out), "+two") {
		t.Errorf("diff shows the intermediate commit:\n%s", out)
	}
}

// The parser reads a fixed shape. Every flag pinned in diffFlags is one a user or
// a repository is entitled to set the other way, and a diff that arrives without
// prefixes or with seven lines of context parses into the wrong thing.
func TestDiffPinsTheFlagsUserConfigCouldMove(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", twentyLines("before"))
	f.Write("café.txt", "one\n")
	base := f.Commit("first")

	f.Git("config", "diff.noprefix", "true")
	f.Git("config", "diff.context", "7")
	f.Git("config", "core.quotePath", "true")

	f.Write("a.txt", twentyLines("after"))
	f.Write("café.txt", "two\n")

	out, err := f.open().Diff(t.Context(), base)
	if err != nil {
		t.Fatalf("diffing against the base: %v", err)
	}

	tests := []struct {
		name string
		want string
	}{
		{"prefixes survive diff.noprefix", "--- a/a.txt"},
		{"three lines of context survive diff.context", "@@ -7,7 +7,7 @@"},
		{"a non-ASCII path survives core.quotePath", "b/café.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("diff does not contain %q:\n%s", tt.want, out)
			}
		})
	}

	t.Run("the index lines carry full blob shas", func(t *testing.T) {
		assertFullIndex(t, string(out))
	})
}

// assertFullIndex checks every index line carries unabbreviated shas, since a
// generation's blob identity comes from them and an abbreviation is not an
// identity.
func assertFullIndex(t *testing.T, diff string) {
	t.Helper()

	found := false
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "index ") {
			continue
		}
		found = true

		shas := strings.TrimPrefix(line, "index ")
		if i := strings.Index(shas, " "); i >= 0 {
			shas = shas[:i]
		}
		before, after, ok := strings.Cut(shas, "..")
		if !ok || len(before) != 40 || len(after) != 40 {
			t.Errorf("index line is abbreviated: %q", line)
		}
	}
	if !found {
		t.Error("the diff has no index line at all")
	}
}

func TestUntrackedSkipsIgnoredFilesAndReachesIntoNewDirectories(t *testing.T) {
	f := newFixture(t)
	f.Write(".gitignore", "ignored.txt\n")
	f.Commit("first")

	f.Write("ignored.txt", "invisible\n")
	f.Write("loose.txt", "new\n")
	f.Write("fresh/dir/deep.txt", "new\n")

	got, err := f.open().Untracked(t.Context())
	if err != nil {
		t.Fatalf("listing untracked files: %v", err)
	}

	want := []string{"fresh/dir/deep.txt", "loose.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// --no-index implies --exit-code, so a difference arrives as exit status 1.
// Reading that as a failure makes every untracked file fatal.
func TestDiffNoIndexTreatsADifferenceAsAnAnswer(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Write("new.txt", "fresh\n")

	out, err := f.open().DiffNoIndex(t.Context(), os.DevNull, "new.txt")
	if err != nil {
		t.Fatalf("diffing an untracked file: %v", err)
	}

	for _, want := range []string{"b/new.txt", "new file mode", "+fresh"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("diff does not contain %q:\n%s", want, out)
		}
	}
}

// The index is shared with whatever the agent is running, so producing this diff
// must not write to it. `git add --intent-to-add` would.
func TestDiffNoIndexLeavesTheIndexAlone(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Write("new.txt", "fresh\n")

	before := f.Git("status", "--porcelain")
	if _, err := f.open().DiffNoIndex(t.Context(), os.DevNull, "new.txt"); err != nil {
		t.Fatalf("diffing an untracked file: %v", err)
	}

	if after := f.Git("status", "--porcelain"); after != before {
		t.Errorf("status changed from %q to %q", before, after)
	}
}

// `git diff --no-index` exits 1 both for "the files differ" and for a path it
// could not read. Reading the second as an answer means an untracked entry git
// will not diff leaves the changeset with no row and no reason.
func TestDiffNoIndexSeparatesADifferenceFromAFailure(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Write("sub/f.txt", "inside a directory\n")

	tests := []struct {
		name    string
		to      string
		wantErr bool
	}{
		{"a file that differs", "a.txt", false},
		{"a directory", "sub", true},
		{"a path that is not there", "nope.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := f.open().DiffNoIndex(t.Context(), os.DevNull, tt.to)
			switch {
			case tt.wantErr && err == nil:
				t.Errorf("got no error and %d bytes of diff, want a failure", len(out))
			case !tt.wantErr && err != nil:
				t.Errorf("diffing %s: %v", tt.to, err)
			}
		})
	}
}

// A hook and `git rebase --exec` both run with GIT_DIR set, and git honours it over
// the process's directory. Inheriting it means every command answers about a
// repository the caller never asked for.
func TestAnInheritedGitDirDoesNotMoveTheRepository(t *testing.T) {
	elsewhere := newFixture(t)
	elsewhere.Write("a.txt", "one\n")
	elsewhere.Commit("first")

	f := newFixture(t)
	f.Write("a.txt", "one\n")
	want := f.Commit("first")

	t.Setenv("GIT_DIR", filepath.Join(elsewhere.Dir(), ".git"))
	t.Setenv("GIT_WORK_TREE", elsewhere.Dir())

	repo, err := Open(t.Context(), f.Dir())
	if err != nil {
		t.Fatalf("opening with GIT_DIR set elsewhere: %v", err)
	}
	if repo.Root() != f.Dir() {
		t.Errorf("root = %q, want %q", repo.Root(), f.Dir())
	}

	head, err := repo.Head(t.Context())
	if err != nil {
		t.Fatalf("reading HEAD: %v", err)
	}
	if head.SHA != want {
		t.Errorf("HEAD = %q, want this repository's %q", head.SHA, want)
	}
}
