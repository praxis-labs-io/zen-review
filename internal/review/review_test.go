package review_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
// as this branch's work. Refusing and naming the candidates beats guessing.
func TestAStackedBranchRefusesAndNamesItsCandidates(t *testing.T) {
	f := newFixture(t)
	f.commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "stack-bottom")
	f.commit("bottom")
	f.Git("checkout", "-q", "-b", "stack-middle")
	f.commit("middle")
	f.Git("checkout", "-q", "-b", "stack-top")
	f.commit("top")

	_, err := f.open("")
	if err == nil {
		t.Fatal("a stacked branch should refuse rather than measure from origin/main")
	}

	var stacked *review.StackedError
	if !errors.As(err, &stacked) {
		t.Fatalf("err = %v (%T), want *review.StackedError", err, err)
	}
	if stacked.Detected != "origin/main" {
		t.Errorf("detected = %s, want origin/main", stacked.Detected)
	}

	// Nearest first: the branch it was actually cut from comes before the one
	// under that.
	var names []string
	for _, c := range stacked.Candidates {
		names = append(names, c.Branch)
	}
	if len(names) != 2 || names[0] != "stack-middle" || names[1] != "stack-bottom" {
		t.Fatalf("candidates = %v, want stack-middle then stack-bottom", names)
	}
	if stacked.Candidates[0].Ahead != 1 || stacked.Candidates[1].Ahead != 2 {
		t.Errorf("ahead = %d and %d, want 1 and 2", stacked.Candidates[0].Ahead, stacked.Candidates[1].Ahead)
	}
	if !strings.Contains(err.Error(), "--base") {
		t.Errorf("err = %v, want it to name the flag", err)
	}

	// Naming one of them settles it, which is the whole point of listing them.
	if got := f.mustOpen("stack-middle").Base().Ref; got != "stack-middle" {
		t.Errorf("base = %s, want stack-middle", got)
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

	t.Run("with no origin to fall back on", func(t *testing.T) {
		f := newFixture(t)
		f.commit("first")

		_, err := f.open("")
		if err == nil {
			t.Fatal("a repository with no origin/HEAD should not guess a base")
		}
		if !strings.Contains(err.Error(), "--base") {
			t.Errorf("err = %v, want it to name the flag", err)
		}
	})

	t.Run("with a base that does not resolve", func(t *testing.T) {
		f := branched(t)

		_, err := f.open("no-such-ref")
		if err == nil {
			t.Fatal("a base that does not resolve should fail")
		}
		if !strings.Contains(err.Error(), "no-such-ref") {
			t.Errorf("err = %v, want it to name the ref", err)
		}
	})

	// origin/HEAD is symbolic and outlives what it points at, so renaming the
	// remote's default branch leaves it naming a ref that is gone. The reader
	// gets the flag, not a merge-base fatal about an object name.
	t.Run("with an origin/HEAD pointing at nothing", func(t *testing.T) {
		f := branched(t)
		f.Git("update-ref", "-d", "refs/remotes/origin/main")

		_, err := f.open("")
		if err == nil {
			t.Fatal("a dangling origin/HEAD should fail")
		}
		if !strings.Contains(err.Error(), "--base") {
			t.Errorf("err = %v, want it to name the flag", err)
		}
		if strings.Contains(err.Error(), "fatal:") {
			t.Errorf("err = %v, want guidance rather than raw git plumbing", err)
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

// A base force-push that loses the fork point has no clean answer. Saying so and
// asking for another beats measuring from a merge base that does not exist.
func TestABaseSharingNoHistorySaysTheForkPointIsGone(t *testing.T) {
	f := branched(t)

	f.Git("checkout", "-q", "--orphan", "unrelated")
	f.commit("nothing in common")
	f.Git("checkout", "-q", "feature")

	_, err := f.open("unrelated")
	if err == nil {
		t.Fatal("a base sharing no history should fail")
	}

	var gone *review.NoMergeBaseError
	if !errors.As(err, &gone) {
		t.Fatalf("err = %v (%T), want *review.NoMergeBaseError", err, err)
	}
	if gone.Ref != "unrelated" {
		t.Errorf("ref = %s, want unrelated", gone.Ref)
	}
}

// The stored base going away is said out loud. Falling back to detection would
// silently change what the review is measuring, and everything already reviewed
// was measured from the ref that just disappeared.
func TestAStoredBaseThatStopsResolvingIsNotReplacedQuietly(t *testing.T) {
	f := branched(t)
	f.Git("branch", "chosen", "main")

	first := f.mustOpen("chosen")
	if first.Base().Ref != "chosen" {
		t.Fatalf("base = %s, want chosen", first.Base().Ref)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	f.Git("branch", "-D", "chosen")

	_, err := f.open("")
	if err == nil {
		t.Fatal("a stored base that no longer resolves should fail")
	}

	var missing *review.UnresolvableBaseError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v (%T), want *review.UnresolvableBaseError", err, err)
	}
	if missing.Ref != "chosen" {
		t.Errorf("ref = %s, want chosen", missing.Ref)
	}

	// And the flag is the way out, which is what the message says to do.
	if got := f.mustOpen("origin/main").Base().Ref; got != "origin/main" {
		t.Errorf("base = %s, want origin/main", got)
	}
}
