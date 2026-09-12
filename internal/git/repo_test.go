package git

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenResolvesTheWorkTreeAndTheCommonDir(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	repo := f.open()

	if repo.Root() != f.Dir() {
		t.Errorf("root = %q, want %q", repo.Root(), f.Dir())
	}
	if want := filepath.Join(f.Dir(), ".git"); repo.CommonDir() != want {
		t.Errorf("common dir = %q, want %q", repo.CommonDir(), want)
	}
}

func TestOpenFromASubdirectoryStillAnswersAbsolutePaths(t *testing.T) {
	f := newFixture(t)
	f.Write("deep/nested/a.txt", "one\n")
	f.Commit("first")

	repo, err := Open(t.Context(), filepath.Join(f.Dir(), "deep", "nested"))
	if err != nil {
		t.Fatalf("opening a subdirectory: %v", err)
	}

	if repo.Root() != f.Dir() {
		t.Errorf("root = %q, want %q", repo.Root(), f.Dir())
	}
	if want := filepath.Join(f.Dir(), ".git"); repo.CommonDir() != want {
		t.Errorf("common dir = %q, want %q", repo.CommonDir(), want)
	}
}

func TestOpenRejectsADirectoryOutsideARepo(t *testing.T) {
	_, err := Open(t.Context(), t.TempDir())

	if !errors.Is(err, ErrNotARepo) {
		t.Fatalf("err = %v, want ErrNotARepo", err)
	}
}

func TestALinkedWorktreeSharesTheParentsCommonDir(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	linked := filepath.Join(filepath.Dir(f.Dir()), "linked")
	f.Git("worktree", "add", "-b", "side", linked)

	repo, err := Open(t.Context(), linked)
	if err != nil {
		t.Fatalf("opening the linked worktree: %v", err)
	}

	if repo.Root() != linked {
		t.Errorf("root = %q, want %q", repo.Root(), linked)
	}
	if want := filepath.Join(f.Dir(), ".git"); repo.CommonDir() != want {
		t.Errorf("common dir = %q, want the parent's %q", repo.CommonDir(), want)
	}
}

func TestAFailedCommandReportsTheArgvAndGitsStderr(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

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
