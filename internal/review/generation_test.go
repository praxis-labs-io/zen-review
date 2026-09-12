package review_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/git"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testrepo"
)

func (f *fixture) refresh(s *review.Session) review.Generation {
	f.t.Helper()

	g, err := s.Refresh(f.t.Context())
	if err != nil {
		f.t.Fatalf("refreshing: %v", err)
	}
	return g
}

func (f *fixture) status(s *review.Session) review.Status {
	f.t.Helper()

	st, err := s.Status(f.t.Context())
	if err != nil {
		f.t.Fatalf("reading the status: %v", err)
	}
	return st
}

func (f *fixture) db() *store.DB {
	f.t.Helper()

	db, err := store.Open(f.t.Context(), filepath.Join(f.Dir(), ".git", "zen-review", "state.db"))
	if err != nil {
		f.t.Fatalf("opening the database: %v", err)
	}
	f.t.Cleanup(func() { _ = db.Close() })
	return db
}

func (f *fixture) latest(id string) (store.Generation, bool) {
	f.t.Helper()

	g, found, err := f.db().LatestGeneration(f.t.Context(), id)
	if err != nil {
		f.t.Fatalf("reading the latest generation: %v", err)
	}
	return g, found
}

func (f *fixture) genFiles(g review.Generation) map[string]store.GenFile {
	f.t.Helper()

	rows, err := f.db().GenFiles(f.t.Context(), g.ID)
	if err != nil {
		f.t.Fatalf("reading the files of generation %d: %v", g.Seq, err)
	}

	byPath := make(map[string]store.GenFile, len(rows))
	for _, row := range rows {
		byPath[row.Path] = row
	}
	return byPath
}

func (f *fixture) parents(commit string) []string {
	f.t.Helper()

	fields := strings.Fields(f.Git("rev-list", "--parents", "-n", "1", commit))
	return fields[1:]
}

func (f *fixture) sessionRefs() []string {
	f.t.Helper()

	out := f.Git("for-each-ref", "--format=%(refname)", "refs/zen-review/")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func (f *fixture) looseObjects() int {
	f.t.Helper()

	for _, line := range strings.Split(f.Git("count-objects", "-v"), "\n") {
		rest, ok := strings.CutPrefix(line, "count: ")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			f.t.Fatalf("reading the object count from %q: %v", line, err)
		}
		return n
	}
	f.t.Fatal("count-objects said nothing about loose objects")
	return 0
}

const signatureEmail = "zen-review@invalid"

func (f *fixture) generations(ref string) int {
	f.t.Helper()
	return strings.Count(f.Git("log", "--first-parent", "--format=%ae", ref), signatureEmail)
}

func edited(t *testing.T) *fixture {
	t.Helper()

	f := branched(t)
	f.Write("a.txt", "edited in the work tree\n")
	return f
}

func TestTheFirstRefreshWritesGenerationOne(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")

	g := f.refresh(s)

	if g.Seq != 1 {
		t.Errorf("seq = %d, want 1", g.Seq)
	}
	if got := f.Git("rev-parse", s.Ref()); got != g.CommitSha {
		t.Errorf("the ref is at %s, want the generation commit %s", got, g.CommitSha)
	}

	if got := f.parents(g.CommitSha); len(got) != 1 || got[0] != s.Base().SHA {
		t.Errorf("parents = %v, want just the base %s", got, s.Base().SHA)
	}

	files, err := s.Files(t.Context(), g)
	if err != nil {
		t.Fatalf("reading the files: %v", err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" {
		t.Errorf("files = %v, want just a.txt", files)
	}
}

func TestASecondRefreshWithNothingTouchedWritesNothing(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")

	first := f.refresh(s)
	second := f.refresh(s)

	if second.Seq != 1 || second.CommitSha != first.CommitSha {
		t.Errorf("second refresh = seq %d at %s, want the first back: seq 1 at %s",
			second.Seq, second.CommitSha, first.CommitSha)
	}
	if got := f.Git("rev-parse", s.Ref()); got != first.CommitSha {
		t.Errorf("the ref moved to %s, want it left at %s", got, first.CommitSha)
	}
}

func TestAnEditBuildsTheNextGeneration(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")

	first := f.refresh(s)
	f.Write("a.txt", "edited again\n")
	second := f.refresh(s)

	if second.Seq != 2 {
		t.Errorf("seq = %d, want 2", second.Seq)
	}
	if got := f.parents(second.CommitSha); len(got) != 1 || got[0] != first.CommitSha {
		t.Errorf("parents = %v, want just generation 1 at %s", got, first.CommitSha)
	}
	if got := f.Git("rev-parse", s.Ref()); got != second.CommitSha {
		t.Errorf("the ref is at %s, want the new generation %s", got, second.CommitSha)
	}
}

func TestCommittingTheWorkTreeBuildsNothing(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")

	first := f.refresh(s)
	f.Commit("the agent committed what it had written")
	second := f.refresh(s)

	if second.Seq != 1 || second.CommitSha != first.CommitSha {
		t.Errorf("second refresh = seq %d at %s, want the first back", second.Seq, second.CommitSha)
	}
}

func TestChangingTheBaseBuildsAGenerationHangingOffBoth(t *testing.T) {
	f := newFixture(t)
	first := f.commit("first")
	f.Git("branch", "older", first)
	f.commit("second")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	f.commit("third")
	f.Write("a.txt", "edited in the work tree\n")

	detected := f.refresh(f.mustOpen(""))

	moved := f.mustOpen("older")
	if moved.Base().SHA != first {
		t.Fatalf("base = %s, want the older branch at %s", moved.Base().SHA, first)
	}

	g := f.refresh(moved)
	if g.Seq != 2 {
		t.Errorf("seq = %d, want 2: the base moved even though the tree did not", g.Seq)
	}
	want := []string{detected.CommitSha, first}
	if got := f.parents(g.CommitSha); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("parents = %v, want %v", got, want)
	}
}

func TestARebaseMovesTheBase(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("feature.txt", "work\n")
	f.Commit("feature work")

	s := f.mustOpen("")
	before := f.refresh(s)

	f.Git("checkout", "-q", "main")
	f.Write("upstream.txt", "moved on\n")
	f.Commit("upstream")
	f.TrackOrigin("main")
	forkPoint := f.Git("rev-parse", "main")
	f.Git("checkout", "-q", "feature")
	f.Git("rebase", "-q", "main")

	after := f.refresh(s)

	if after.BaseSha != forkPoint {
		t.Errorf("base = %s, want the new fork point %s", after.BaseSha, forkPoint)
	}
	if after.Seq != 2 || after.CommitSha == before.CommitSha {
		t.Errorf("seq = %d at %s, want a new generation after the rebase", after.Seq, after.CommitSha)
	}
	if s.Base().SHA != forkPoint {
		t.Errorf("the session still reports %s as its base, want %s", s.Base().SHA, forkPoint)
	}
}

func TestAnUntrackedFileGetsABlobThatResolves(t *testing.T) {
	f := branched(t)
	f.Write("new.txt", "untracked content\n")

	s := f.mustOpen("")
	g := f.refresh(s)

	row, ok := f.genFiles(g)["new.txt"]
	if !ok {
		t.Fatalf("new.txt is not in the generation: %v", f.genFiles(g))
	}
	if row.Status != diff.FileAdded {
		t.Errorf("status = %q, want added", row.Status)
	}
	if row.HeadBlob == "" {
		t.Fatal("new.txt has no head blob")
	}
	if got := f.Git("cat-file", "blob", row.HeadBlob); got != "untracked content" {
		t.Errorf("the blob reads %q, want the file's content", got)
	}
}

func TestADeletedFileKeepsItsBaseBlob(t *testing.T) {
	f := branched(t)
	if err := os.Remove(filepath.Join(f.Dir(), "a.txt")); err != nil {
		t.Fatalf("removing the file: %v", err)
	}

	g := f.refresh(f.mustOpen(""))

	row, ok := f.genFiles(g)["a.txt"]
	if !ok {
		t.Fatalf("a.txt is not in the generation: %v", f.genFiles(g))
	}
	if row.Status != diff.FileDeleted {
		t.Errorf("status = %q, want deleted", row.Status)
	}
	if row.BaseBlob == "" || row.HeadBlob != "" {
		t.Errorf("blobs = base %q head %q, want a base and no head", row.BaseBlob, row.HeadBlob)
	}
}

func TestAnEmbeddedRepositoryIsOneOrdinaryRow(t *testing.T) {
	inner := testrepo.New(t)
	inner.Write("f.txt", "inner\n")
	want := inner.Commit("inner")

	f := branched(t)
	if err := os.Rename(inner.Dir(), filepath.Join(f.Dir(), "sub")); err != nil {
		t.Fatalf("moving a repository inside the work tree: %v", err)
	}

	g := f.refresh(f.mustOpen(""))

	row, ok := f.genFiles(g)["sub"]
	if !ok {
		t.Fatalf("the embedded repository is not in the generation: %v", f.genFiles(g))
	}
	if row.Status != diff.FileAdded {
		t.Errorf("status = %q, want added", row.Status)
	}
	if row.HeadBlob != want {
		t.Errorf("head sha = %q, want the inner repository's commit %q", row.HeadBlob, want)
	}
}

func TestAFileMovedInTheWorkTreeIsOneRename(t *testing.T) {
	f := newFixture(t)
	body := strings.Repeat("a line of content\n", 20)
	f.Write("old.txt", body)
	f.Commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")

	if err := os.Rename(filepath.Join(f.Dir(), "old.txt"), filepath.Join(f.Dir(), "new.txt")); err != nil {
		t.Fatalf("moving the file: %v", err)
	}

	g := f.refresh(f.mustOpen(""))

	rows := f.genFiles(g)
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one rename rather than a delete and an add", rows)
	}
	row := rows["new.txt"]
	if row.Status != diff.FileRenamed || row.OldPath != "old.txt" {
		t.Errorf("row = %+v, want new.txt renamed from old.txt", row)
	}
}

func TestUntrackedFilesPastTheCeilingRefuseBeforeAnythingIsHashed(t *testing.T) {
	f := branched(t)
	for i := 0; i <= 5000; i++ {
		f.Write(fmt.Sprintf("generated/pack/%d.txt", i), fmt.Sprintf("%d\n", i))
	}

	s := f.mustOpen("")
	before := f.looseObjects()
	_, err := s.Refresh(t.Context())

	var tooLarge *review.TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want a TooLargeError", err)
	}
	if tooLarge.Dir != "generated" {
		t.Errorf("Dir = %q, want the directory worth ignoring rather than a leaf under it", tooLarge.Dir)
	}
	if !strings.Contains(err.Error(), "generated") {
		t.Errorf("err = %v, want it to name the directory", err)
	}

	if after := f.looseObjects(); after != before {
		t.Errorf("loose objects went from %d to %d, so the refusal paid for the whole directory", before, after)
	}
	if refs := f.sessionRefs(); refs != nil {
		t.Errorf("refs = %v, want none written", refs)
	}
	if _, found := f.latest(s.ID()); found {
		t.Error("a refused changeset wrote a generation row")
	}
}

func TestATrackedChangesetPastTheCeilingRefuses(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.TrackOrigin("main")

	f.Git("checkout", "-q", "-b", "feature")
	for i := 0; i <= 5000; i++ {
		f.Write(fmt.Sprintf("generated/pack/%d.txt", i), "x\n")
	}
	f.Commit("committed the world")

	s := f.mustOpen("")
	_, err := s.Refresh(t.Context())

	var tooLarge *review.TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want a TooLargeError", err)
	}
	if tooLarge.Dir != "generated" {
		t.Errorf("Dir = %q, want the directory the changeset is mostly made of", tooLarge.Dir)
	}
	if refs := f.sessionRefs(); refs != nil {
		t.Errorf("refs = %v, want none written", refs)
	}
	if _, found := f.latest(s.ID()); found {
		t.Error("a refused changeset wrote a generation row")
	}
}

func TestAGenerationTheDatabaseDoesNotKnowStillPinsTheBase(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")

	orphan := f.Git("rev-parse", "HEAD")
	f.Git("update-ref", s.Ref(), orphan)

	g := f.refresh(s)

	want := []string{orphan, s.Base().SHA}
	if got := f.parents(g.CommitSha); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("parents = %v, want the orphan and the base %v", got, want)
	}
}

// The two race on different bases, or both build one commit and a loser that wrote a row hides it.
func TestConcurrentRefreshesNeverClaimAGenerationTheRefLost(t *testing.T) {
	f := newFixture(t)
	first := f.commit("first")
	f.Git("branch", "older", first)
	f.commit("second")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	f.commit("third")
	f.Write("a.txt", "edited in the work tree\n")

	racers := []*review.Session{f.mustOpen(""), f.mustOpen("older")}
	errs := make([]error, len(racers))

	var wg sync.WaitGroup
	for i, s := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Refresh(t.Context())
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil && !errors.Is(err, git.ErrRefMoved) {
			t.Fatalf("instance %d failed with %v, want either a generation or ErrRefMoved", i, err)
		}
	}

	latest, found := f.latest(racers[0].ID())
	if !found {
		t.Fatal("no generation was written at all")
	}
	if ref := f.Git("rev-parse", racers[0].Ref()); ref != latest.CommitSha {
		t.Errorf("the ref is at %s and the newest row says %s", ref, latest.CommitSha)
	}

	if built := f.generations(racers[0].Ref()); latest.Seq != built {
		t.Errorf("the database holds %d generations and the ref chain holds %d", latest.Seq, built)
	}
}

func TestARefusedGenerationPutsTheRefBack(t *testing.T) {
	f := edited(t)
	slow, quick := f.mustOpen(""), f.mustOpen("")

	slow.DuringRefresh(func() {
		slow.DuringRefresh(nil)
		if _, err := quick.Refresh(t.Context()); err != nil {
			t.Fatalf("the quick refresh failed: %v", err)
		}
	})

	if _, err := slow.Refresh(t.Context()); !errors.Is(err, git.ErrRefMoved) {
		t.Fatalf("the slow refresh returned %v, want ErrRefMoved", err)
	}

	latest, found := f.latest(slow.ID())
	if !found {
		t.Fatal("the quick refresh wrote no generation")
	}
	if ref := f.Git("rev-parse", slow.Ref()); ref != latest.CommitSha {
		t.Errorf("the ref is at %s and the only row says %s", ref, latest.CommitSha)
	}
	if built := f.generations(slow.Ref()); built != latest.Seq {
		t.Errorf("the ref chain holds %d generations and the database holds %d", built, latest.Seq)
	}
}

func TestACancelledRefreshStillPutsTheRefBack(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")

	ctx, cancel := context.WithCancel(t.Context())
	s.AfterSwap(cancel)

	if _, err := s.Refresh(ctx); err == nil {
		t.Fatal("the cancelled refresh reported a generation")
	}

	if refs := f.sessionRefs(); refs != nil {
		t.Errorf("refs = %v, want the swap taken back", refs)
	}
	if _, found := f.latest(s.ID()); found {
		t.Error("a cancelled refresh wrote a generation row")
	}
}

func TestStatusOnASessionWithNoGenerationWritesNothing(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")

	st := f.status(s)

	if st.Exists {
		t.Error("Exists = true on a session that has never refreshed")
	}
	if !st.Stale {
		t.Error("Stale = false, but nothing has been reviewed against yet")
	}
	if refs := f.sessionRefs(); refs != nil {
		t.Errorf("refs = %v, want status to have written none", refs)
	}
	if _, found := f.latest(s.ID()); found {
		t.Error("status wrote a generation row")
	}
}

func TestStatusFollowsTheWorkTree(t *testing.T) {
	f := edited(t)
	s := f.mustOpen("")
	g := f.refresh(s)

	settled := f.status(s)
	if !settled.Exists || settled.Stale {
		t.Errorf("status = exists %v stale %v, want a generation that still describes the tree", settled.Exists, settled.Stale)
	}
	if settled.Generation.CommitSha != g.CommitSha {
		t.Errorf("status is on %s, want the generation just built %s", settled.Generation.CommitSha, g.CommitSha)
	}
	if len(settled.Files) != 1 || settled.Files[0].Path != "a.txt" {
		t.Errorf("files = %v, want just a.txt", settled.Files)
	}

	f.Write("a.txt", "edited again\n")

	moved := f.status(s)
	if !moved.Stale {
		t.Error("Stale = false after the work tree moved")
	}
	if moved.Generation.CommitSha != g.CommitSha {
		t.Errorf("status is on %s, want the generation unchanged: it reports, it does not build", moved.Generation.CommitSha)
	}
}

func TestALinkedWorktreeRefreshesIntoTheSharedRepository(t *testing.T) {
	f := branched(t)

	tree := filepath.Join(filepath.Dir(f.Dir()), "linked")
	f.Git("worktree", "add", "-q", "-b", "in-the-worktree", tree, "main")
	if err := os.WriteFile(filepath.Join(tree, "b.txt"), []byte("in the worktree\n"), 0o644); err != nil {
		t.Fatalf("writing in the worktree: %v", err)
	}

	s, err := review.Open(t.Context(), tree, review.Options{})
	if err != nil {
		t.Fatalf("opening a session in a linked worktree: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	g := f.refresh(s)

	if got := f.Git("rev-parse", s.Ref()); got != g.CommitSha {
		t.Errorf("the parent reads the ref as %s, want %s", got, g.CommitSha)
	}
	if _, found := f.latest(s.ID()); !found {
		t.Error("the generation is not in the parent's database")
	}
}

func TestARefreshNamesWhatItCouldNotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file with no permissions, so there is nothing to skip")
	}

	f := edited(t)
	f.Write("locked.txt", "unreadable\n")
	if err := os.Chmod(filepath.Join(f.Dir(), "locked.txt"), 0o000); err != nil {
		t.Fatalf("removing the permissions: %v", err)
	}

	g := f.refresh(f.mustOpen(""))

	if len(g.Skipped) != 1 || g.Skipped[0] != "locked.txt" {
		t.Errorf("Skipped = %v, want [locked.txt]", g.Skipped)
	}
}
