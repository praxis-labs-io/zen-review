package git

import (
	"errors"
	"os"
	"slices"
	"testing"
)

func TestHeadReportsTheBranchAndTheCommit(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	sha := f.Commit("first")

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

// A repository whose first commit has not landed is a state to review from, so
// it answers Unborn rather than failing. The branch name is still there.
func TestAnUnbornHeadIsAnAnswerRatherThanAFailure(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")

	head, err := f.open().Head(t.Context())
	if err != nil {
		t.Fatalf("reading an unborn HEAD: %v", err)
	}

	if !head.Unborn() {
		t.Errorf("head = %+v, want it to read as unborn", head)
	}
	if head.Branch != "main" {
		t.Errorf("branch = %q, want main", head.Branch)
	}
}

// The empty tree is what an unborn HEAD is measured from. It is asked of git
// rather than hardcoded, so a repository on sha256 gets its own.
func TestEmptyTreeIsTheTreeWithNothingInIt(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	got, err := f.open().EmptyTree(t.Context())
	if err != nil {
		t.Fatalf("writing the empty tree: %v", err)
	}

	if want := f.Git("hash-object", "-t", "tree", os.DevNull); got != want {
		t.Errorf("empty tree = %q, want %q", got, want)
	}
}

// A detached HEAD is a session keyed on the sha, so an empty branch is the answer
// rather than an error.
func TestADetachedHeadHasNoBranch(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	sha := f.Commit("first")
	f.Git("checkout", "--detach", sha)

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
	f.Write("a.txt", "one\n")
	sha := f.Commit("first")
	f.Git("tag", "-a", "v1", "-m", "release")

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
	f.Write("a.txt", "one\n")
	fork := f.Commit("first")

	f.Git("checkout", "-b", "side")
	f.Write("b.txt", "side\n")
	f.Commit("on the side")

	f.Git("checkout", "main")
	f.Write("c.txt", "main\n")
	f.Commit("on main")

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
	f.Write("a.txt", "one\n")
	f.Commit("first")

	f.Git("checkout", "--orphan", "unrelated")
	f.Git("rm", "-rf", ".")
	f.Write("b.txt", "other\n")
	f.Commit("a history of its own")

	_, err := f.open().MergeBase(t.Context(), "main", "unrelated")

	if !errors.Is(err, ErrNoMergeBase) {
		t.Fatalf("err = %v, want ErrNoMergeBase", err)
	}
}

// The refs are written by hand rather than by cloning: this is the state a clone
// leaves behind, and what is under test is whether origin/HEAD is read, not
// whether git can fetch.
func TestDefaultRemoteBranchReadsOriginHead(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	sha := f.Commit("first")
	f.Git("update-ref", "refs/remotes/origin/main", sha)
	f.Git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

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
	f.Write("a.txt", "one\n")
	f.Commit("first")

	_, err := f.open().DefaultRemoteBranch(t.Context())

	if !errors.Is(err, ErrNoDefaultBranch) {
		t.Fatalf("err = %v, want ErrNoDefaultBranch", err)
	}
}

func TestLocalBranchesListsEveryHeadWithItsCommit(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	main := f.Commit("first")
	f.Git("branch", "side")
	f.Git("branch", "feature/nested-name")

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

func TestRemoteBranchesLeaveOutSymbolicAliases(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	main := f.Commit("first")
	f.Git("update-ref", "refs/remotes/origin/main", main)
	f.Git("update-ref", "refs/remotes/upstream/stable", main)
	f.Git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	got, err := f.open().RemoteBranches(t.Context())
	if err != nil {
		t.Fatalf("listing remote branches: %v", err)
	}
	want := []Branch{{Name: "origin/main", SHA: main}, {Name: "upstream/stable", SHA: main}}
	if !slices.Equal(got, want) {
		t.Errorf("branches = %v, want %v", got, want)
	}
}

// The first-parent chain is what tells a branch HEAD was cut from apart from one
// merged into it. Both are ancestors of HEAD, and only the first is on the chain.
func TestFirstParentsSkipsWhatWasMergedIn(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	base := f.Commit("first")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("a.txt", "two\n")
	cut := f.Commit("on the branch")

	f.Git("checkout", "-q", "-b", "side")
	f.Write("side.txt", "aside\n")
	merged := f.Commit("on the side")

	f.Git("checkout", "-q", "feature")
	f.Git("merge", "-q", "--no-ff", "-m", "merge side", "side")

	chain, err := f.open().FirstParents(t.Context(), base, "feature")
	if err != nil {
		t.Fatalf("walking the first parents: %v", err)
	}

	on := make(map[string]bool, len(chain))
	for _, sha := range chain {
		on[sha] = true
	}
	if !on[cut] {
		t.Errorf("the commit the branch was cut at is not on the chain: %v", chain)
	}
	if on[merged] {
		t.Errorf("a merged side branch's commit is on the chain: %v", chain)
	}
}

// An empty range is an answer, not a failure: a branch sitting at its base has
// nothing above it.
func TestFirstParentsOfNothingIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	sha := f.Commit("first")

	chain, err := f.open().FirstParents(t.Context(), sha, sha)
	if err != nil {
		t.Fatalf("walking the first parents: %v", err)
	}
	if len(chain) != 0 {
		t.Errorf("chain = %v, want empty", chain)
	}
}

func TestAheadCountsWhatTheTipHasBeyondTheBase(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	base := f.Commit("first")

	f.Git("checkout", "-q", "-b", "feature")
	for _, n := range []string{"two", "three", "four"} {
		f.Write("a.txt", n+"\n")
		f.Commit(n)
	}
	repo := f.open()

	got, err := repo.Ahead(t.Context(), base, "feature")
	if err != nil {
		t.Fatalf("counting ahead: %v", err)
	}
	if got != 3 {
		t.Errorf("ahead = %d, want 3", got)
	}

	// The count is one-directional. Reading it the wrong way round would sort
	// every stack candidate the same distance from HEAD.
	behind, err := repo.Ahead(t.Context(), "feature", base)
	if err != nil {
		t.Fatalf("counting the other way: %v", err)
	}
	if behind != 0 {
		t.Errorf("ahead of the tip = %d, want 0", behind)
	}
}

// A ref that does not resolve has to fail rather than answer 0, which would read
// as no distance and sort a typo to the front of the candidate list.
func TestAheadRefusesARefThatDoesNotResolve(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	if _, err := f.open().Ahead(t.Context(), "no-such-ref", "HEAD"); err == nil {
		t.Fatal("counting from a ref that does not exist should fail")
	}
}

// A session that has never refreshed has no ref, and that is its normal first
// state rather than a failure to report.
func TestRefShaAnswersFalseForARefThatDoesNotExist(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	sha, found, err := f.open().RefSha(t.Context(), "refs/zen-review/sessions/nothing")
	if err != nil {
		t.Fatalf("reading a ref that does not exist: %v", err)
	}
	if found {
		t.Errorf("found = true for a ref that was never written, sha %q", sha)
	}
}

func TestRefShaReadsARefThatExists(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	want := f.Commit("first")

	const ref = "refs/zen-review/sessions/abc"
	f.Git("update-ref", ref, want)

	sha, found, err := f.open().RefSha(t.Context(), ref)
	if err != nil {
		t.Fatalf("reading the ref: %v", err)
	}
	if !found {
		t.Fatal("found = false for a ref that was just written")
	}
	if sha != want {
		t.Errorf("sha = %q, want %q", sha, want)
	}
}

// A ref that names nothing is an answer, and a git that broke is not. Reading
// the two as one drops a caller onto a different base with nothing said.
func TestResolveTellsAMissingRefFromABrokenGit(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	sha := f.Commit("first")

	got, ok, err := f.open().Resolve(t.Context(), "main")
	if err != nil || !ok || got != sha {
		t.Errorf("Resolve(main) = %q, %v, %v, want %q, true, nil", got, ok, err, sha)
	}

	if _, ok, err := f.open().Resolve(t.Context(), "no-such-ref"); err != nil || ok {
		t.Errorf("Resolve(no-such-ref) = %v, %v, want false and no error", ok, err)
	}
}

// An empty base walks the whole chain, which is what a tip with nothing above
// it to bound the walk needs.
func TestFirstParentsWithNoBaseWalksTheWholeChain(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	first := f.Commit("first")
	f.Write("a.txt", "two\n")
	second := f.Commit("second")

	got, err := f.open().FirstParents(t.Context(), "", "HEAD")
	if err != nil {
		t.Fatalf("walking the whole chain: %v", err)
	}

	if len(got) != 2 || got[0] != second || got[1] != first {
		t.Errorf("chain = %v, want %s then %s", got, second, first)
	}
}
