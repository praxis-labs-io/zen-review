package review_test

import (
	"os"
	"path/filepath"
	"slices"
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
	if base.Fallback != "stacked" {
		t.Errorf("fallback = %q, want it tagged stacked", base.Fallback)
	}

	// The flag still settles it, and what it settles on is not a fallback.
	chosen := f.mustOpen("stack-bottom").Base()
	if chosen.Ref != "stack-bottom" || chosen.Fallback != "" {
		t.Errorf("base = %+v, want stack-bottom with nothing to explain", chosen)
	}
}

func TestCandidatesGroupEveryFirstParentBranchNearestFirst(t *testing.T) {
	f := newFixture(t)
	main := f.commit("main")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "parent")
	parent := f.commit("parent")
	f.Git("update-ref", "refs/remotes/upstream/parent", parent)
	f.Git("checkout", "-q", "-b", "child")
	f.commit("child")
	f.Git("branch", "merged", main)

	s := f.mustOpen("")
	got, err := s.Candidates(t.Context())
	if err != nil {
		t.Fatalf("listing candidates: %v", err)
	}

	wantLocal := []review.Candidate{
		{Branch: "child", SHA: f.Git("rev-parse", "child"), Ahead: 0},
		{Branch: "parent", SHA: parent, Ahead: 1},
		{Branch: "main", SHA: main, Ahead: 2},
		{Branch: "merged", SHA: main, Ahead: 2},
	}
	wantRemote := []review.Candidate{
		{Branch: "upstream/parent", SHA: parent, Ahead: 1},
		{Branch: "origin/main", SHA: main, Ahead: 2},
	}
	if !slices.Equal(got.Local, wantLocal) {
		t.Errorf("local = %v, want %v", got.Local, wantLocal)
	}
	if !slices.Equal(got.Remote, wantRemote) {
		t.Errorf("remote = %v, want %v", got.Remote, wantRemote)
	}
}

func TestCandidatesKeepAnActiveBaseWhoseTipMovedAway(t *testing.T) {
	f := branched(t)
	s := f.mustOpen("")

	f.Git("checkout", "-q", "main")
	f.commit("new trunk work")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")

	got, err := s.Candidates(t.Context())
	if err != nil {
		t.Fatalf("listing candidates: %v", err)
	}
	if !slices.ContainsFunc(got.Remote, func(candidate review.Candidate) bool {
		return candidate.Branch == "origin/main"
	}) {
		t.Errorf("remote candidates = %v, want the active origin/main", got.Remote)
	}
}

func TestSetBasePersistsTheChoiceAndClearsTheFallback(t *testing.T) {
	f := branched(t)
	s := f.mustOpen("missing")
	if s.Base().Fallback == "" {
		t.Fatal("the setup did not fall back")
	}
	if err := s.SetSummary(t.Context(), "keep this"); err != nil {
		t.Fatalf("setting summary: %v", err)
	}

	if err := s.SetBase(t.Context(), "main"); err != nil {
		t.Fatalf("setting base: %v", err)
	}
	if got := s.Base(); got.Ref != "main" || got.Fallback != "" {
		t.Errorf("base = %+v, want main without a fallback", got)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("closing the first session: %v", err)
	}

	reopened := f.mustOpen("")
	if reopened.Base().Ref != "main" {
		t.Errorf("reopened base = %s, want main", reopened.Base().Ref)
	}
	if summary, err := reopened.Summary(t.Context()); err != nil || summary != "keep this" {
		t.Errorf("summary = %q, %v, want it preserved", summary, err)
	}
}

func TestSetBaseRejectsARefBeforeChangingTheSession(t *testing.T) {
	f := branched(t)
	s := f.mustOpen("")
	before := s.Base()

	if err := s.SetBase(t.Context(), "missing"); err == nil {
		t.Fatal("a missing ref was accepted")
	}
	if s.Base() != before {
		t.Errorf("base = %+v, want %+v", s.Base(), before)
	}
}

func TestSetBaseRetriesPersistenceAfterASaveFails(t *testing.T) {
	f := branched(t)
	s, err := f.open("")
	if err != nil {
		t.Fatalf("opening the session: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if err := s.SetBase(t.Context(), "main"); err == nil {
			t.Errorf("attempt %d succeeded after the database closed", attempt)
		}
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
		tag  string
	}{
		{
			name:  "a base that does not resolve",
			setup: func(*fixture) string { return "no-such-ref" },
			want:  "origin/main",
			tag:   "not no-such-ref",
		},
		{
			name: "a stored base that has since gone",
			setup: func(f *fixture) string {
				f.mustOpen("chosen")
				f.Git("branch", "-D", "chosen")
				return ""
			},
			want: "origin/main",
			tag:  "not chosen",
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
			tag:  "origin/main gone",
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
			tag:  "not unrelated",
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
			if base.Fallback != tc.tag {
				t.Errorf("fallback = %q, want %q", base.Fallback, tc.tag)
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
		if base.Fallback != "no remote" {
			t.Errorf("fallback = %q, want it tagged no remote", base.Fallback)
		}
	})

	t.Run("to HEAD on the default branch itself", func(t *testing.T) {
		f := newFixture(t)
		f.commit("first")

		base := f.mustOpen("").Base()

		if base.Ref != "HEAD" {
			t.Errorf("base = %s, want HEAD", base.Ref)
		}
		// The bottom rung reads as what the changeset is rather than as what the
		// repository lacks, which the rung above it already said.
		if base.Fallback != "uncommitted" {
			t.Errorf("fallback = %q, want it tagged uncommitted", base.Fallback)
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
	// No tag: "empty tree" is the whole of what there is to say.
	if s.Base().Fallback != "" {
		t.Errorf("fallback = %q, want the name to be the whole answer", s.Base().Fallback)
	}
	if s.Base().Name() != "empty tree" {
		t.Errorf("name = %q, want empty tree", s.Base().Name())
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
	if base.Fallback != "stacked" {
		t.Errorf("fallback = %q, want it tagged stacked", base.Fallback)
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

// A failed resolution must not cost the session the base it had. The stored ref
// is what everything already reviewed was measured from.
func TestAFallbackLeavesTheStoredBaseAlone(t *testing.T) {
	f := branched(t)
	f.Git("branch", "stable", "main")

	if got := f.mustOpen("stable").Base().Ref; got != "stable" {
		t.Fatalf("base = %s, want stable", got)
	}

	// A typo is one invocation, not a decision about the session.
	typo := f.mustOpen("stabel").Base()
	if typo.Ref != "origin/main" || typo.Fallback != "not stabel" {
		t.Errorf("base = %+v, want origin/main tagged not stabel", typo)
	}

	if got := f.mustOpen("").Base(); got.Ref != "stable" || got.Fallback != "" {
		t.Errorf("base = %+v, want the stored stable back", got)
	}
}

// The tag stands on every run rather than on the first. status and refresh are
// two processes, and the second must not read as a base somebody chose.
func TestAFallbackTagSurvivesTheNextOpen(t *testing.T) {
	f := branched(t)
	f.Git("branch", "chosen", "main")
	f.mustOpen("chosen")
	f.Git("branch", "-D", "chosen")

	for i := range 2 {
		base := f.mustOpen("").Base()
		if base.Ref != "origin/main" || base.Fallback != "not chosen" {
			t.Errorf("open %d: base = %+v, want origin/main tagged not chosen", i+1, base)
		}
	}
}

// The stack picked the rung, so it owns the tag. A missing or dangling remote
// would have landed on the same branch, so naming one misstates the cause.
func TestAStackedBranchIsTaggedStackedWhateverTheRemoteDid(t *testing.T) {
	tests := []struct {
		name  string
		broke func(f *fixture)
	}{
		{"with no remote at all", func(f *fixture) {
			f.Git("update-ref", "-d", "refs/remotes/origin/HEAD")
			f.Git("update-ref", "-d", "refs/remotes/origin/main")
		}},
		{"with a dangling origin/HEAD", func(f *fixture) {
			f.Git("update-ref", "-d", "refs/remotes/origin/main")
		}},
		{"with a healthy remote", func(*fixture) {}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := branched(t)
			f.Git("checkout", "-q", "-b", "child")
			f.commit("on the child")
			tc.broke(f)

			base := f.mustOpen("").Base()

			if base.Ref != "feature" {
				t.Errorf("base = %s, want feature", base.Ref)
			}
			if base.Fallback != "stacked" {
				t.Errorf("fallback = %q, want stacked", base.Fallback)
			}
		})
	}
}

// A repository whose trunk is not called main has no rung above HEAD to bound
// the stack walk, and the branch's own commits are what the reader came for.
func TestAStackIsFoundWithNoRemoteAndNoDefaultBranch(t *testing.T) {
	f := newFixture(t)
	f.Git("checkout", "-q", "-b", "develop")
	f.commit("first")
	f.Git("checkout", "-q", "-b", "feature")
	f.commit("real work")

	base := f.mustOpen("").Base()

	if base.Ref != "develop" {
		t.Errorf("base = %s, want develop", base.Ref)
	}
	if base.Fallback != "stacked" {
		t.Errorf("fallback = %q, want stacked", base.Fallback)
	}
}

// On a default branch nothing under it is a stack. A tip left behind on this
// branch's own history is a branch nobody deleted, and measuring from it would
// hide every commit since.
func TestADefaultBranchDoesNotStackOnATipLeftBehindOnIt(t *testing.T) {
	f := newFixture(t)
	f.commit("first")
	f.Git("branch", "left-behind", "main")
	f.commit("second")

	base := f.mustOpen("").Base()

	if base.Ref != "HEAD" {
		t.Errorf("base = %s, want HEAD", base.Ref)
	}
	if base.Fallback != "uncommitted" {
		t.Errorf("fallback = %q, want uncommitted", base.Fallback)
	}
}
