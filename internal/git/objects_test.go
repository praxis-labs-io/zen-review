package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// sig is the identity every test commits with, fixed so a commit is reproducible.
var sig = Signature{
	Name:  "zen-review",
	Email: "zen-review@localhost",
	When:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
}

// entries reads a tree as "mode sha" keyed by path, which is the level a
// snapshot has to be checked at: the mode carries the exec bit and the symlink,
// and the sha carries the content.
func entries(t *testing.T, f *fixture, tree string) map[string]string {
	t.Helper()

	out := f.Git("ls-tree", "-r", tree)
	got := map[string]string{}
	if out == "" {
		return got
	}
	for _, line := range strings.Split(out, "\n") {
		meta, path, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("ls-tree line has no tab: %q", line)
		}
		fields := strings.Fields(meta)
		got[path] = fields[0] + " " + fields[2]
	}
	return got
}

func snapshot(t *testing.T, f *fixture) (*Repo, Snapshot) {
	t.Helper()

	repo := f.open()
	snap, err := repo.SnapshotTree(t.Context())
	if err != nil {
		t.Fatalf("snapshotting: %v", err)
	}
	return repo, snap
}

// exists says whether an object is in the store. cat-file -e answers with its
// status and says nothing either way.
func exists(t *testing.T, f *fixture, sha string) bool {
	t.Helper()

	_, code, err := runStatus(t.Context(), f.Dir(), 1, "cat-file", "-e", sha)
	if err != nil {
		t.Fatalf("checking for %s: %v", sha, err)
	}
	return code == 0
}

// Every way a file can differ from HEAD has to arrive in the tree, because the
// snapshot is what a review is measured against and one it drops is a change
// nobody sees.
func TestASnapshotHoldsEveryChangeInTheWorkTree(t *testing.T) {
	f := newFixture(t)
	f.Write(".gitignore", "ignored.log\n")
	f.Write("kept.txt", "unchanged\n")
	f.Write("edited.txt", "before\n")
	f.Write("staged.txt", "before\n")
	f.Write("gone.txt", "doomed\n")
	f.Write("old-name.txt", "renamed content\n")
	f.Write("exec.sh", "#!/bin/sh\n")
	f.Commit("first")

	f.Write("edited.txt", "after\n")
	f.Write("staged.txt", "after\n")
	f.Git("add", "staged.txt")
	f.Git("rm", "-q", "gone.txt")
	f.Git("mv", "old-name.txt", "new-name.txt")
	f.Write("untracked.txt", "never added\n")
	f.Write("ignored.log", "noise\n")
	if err := os.Chmod(filepath.Join(f.Dir(), "exec.sh"), 0o755); err != nil {
		t.Fatalf("setting the exec bit: %v", err)
	}
	if err := os.Symlink("kept.txt", filepath.Join(f.Dir(), "link.txt")); err != nil {
		t.Fatalf("creating the symlink: %v", err)
	}

	_, snap := snapshot(t, f)
	got := entries(t, f, snap.Tree)

	for _, path := range []string{"kept.txt", "edited.txt", "staged.txt", "new-name.txt", "untracked.txt"} {
		if _, ok := got[path]; !ok {
			t.Errorf("%s is missing from the snapshot: %v", path, got)
		}
	}
	for _, path := range []string{"gone.txt", "old-name.txt", "ignored.log"} {
		if _, ok := got[path]; ok {
			t.Errorf("%s should not be in the snapshot: %v", path, got)
		}
	}

	// The work tree's content, not HEAD's, is what the tree has to carry.
	want := f.Git("hash-object", "edited.txt")
	if !strings.HasSuffix(got["edited.txt"], want) {
		t.Errorf("edited.txt = %q, want the work tree blob %s", got["edited.txt"], want)
	}
	if !strings.HasPrefix(got["exec.sh"], "100755 ") {
		t.Errorf("exec.sh = %q, want mode 100755", got["exec.sh"])
	}

	// A symlink is stored as its target path, not as the bytes it points at.
	// Hashing the path instead would follow the link and record kept.txt.
	if !strings.HasPrefix(got["link.txt"], "120000 ") {
		t.Errorf("link.txt = %q, want mode 120000", got["link.txt"])
	}
	if target := f.Git("cat-file", "blob", strings.Fields(got["link.txt"])[1]); target != "kept.txt" {
		t.Errorf("the symlink blob = %q, want the target path", target)
	}
	if len(snap.Skipped) != 0 {
		t.Errorf("Skipped = %v, want nothing skipped", snap.Skipped)
	}
}

// The index is the one thing in a repository an agent is entitled to be holding,
// so a snapshot that disturbed it would break the tool it is watching.
func TestASnapshotLeavesTheRealIndexAlone(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Write("b.txt", "staged\n")
	f.Git("add", "b.txt")
	f.Write("c.txt", "untracked\n")

	index := filepath.Join(f.Dir(), ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("reading the index: %v", err)
	}
	status := f.Git("status", "--porcelain")

	snapshot(t, f)

	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("reading the index: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the snapshot rewrote the real index")
	}
	if got := f.Git("status", "--porcelain"); got != status {
		t.Errorf("status changed after the snapshot:\n got %q\nwant %q", got, status)
	}
	if _, err := os.Stat(index + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Error("the snapshot left a lock on the real index")
	}
}

// A merge in progress is exactly when a reviewer wants to look, and a conflicted
// index is the state git refuses to write a tree from.
func TestASnapshotWorksWithAConflictedIndex(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "base\n")
	f.Commit("first")

	f.Git("checkout", "-q", "-b", "side")
	f.Write("a.txt", "theirs\n")
	f.Commit("side")

	f.Git("checkout", "-q", "main")
	f.Write("a.txt", "ours\n")
	f.Commit("ours")

	// The merge is meant to fail. What is under test is the state it leaves.
	merge := exec.Command("git", "merge", "side")
	merge.Dir = f.Dir()
	_ = merge.Run()
	if !strings.HasPrefix(f.Git("status", "--porcelain", "a.txt"), "UU") {
		t.Fatal("a.txt is not conflicted, so this test proves nothing")
	}

	_, snap := snapshot(t, f)

	if _, ok := entries(t, f, snap.Tree)["a.txt"]; !ok {
		t.Error("the conflicted file is missing from the snapshot")
	}
	if !strings.HasPrefix(f.Git("status", "--porcelain", "a.txt"), "UU") {
		t.Error("the snapshot resolved the conflict in the real index")
	}
}

// A repository whose first commit has not landed still has a changeset, and
// read-tree has nothing to read.
func TestASnapshotOfARepositoryWithNoCommits(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "first ever\n")

	_, snap := snapshot(t, f)

	if _, ok := entries(t, f, snap.Tree)["a.txt"]; !ok {
		t.Error("the file is missing from the snapshot of an unborn HEAD")
	}
}

// A file git cannot read is named rather than dropped, and the rest of the
// snapshot still gets written.
func TestASnapshotNamesWhatItCouldNotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file with no permissions, so there is nothing to skip")
	}

	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Write("locked.txt", "secret\n")

	locked := filepath.Join(f.Dir(), "locked.txt")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("removing the permissions: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	_, snap := snapshot(t, f)

	if len(snap.Skipped) != 1 || snap.Skipped[0] != "locked.txt" {
		t.Errorf("Skipped = %v, want [locked.txt]", snap.Skipped)
	}
	if _, ok := entries(t, f, snap.Tree)["a.txt"]; !ok {
		t.Error("the readable file is missing, so one bad file lost the whole snapshot")
	}
}

// A directory git cannot open takes every file under it out of the tree, and it
// says so in a warning while exiting 0. Nothing else marks their absence.
func TestASnapshotNamesADirectoryItCouldNotOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a directory with no permissions, so there is nothing to skip")
	}

	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Write("locked/inside.txt", "unreachable\n")

	locked := filepath.Join(f.Dir(), "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("removing the permissions: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, snap := snapshot(t, f)

	if len(snap.Skipped) != 1 || !strings.HasPrefix(snap.Skipped[0], "locked") {
		t.Errorf("Skipped = %v, want the locked directory", snap.Skipped)
	}
	if _, ok := entries(t, f, snap.Tree)["locked/inside.txt"]; ok {
		t.Error("the unreachable file is in the tree, so this test proves nothing")
	}
}

// Repo promises a value is safe to call from more than one goroutine, and the
// TUI will refresh while a build is already running. Two builds sharing an index
// clear each other halfway through, and the bad outcome is a wrong tree rather
// than an error.
func TestConcurrentSnapshotsDoNotCollide(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Write("b.txt", "untracked\n")

	repo := f.open()
	const runs = 8
	trees := make([]string, runs)
	errs := make([]error, runs)

	var wg sync.WaitGroup
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap, err := repo.SnapshotTree(t.Context())
			trees[i], errs[i] = snap.Tree, err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("snapshot %d failed: %v", i, err)
		}
		if trees[i] != trees[0] {
			t.Errorf("snapshot %d built tree %s, snapshot 0 built %s", i, trees[i], trees[0])
		}
	}
}

// An index left by a killed process is the size of the work tree and nothing
// else ever comes back for it. One a live build is still writing has to survive.
func TestASnapshotSweepsAbandonedIndexes(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")

	repo := f.open()
	dir := filepath.Join(repo.CommonDir(), "zen-review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("preparing the directory: %v", err)
	}

	abandoned := filepath.Join(dir, "index-999.tmp")
	fresh := filepath.Join(dir, "index-111.tmp")
	for _, path := range []string{abandoned, abandoned + ".lock", fresh} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("planting %s: %v", path, err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, path := range []string{abandoned, abandoned + ".lock"} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("ageing %s: %v", path, err)
		}
	}

	if _, err := repo.SnapshotTree(t.Context()); err != nil {
		t.Fatalf("snapshotting: %v", err)
	}

	for _, path := range []string{abandoned, abandoned + ".lock"} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived the sweep", filepath.Base(path))
		}
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the sweep took an index a live build could still be writing: %v", err)
	}
}

// commit-tree with no identity configured invents one from the hostname, so the
// pinned signature is what keeps a generation attributable.
func TestACommitTakesTheSignatureItIsGiven(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	f.Commit("first")
	f.Git("config", "--unset", "user.name")
	f.Git("config", "--unset", "user.email")

	repo, snap := snapshot(t, f)

	commit, err := repo.CommitTree(t.Context(), snap.Tree, nil, "generation 1", sig)
	if err != nil {
		t.Fatalf("committing the tree: %v", err)
	}

	body := f.Git("cat-file", "commit", commit)
	if !strings.Contains(body, "author zen-review <zen-review@localhost>") {
		t.Errorf("the commit is not attributed to the signature:\n%s", body)
	}
	if strings.Contains(body, "parent ") {
		t.Errorf("a commit with no parents should be a root commit:\n%s", body)
	}
	if got, err := repo.Tree(t.Context(), commit); err != nil || got != snap.Tree {
		t.Errorf("Tree(%s) = %q, %v, want %s", commit, got, err, snap.Tree)
	}
}

func TestACommitChainsOntoItsParents(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	base := f.Commit("first")

	repo, snap := snapshot(t, f)

	commit, err := repo.CommitTree(t.Context(), snap.Tree, []string{base}, "generation 1", sig)
	if err != nil {
		t.Fatalf("committing the tree: %v", err)
	}
	if got := f.Git("rev-parse", commit+"^"); got != base {
		t.Errorf("the parent = %s, want the base %s", got, base)
	}
}

// Two instances on one repository share a ref, and the one that lost the race
// has to hear about it rather than write over the other's generation.
func TestUpdateRefSwapsOnlyFromWhereItWasTold(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	first := f.Commit("first")
	f.Write("a.txt", "two\n")
	second := f.Commit("second")

	repo := f.open()
	const ref = "refs/zen-review/sessions/abc"

	if err := repo.UpdateRef(t.Context(), ref, first, ""); err != nil {
		t.Fatalf("creating the ref: %v", err)
	}
	if got := f.Git("rev-parse", ref); got != first {
		t.Errorf("%s = %s, want %s", ref, got, first)
	}

	t.Run("creating a ref that already exists", func(t *testing.T) {
		if err := repo.UpdateRef(t.Context(), ref, second, ""); !errors.Is(err, ErrRefMoved) {
			t.Errorf("err = %v, want ErrRefMoved", err)
		}
	})

	t.Run("moving from the wrong place", func(t *testing.T) {
		if err := repo.UpdateRef(t.Context(), ref, second, second); !errors.Is(err, ErrRefMoved) {
			t.Errorf("err = %v, want ErrRefMoved", err)
		}
		if got := f.Git("rev-parse", ref); got != first {
			t.Errorf("a lost race moved the ref to %s", got)
		}
	})

	t.Run("moving from where it is", func(t *testing.T) {
		if err := repo.UpdateRef(t.Context(), ref, second, first); err != nil {
			t.Fatalf("moving the ref: %v", err)
		}
		if got := f.Git("rev-parse", ref); got != second {
			t.Errorf("%s = %s, want %s", ref, got, second)
		}
	})

	// git wraps both a lost race and a lock left by a crashed process in the same
	// "cannot lock ref". Only the first clears on its own, so a caller told to
	// reload and retry on the second retries forever.
	t.Run("blocked by a lock nobody will clear", func(t *testing.T) {
		lock := filepath.Join(repo.CommonDir(), ref+".lock")
		if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
			t.Fatalf("preparing the lock directory: %v", err)
		}
		if err := os.WriteFile(lock, nil, 0o644); err != nil {
			t.Fatalf("planting the stale lock: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(lock) })

		err := repo.UpdateRef(t.Context(), ref, first, second)
		if err == nil {
			t.Fatal("a stale lock should stop the write")
		}
		if errors.Is(err, ErrRefMoved) {
			t.Errorf("err = %v, want a plain failure rather than ErrRefMoved", err)
		}
	})
}

// The whole promise of writing a generation into git: the bytes a comment was
// written against survive a collection that takes everything unreferenced, and
// the base stays readable after the branch it came from is destroyed.
func TestAGenerationSurvivesGarbageCollection(t *testing.T) {
	f := newFixture(t)
	f.Write("tracked.txt", "before\n")
	base := f.Commit("first")

	f.Write("tracked.txt", "after\n")
	f.Write("untracked.txt", "never added\n")
	edited := f.Git("hash-object", "tracked.txt")
	added := f.Git("hash-object", "untracked.txt")

	repo, snap := snapshot(t, f)
	commit, err := repo.CommitTree(t.Context(), snap.Tree, []string{base}, "generation 1", sig)
	if err != nil {
		t.Fatalf("committing the tree: %v", err)
	}
	if err := repo.UpdateRef(t.Context(), "refs/zen-review/sessions/abc", commit, ""); err != nil {
		t.Fatalf("writing the session ref: %v", err)
	}

	// A blob nothing points at, so a collection that keeps everything proves as
	// little as one that keeps nothing.
	f.Write("control.txt", "unreferenced\n")
	control := f.Git("hash-object", "-w", "control.txt")

	// Now take away every other way to reach the generation's contents: the
	// branch, the index, the files and the reflog.
	f.Git("checkout", "--orphan", "wiped")
	f.Git("rm", "-q", "-rf", "--cached", ".")
	for _, name := range []string{"tracked.txt", "untracked.txt", "control.txt"} {
		if err := os.Remove(filepath.Join(f.Dir(), name)); err != nil {
			t.Fatalf("removing %s: %v", name, err)
		}
	}
	f.Git("branch", "-D", "main")
	f.Git("reflog", "expire", "--expire=now", "--expire-unreachable=now", "--all")
	f.Git("gc", "--prune=now")

	for _, sha := range []string{commit, snap.Tree, edited, added, base} {
		if !exists(t, f, sha) {
			t.Errorf("%s did not survive the collection", sha)
		}
	}
	if exists(t, f, control) {
		t.Error("the unreferenced control blob survived, so the collection proves nothing")
	}
	if out := f.Git("fsck"); strings.Contains(out, "error") {
		t.Errorf("fsck complains after the collection:\n%s", out)
	}
}
