package git

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenResolvesTheWorkTreeAndTheCommonDir(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	f.commit("first")

	repo := f.open()

	if repo.Root() != f.dir {
		t.Errorf("root = %q, want %q", repo.Root(), f.dir)
	}
	if want := filepath.Join(f.dir, ".git"); repo.CommonDir() != want {
		t.Errorf("common dir = %q, want %q", repo.CommonDir(), want)
	}
}

// A subdirectory is the normal case: zen-review is run from wherever the reader
// happens to be, and rev-parse answers --git-common-dir relative to the process
// unless it is asked for an absolute path.
func TestOpenFromASubdirectoryStillAnswersAbsolutePaths(t *testing.T) {
	f := newFixture(t)
	f.write("deep/nested/a.txt", "one\n")
	f.commit("first")

	repo, err := Open(t.Context(), filepath.Join(f.dir, "deep", "nested"))
	if err != nil {
		t.Fatalf("opening a subdirectory: %v", err)
	}

	if repo.Root() != f.dir {
		t.Errorf("root = %q, want %q", repo.Root(), f.dir)
	}
	if want := filepath.Join(f.dir, ".git"); repo.CommonDir() != want {
		t.Errorf("common dir = %q, want %q", repo.CommonDir(), want)
	}
}

func TestOpenRejectsADirectoryOutsideARepo(t *testing.T) {
	_, err := Open(t.Context(), t.TempDir())

	if !errors.Is(err, ErrNotARepo) {
		t.Fatalf("err = %v, want ErrNotARepo", err)
	}
}

// The review database lives under the common dir, so a throwaway worktree has to
// resolve to its parent's. Getting this wrong means the review disappears with
// the worktree.
func TestALinkedWorktreeSharesTheParentsCommonDir(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	f.commit("first")

	linked := filepath.Join(filepath.Dir(f.dir), "linked")
	f.git("worktree", "add", "-b", "side", linked)

	repo, err := Open(t.Context(), linked)
	if err != nil {
		t.Fatalf("opening the linked worktree: %v", err)
	}

	if repo.Root() != linked {
		t.Errorf("root = %q, want %q", repo.Root(), linked)
	}
	if want := filepath.Join(f.dir, ".git"); repo.CommonDir() != want {
		t.Errorf("common dir = %q, want the parent's %q", repo.CommonDir(), want)
	}
}

// A failing command has to say what was run and what git said, because the caller
// is a TUI that can only show the string.
func TestAFailedCommandReportsTheArgvAndGitsStderr(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	f.commit("first")

	_, err := f.open().RevParse(t.Context(), "nope")
	if err == nil {
		t.Fatal("resolving a ref that does not exist should fail")
	}

	for _, want := range []string{"nope", "rev-parse", "exit status", "Needed a single revision"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
