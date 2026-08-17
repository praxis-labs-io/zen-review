package review_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/testrepo"
)

func TestMain(m *testing.M) { os.Exit(testrepo.Main(m)) }

// fixture is a real repository with a real database under it, plus the two
// helpers only this package wants. Nothing is mocked: what these tests check is
// the resolution order and the candidate rule, and both are answers about a
// repository's actual shape.
type fixture struct {
	*testrepo.Repo
	t *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{Repo: testrepo.New(t), t: t}
}

// commit writes the message into a file first, so each call is a distinct commit
// rather than one git refuses as empty.
func (f *fixture) commit(message string) string {
	f.t.Helper()

	f.Write("a.txt", message+"\n")
	return f.Commit(message)
}

func (f *fixture) open(base string) (*review.Session, error) {
	f.t.Helper()
	return review.Open(f.t.Context(), f.Dir(), review.Options{BaseRef: base})
}

func (f *fixture) mustOpen(base string) *review.Session {
	f.t.Helper()

	s, err := f.open(base)
	if err != nil {
		f.t.Fatalf("opening the session: %v", err)
	}
	f.t.Cleanup(func() {
		if err := s.Close(); err != nil {
			f.t.Errorf("closing the session: %v", err)
		}
	})
	return s
}

// branched is the common shape: a commit on main tracked as origin/main, then a
// feature branch with a commit on it.
func branched(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	f.commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	f.commit("second")
	return f
}

// The whole point of a session: come back three days later, same branch, same
// state. The id has to be the same both times or nothing above this can find
// what was reviewed.
func TestASessionResumesAcrossOpens(t *testing.T) {
	f := branched(t)

	first := f.mustOpen("")
	id, base := first.ID(), first.Base()
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first session: %v", err)
	}

	second := f.mustOpen("")

	if second.ID() != id {
		t.Errorf("id = %s, want %s", second.ID(), id)
	}
	if second.Base() != base {
		t.Errorf("base = %+v, want %+v", second.Base(), base)
	}
	if second.Kind() != store.KindBranch {
		t.Errorf("kind = %s, want %s", second.Kind(), store.KindBranch)
	}
	if second.Branch() != "feature" {
		t.Errorf("branch = %s, want feature", second.Branch())
	}
}

// The base is set once per branch and sticks. Re-detecting it on every run is
// how a base moves under a half-finished review, and every range already
// reviewed was measured from the old one.
func TestTheBaseSticksEvenWhenDetectionWouldNowDisagree(t *testing.T) {
	f := branched(t)

	first := f.mustOpen("")
	if first.Base().Ref != "origin/main" {
		t.Fatalf("base = %s, want origin/main", first.Base().Ref)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first session: %v", err)
	}

	// origin/HEAD moves to a different branch, so a fresh detection would answer
	// origin/develop.
	f.Git("update-ref", "refs/remotes/origin/develop", f.Git("rev-parse", "main"))
	f.Git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")

	if got := f.mustOpen("").Base().Ref; got != "origin/main" {
		t.Errorf("base = %s, want the stored origin/main", got)
	}
}

// The flag replaces whatever was stuck, and the replacement sticks in its turn.
func TestTheBaseFlagOverridesAndThenSticks(t *testing.T) {
	f := branched(t)
	f.Git("branch", "other", "main")

	if got := f.mustOpen("").Base().Ref; got != "origin/main" {
		t.Fatalf("base = %s, want origin/main", got)
	}
	if got := f.mustOpen("other").Base().Ref; got != "other" {
		t.Fatalf("base = %s, want the flag's other", got)
	}
	if got := f.mustOpen("").Base().Ref; got != "other" {
		t.Errorf("base = %s, want other to have stuck", got)
	}
}

// Measuring a stacked branch from origin/main shows the parent branch's commits
// as this branch's work, so the nearest branch under it wins instead.
func TestAStackedBranchTakesTheNearestBranchUnderIt(t *testing.T) {
	f := newFixture(t)
	f.commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "stack-bottom")
	f.commit("bottom")
	f.Git("checkout", "-q", "-b", "stack-middle")
	f.commit("middle")
	f.Git("checkout", "-q", "-b", "stack-top")
	f.commit("top")

	// Nearest first: the branch it was actually cut from beats the one under that.
	base := f.mustOpen("").Base()
	if base.Ref != "stack-middle" {
		t.Errorf("base = %s, want stack-middle", base.Ref)
	}
	if base.Fallback != "origin/main is not the fork point" {
		t.Errorf("fallback = %q, want it to name what it passed over", base.Fallback)
	}
	if want := "origin/main is not the fork point, so the base is stack-middle"; base.Explain() != want {
		t.Errorf("explain = %q, want %q", base.Explain(), want)
	}

	// The flag still settles it, and what it settles on is not a fallback.
	chosen := f.mustOpen("stack-bottom").Base()
	if chosen.Ref != "stack-bottom" || chosen.Fallback != "" {
		t.Errorf("base = %+v, want stack-bottom with nothing to explain", chosen)
	}
}

// A local main left behind origin/main is an ancestor of HEAD and is nothing
// anyone stacked on. In a workflow where local main is never checked out this is
// the common case, so reading it as a stack would refuse on every branch.
func TestALocalBranchAlreadyInTheBaseIsNotACandidate(t *testing.T) {
	f := newFixture(t)
	f.commit("first")

	// stale sits where main was before the remote moved on.
	f.Git("branch", "stale", "main")
	f.commit("second")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.commit("third")

	s, err := f.open("")
	if err != nil {
		t.Fatalf("a branch behind the base is not a stack, but opening failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if s.Base().Ref != "origin/main" {
		t.Errorf("base = %s, want origin/main", s.Base().Ref)
	}
}

// TestRepoNamesTheRepository. It is what the reader is shown to say which
// repository is on screen, so it has to be a name rather than the path to one.
//
// A linked worktree gets the same name as the checkout it came from: the two
// share one session, keyed on the common directory, and one review answering to
// two names depending on where it was opened is worse than a name that is not
// the directory you are standing in.
func TestRepoNamesTheRepository(t *testing.T) {
	f := branched(t)
	want := filepath.Base(f.Dir())

	s := f.mustOpen("")
	t.Cleanup(func() { _ = s.Close() })

	if got := s.Repo(); got != want {
		t.Errorf("Repo() = %q, want %q", got, want)
	}
	if strings.ContainsRune(s.Repo(), filepath.Separator) {
		t.Errorf("Repo() = %q, want a name and not a path", s.Repo())
	}

	linked := filepath.Join(t.TempDir(), "a-worktree-of-its-own")
	f.Git("worktree", "add", "-q", "-b", "side", linked)

	w, err := review.Open(t.Context(), linked, review.Options{})
	if err != nil {
		t.Fatalf("opening a session in a worktree: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if got := w.Repo(); got != want {
		t.Errorf("from a worktree Repo() = %q, want %q", got, want)
	}
}

// A branch merged into HEAD is an ancestor of HEAD and not of the base, which is
// every test ancestry alone can apply, and it is not a stack. Merging local main
// into a feature branch to catch up is the everyday version, and reading it as a
// stack refuses to open a branch there is nothing wrong with.
func TestABranchMergedIntoHeadIsNotACandidate(t *testing.T) {
	f := branched(t)

	f.Git("checkout", "-q", "-b", "side")
	f.commit("on the side")
	f.Git("checkout", "-q", "feature")
	f.Git("merge", "-q", "--no-ff", "-m", "merge side", "side")

	s, err := f.open("")
	if err != nil {
		t.Fatalf("a merged branch is not a stack, but opening failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if s.Base().Ref != "origin/main" {
		t.Errorf("base = %s, want origin/main", s.Base().Ref)
	}
}

// Measuring from a branch sitting exactly at HEAD leaves an empty changeset, so
// it is not something to offer.
func TestABranchTipAtHeadIsNotACandidate(t *testing.T) {
	f := branched(t)
	f.Git("branch", "alias")

	s, err := f.open("")
	if err != nil {
		t.Fatalf("a branch at HEAD is not a stack, but opening failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if s.Base().Ref != "origin/main" {
		t.Errorf("base = %s, want origin/main", s.Base().Ref)
	}
}

// A detached HEAD has no branch to key on, so it keys on the sha and gets a
// session of its own rather than borrowing the branch's.
func TestADetachedHeadGetsItsOwnSession(t *testing.T) {
	f := branched(t)

	onBranch := f.mustOpen("")
	branchID := onBranch.ID()
	if err := onBranch.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	f.Git("checkout", "-q", "--detach", "HEAD")
	detached := f.mustOpen("")

	if detached.ID() == branchID {
		t.Errorf("a detached HEAD reused the branch's session %s", branchID)
	}
	if detached.Kind() != store.KindDetached {
		t.Errorf("kind = %s, want %s", detached.Kind(), store.KindDetached)
	}
	if detached.Branch() != "" {
		t.Errorf("branch = %s, want empty on a detached HEAD", detached.Branch())
	}
	if err := detached.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	// And going back returns the branch's session rather than a third one.
	f.Git("checkout", "-q", "feature")
	if got := f.mustOpen("").ID(); got != branchID {
		t.Errorf("id = %s, want the branch's %s", got, branchID)
	}
}

// The database lives under the common dir, so reviewing in a throwaway worktree
// and deleting it does not take the review with it.
func TestALinkedWorktreeWritesToItsParentsDatabase(t *testing.T) {
	f := branched(t)

	tree := filepath.Join(filepath.Dir(f.Dir()), "linked")
	f.Git("worktree", "add", "-q", "-b", "in-the-worktree", tree, "main")

	s, err := review.Open(t.Context(), tree, review.Options{})
	if err != nil {
		t.Fatalf("opening a session in a linked worktree: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	parent := filepath.Join(f.Dir(), ".git", "zen-review", "state.db")
	if _, err := os.Stat(parent); err != nil {
		t.Fatalf("the worktree's review is not in the parent's database: %v", err)
	}

	// And the row is really there, not just the file.
	db, err := store.Open(t.Context(), parent)
	if err != nil {
		t.Fatalf("opening the parent's database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, found, err := db.Session(t.Context(), s.ID()); err != nil || !found {
		t.Errorf("session %s is not in the parent's database: found = %v, err = %v", s.ID(), found, err)
	}
}

func TestStartupFailuresSayWhatToDo(t *testing.T) {
	t.Run("outside a repository", func(t *testing.T) {
		if _, err := review.Open(t.Context(), t.TempDir(), review.Options{}); err == nil {
			t.Fatal("opening outside a repository should fail")
		}
	})

	// A database that will not open is not always a permissions problem. A corrupt
	// file and one written by a newer build both arrive here, and blaming
	// permissions tells the reader to fix something that is already fine.
	t.Run("with a database that is not a database", func(t *testing.T) {
		f := branched(t)

		path := filepath.Join(f.Dir(), ".git", "zen-review", "state.db")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating the database directory: %v", err)
		}
		if err := os.WriteFile(path, []byte("not a database"), 0o644); err != nil {
			t.Fatalf("writing over the database: %v", err)
		}

		_, err := f.open("")
		if err == nil {
			t.Fatal("a corrupt database should not open")
		}
		if strings.Contains(err.Error(), "writable") {
			t.Errorf("err = %v, want it not to blame permissions", err)
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("err = %v, want it to name %s", err, path)
		}
	})
}

// Every way a base fails to name a fork point drops a rung and says so, rather
// than refusing: a reader who cannot open the tool cannot change the base.
func TestABaseThatCannotBeUsedFallsBackAndSaysWhy(t *testing.T) {
	tests := []struct {
		name string

		// setup runs on a branched fixture and returns the base to open with.
		setup func(f *fixture) string

		want string
		says []string
	}{
		{
			name:  "a base that does not resolve",
			setup: func(*fixture) string { return "no-such-ref" },
			want:  "origin/main",
			says:  []string{"no-such-ref", "does not resolve"},
		},
		{
			name: "a stored base that has since gone",
			setup: func(f *fixture) string {
				f.mustOpen("chosen")
				f.Git("branch", "-D", "chosen")
				return ""
			},
			want: "origin/main",
			says: []string{"chosen", "does not resolve"},
		},
		{
			// origin/HEAD is symbolic and outlives what it points at, so renaming
			// the remote's default branch leaves it naming a ref that is gone.
			name: "an origin/HEAD pointing at nothing",
			setup: func(f *fixture) string {
				f.Git("update-ref", "-d", "refs/remotes/origin/main")
				return ""
			},
			want: "main",
			says: []string{"origin/main", "is gone"},
		},
		{
			// A base force-push that loses the fork point looks like this from here.
			name: "a base sharing no history with the branch",
			setup: func(f *fixture) string {
				f.Git("checkout", "-q", "--orphan", "unrelated")
				f.commit("nothing in common")
				f.Git("checkout", "-q", "feature")
				return "unrelated"
			},
			want: "origin/main",
			says: []string{"unrelated", "shares no history"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := branched(t)
			f.Git("branch", "chosen", "main")

			base := f.mustOpen(tc.setup(f)).Base()

			if base.Ref != tc.want {
				t.Errorf("base = %s, want %s", base.Ref, tc.want)
			}
			for _, want := range tc.says {
				if !strings.Contains(base.Fallback, want) {
					t.Errorf("fallback = %q, want it to contain %q", base.Fallback, want)
				}
			}

			// The short reason is what sits beside the base on screen. The
			// sentence the CLI prints adds what was taken for it.
			if want := ", so the base is " + tc.want; !strings.HasSuffix(base.Explain(), want) {
				t.Errorf("explain = %q, want it to end %q", base.Explain(), want)
			}
		})
	}
}

// With no origin/HEAD the ladder tries a local default branch, then HEAD, where
// the changeset is whatever has not been committed.
func TestARepositoryWithNoRemoteFallsToTheLocalDefaultThenHead(t *testing.T) {
	t.Run("to a local main", func(t *testing.T) {
		f := newFixture(t)
		f.commit("first")
		f.Git("checkout", "-q", "-b", "feature")
		f.commit("second")

		base := f.mustOpen("").Base()

		if base.Ref != "main" {
			t.Errorf("base = %s, want main", base.Ref)
		}
		if !strings.Contains(base.Fallback, "no origin/HEAD") {
			t.Errorf("fallback = %q, want it to say there is no origin/HEAD", base.Fallback)
		}
	})

	t.Run("to HEAD on the default branch itself", func(t *testing.T) {
		f := newFixture(t)
		f.commit("first")

		base := f.mustOpen("").Base()

		if base.Ref != "HEAD" {
			t.Errorf("base = %s, want HEAD", base.Ref)
		}
		if base.Fallback != "no origin/HEAD" {
			t.Errorf("fallback = %q, want it to say there is no origin/HEAD", base.Fallback)
		}
		if !strings.Contains(base.Explain(), "has not been committed") {
			t.Errorf("explain = %q, want it to say what the changeset is", base.Explain())
		}
	})
}

// A repository whose first commit has not landed has nothing to measure from,
// so the other side is the empty tree and every file reads as new.
func TestAnUnbornHeadIsMeasuredFromTheEmptyTree(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")

	s := f.mustOpen("")

	if !s.Base().EmptyTree() {
		t.Fatalf("base = %+v, want the empty tree", s.Base())
	}
	if s.Base().Fallback != "no commits" {
		t.Errorf("fallback = %q, want it to say there are no commits", s.Base().Fallback)
	}
	if !strings.Contains(s.Base().Explain(), "every file reads as new") {
		t.Errorf("explain = %q, want it to say what the changeset is", s.Base().Explain())
	}

	files := f.changeset(s, f.refresh(s)).Files
	if len(files) != 1 || files[0].Diff.Path != "a.txt" {
		t.Fatalf("files = %+v, want a.txt alone", files)
	}
	if files[0].Diff.Status != diff.FileAdded {
		t.Errorf("status = %v, want the file to read as added", files[0].Diff.Status)
	}
}

// A branch stacked on another local branch is measured from the branch under it.
// origin/main would read the parent's commits as this branch's work.
func TestAStackedBranchIsMeasuredFromTheBranchBelowIt(t *testing.T) {
	f := branched(t)
	f.Git("checkout", "-q", "-b", "child")
	f.commit("on the child")

	base := f.mustOpen("").Base()

	if base.Ref != "feature" {
		t.Errorf("base = %s, want feature", base.Ref)
	}
	if want := "origin/main is not the fork point, so the base is feature"; base.Explain() != want {
		t.Errorf("explain = %q, want %q", base.Explain(), want)
	}
}

// A fallback is a guess made because the base asked for was not there, so it is
// made again once the repository moves rather than kept.
func TestAFallbackBaseIsNotStored(t *testing.T) {
	f := branched(t)
	f.Git("update-ref", "-d", "refs/remotes/origin/main")

	if got := f.mustOpen("").Base().Ref; got != "main" {
		t.Fatalf("base = %s, want main", got)
	}

	f.TrackOrigin("main")

	base := f.mustOpen("").Base()
	if base.Ref != "origin/main" {
		t.Errorf("base = %s, want origin/main once origin/HEAD is back", base.Ref)
	}
	if base.Fallback != "" {
		t.Errorf("fallback = %q, want nothing left to explain", base.Fallback)
	}
}
