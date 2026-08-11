package git

import (
	"errors"
	"testing"
)

func TestHeadReportsTheBranchAndTheCommit(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	sha := f.commit("first")

	head, err := f.open().Head(t.Context())
	if err != nil {
		t.Fatalf("reading HEAD: %v", err)
	}

	if head.Branch != "main" {
		t.Errorf("branch = %q, want main", head.Branch)
	}
	if head.SHA != sha {
		t.Errorf("sha = %q, want %q", head.SHA, sha)
	}
}

// A detached HEAD is a session keyed on the sha, so an empty branch is the answer
// rather than an error.
func TestADetachedHeadHasNoBranch(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	sha := f.commit("first")
	f.git("checkout", "--detach", sha)

	head, err := f.open().Head(t.Context())
	if err != nil {
		t.Fatalf("reading a detached HEAD: %v", err)
	}

	if head.Branch != "" {
		t.Errorf("branch = %q, want empty", head.Branch)
	}
	if head.SHA != sha {
		t.Errorf("sha = %q, want %q", head.SHA, sha)
	}
}

func TestRevParsePeelsATagToItsCommit(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	sha := f.commit("first")
	f.git("tag", "-a", "v1", "-m", "release")

	got, err := f.open().RevParse(t.Context(), "v1")
	if err != nil {
		t.Fatalf("resolving an annotated tag: %v", err)
	}

	if got != sha {
		t.Errorf("v1 = %q, want the commit %q", got, sha)
	}
}

func TestMergeBaseFindsTheForkPoint(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	fork := f.commit("first")

	f.git("checkout", "-b", "side")
	f.write("b.txt", "side\n")
	f.commit("on the side")

	f.git("checkout", "main")
	f.write("c.txt", "main\n")
	f.commit("on main")

	got, err := f.open().MergeBase(t.Context(), "main", "side")
	if err != nil {
		t.Fatalf("finding the merge base: %v", err)
	}

	if got != fork {
		t.Errorf("merge base = %q, want the fork point %q", got, fork)
	}
}

// Unrelated histories are what a base force-push that loses the fork point looks
// like from here, and the spec answers it with a new base rather than a crash.
func TestMergeBaseReportsUnrelatedHistories(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	f.commit("first")

	f.git("checkout", "--orphan", "unrelated")
	f.git("rm", "-rf", ".")
	f.write("b.txt", "other\n")
	f.commit("a history of its own")

	_, err := f.open().MergeBase(t.Context(), "main", "unrelated")

	if !errors.Is(err, ErrNoMergeBase) {
		t.Fatalf("err = %v, want ErrNoMergeBase", err)
	}
}

func TestIsAncestor(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	first := f.commit("first")
	f.write("a.txt", "two\n")
	second := f.commit("second")

	repo := f.open()

	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"a parent is an ancestor", first, second, true},
		{"a child is not", second, first, false},
		{"a commit is its own ancestor", first, first, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.IsAncestor(t.Context(), tt.a, tt.b)
			if err != nil {
				t.Fatalf("checking ancestry: %v", err)
			}
			if got != tt.want {
				t.Errorf("IsAncestor = %v, want %v", got, tt.want)
			}
		})
	}
}

// The refs are written by hand rather than by cloning: this is the state a clone
// leaves behind, and what is under test is whether origin/HEAD is read, not
// whether git can fetch.
func TestDefaultRemoteBranchReadsOriginHead(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	sha := f.commit("first")
	f.git("update-ref", "refs/remotes/origin/main", sha)
	f.git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	got, err := f.open().DefaultRemoteBranch(t.Context())
	if err != nil {
		t.Fatalf("reading origin/HEAD: %v", err)
	}

	if got != "origin/main" {
		t.Errorf("default branch = %q, want origin/main", got)
	}
}

func TestDefaultRemoteBranchSaysSoWhenThereIsNoRemote(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	f.commit("first")

	_, err := f.open().DefaultRemoteBranch(t.Context())

	if !errors.Is(err, ErrNoDefaultBranch) {
		t.Fatalf("err = %v, want ErrNoDefaultBranch", err)
	}
}

func TestLocalBranchesListsEveryHeadWithItsCommit(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\n")
	main := f.commit("first")
	f.git("branch", "side")
	f.git("branch", "feature/nested-name")

	got, err := f.open().LocalBranches(t.Context())
	if err != nil {
		t.Fatalf("listing branches: %v", err)
	}

	want := []Branch{
		{Name: "feature/nested-name", SHA: main},
		{Name: "main", SHA: main},
		{Name: "side", SHA: main},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d branches, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("branch %d = %v, want %v", i, got[i], want[i])
		}
	}
}
