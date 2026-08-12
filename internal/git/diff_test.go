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

	repo := f.open()
	snap, err := repo.SnapshotTree(t.Context())
	if err != nil {
		t.Fatalf("snapshotting the work tree: %v", err)
	}
	out, err := repo.DiffTrees(t.Context(), base, snap.Tree)
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

// The generation path diffs a tree that has no commit yet, which is what lets a
// caller see the whole changeset before deciding to write one.
func TestDiffTreesReadsATreeWithNoCommit(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "before\n")
	base := f.Commit("first")

	f.Write("a.txt", "after\n")
	f.Write("new.txt", "untracked\n")

	repo := f.open()
	snap, err := repo.SnapshotTree(t.Context())
	if err != nil {
		t.Fatalf("snapshotting: %v", err)
	}

	out, err := repo.DiffTrees(t.Context(), base, snap.Tree)
	if err != nil {
		t.Fatalf("diffing the base against the snapshot: %v", err)
	}
	for _, want := range []string{"b/a.txt", "+after", "b/new.txt", "+untracked"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("diff does not mention %s:\n%s", want, out)
		}
	}
}

// diff.submodule=log writes an embedded repository as a bare "Submodule x"
// line with no "diff --git" header, so the parser above sees no file at all and
// the changeset silently loses a row. The pinned flag is the whole defence.
func TestDiffTreesKeepsAnEmbeddedRepositoryVisible(t *testing.T) {
	inner := newFixture(t)
	inner.Write("f.txt", "inner\n")
	inner.Commit("inner")

	f := newFixture(t)
	f.Write("a.txt", "one\n")
	base := f.Commit("first")

	f.Git("config", "diff.submodule", "log")
	if err := os.Rename(inner.Dir(), filepath.Join(f.Dir(), "sub")); err != nil {
		t.Fatalf("moving a repository inside the work tree: %v", err)
	}

	repo := f.open()
	snap, err := repo.SnapshotTree(t.Context())
	if err != nil {
		t.Fatalf("snapshotting: %v", err)
	}

	out, err := repo.DiffTrees(t.Context(), base, snap.Tree)
	if err != nil {
		t.Fatalf("diffing the base against the snapshot: %v", err)
	}
	if !strings.Contains(string(out), "diff --git a/sub b/sub") {
		t.Errorf("the embedded repository has no file header:\n%s", out)
	}
	if !strings.Contains(string(out), "new file mode 160000") {
		t.Errorf("the embedded repository is not recorded as a gitlink:\n%s", out)
	}
}
