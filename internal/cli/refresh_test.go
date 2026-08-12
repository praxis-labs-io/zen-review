package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The changeset is the merge base through the working tree, and every way a
// file can arrive in it has to show up: committed, staged, unstaged, and never
// added at all.
func TestTheChangesetListsEveryChangedFile(t *testing.T) {
	f := newFixture(t)
	f.Write("kept.txt", "one\n")
	f.Write("edited.txt", "before\n")
	f.Write("staged.txt", "before\n")
	f.Write("gone.txt", "doomed\n")
	base := f.Commit("first")
	f.TrackOrigin("HEAD")

	f.Git("checkout", "-q", "-b", "feature")
	f.Write("committed.txt", "from a commit\n")
	f.Commit("the agent committed this one")

	f.Write("edited.txt", "after\n")
	f.Write("staged.txt", "after\n")
	f.Git("add", "staged.txt")
	f.Git("rm", "-q", "gone.txt")
	f.Write("untracked.txt", "never added\n")

	out := f.mustRun()

	if !strings.Contains(out, "base        origin/main ("+base[:7]+")") {
		t.Errorf("the base line is wrong:\n%s", out)
	}
	for _, want := range []string{
		"M  edited.txt",
		"M  staged.txt",
		"A  committed.txt",
		"A  untracked.txt",
		"D  gone.txt",
		"5 files",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "kept.txt") {
		t.Errorf("an unchanged file is in the changeset:\n%s", out)
	}
}

// One diff against a snapshot tree sees the untracked file and the deleted one
// together, so it pairs them. The composition this replaced ran two diffs and
// could only report an unrelated delete beside an unrelated add.
func TestAnUntrackedFileReplacingATrackedOnePairsAsARename(t *testing.T) {
	f := newFixture(t)
	f.Write("old-name.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n")
	f.Commit("first")
	f.TrackOrigin("HEAD")
	f.Git("checkout", "-q", "-b", "feature")

	// Deleted from the index and replaced by a file git has never been told
	// about, which is what an agent renaming a file leaves behind.
	f.Git("rm", "-q", "old-name.txt")
	f.Write("new-name.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\nCHANGED\n")

	w, _ := f.decode()

	if len(w.Files) != 1 {
		t.Fatalf("files = %+v, want the delete and the add paired into one row", w.Files)
	}
	if w.Files[0].Status != "renamed" {
		t.Errorf("status = %q, want renamed", w.Files[0].Status)
	}
	if w.Files[0].OldPath != "old-name.txt" || w.Files[0].Path != "new-name.txt" {
		t.Errorf("row = %+v, want old-name.txt -> new-name.txt", w.Files[0])
	}
}

// The first refresh writes generation 1 and the ref that carries it.
func TestTheFirstRefreshBuildsGenerationOne(t *testing.T) {
	f := edited(t)

	w, _ := f.decode("refresh")

	if w.Generation == nil {
		t.Fatal("refresh reported no generation")
	}
	if w.Generation.Seq != 1 {
		t.Errorf("seq = %d, want 1", w.Generation.Seq)
	}
	if w.Stale {
		t.Error("a generation built from the tree just snapshotted is not stale")
	}
	if refs := f.sessionRefs(); len(refs) != 1 || refs[0] != w.Ref {
		t.Errorf("refs = %v, want just %s", refs, w.Ref)
	}
	if got := f.Git("rev-parse", w.Ref); got != w.Generation.Commit {
		t.Errorf("the ref is at %s, want the generation commit %s", got, w.Generation.Commit)
	}
}

// Refreshing with nothing touched builds nothing. Without that check every
// invocation would write a commit, and the ref is what proves it did not.
func TestARefreshWithNothingTouchedBuildsNothing(t *testing.T) {
	f := edited(t)

	first, _ := f.decode("refresh")
	was := f.Git("rev-parse", first.Ref)

	second, _ := f.decode("refresh")

	if second.Generation.Seq != first.Generation.Seq {
		t.Errorf("seq = %d, want it to stay at %d", second.Generation.Seq, first.Generation.Seq)
	}
	if got := f.Git("rev-parse", first.Ref); got != was {
		t.Errorf("the ref moved to %s from %s, so a second refresh wrote a commit", got, was)
	}
}

// An edit after a refresh builds the next generation.
func TestAnEditAfterARefreshBuildsTheNextGeneration(t *testing.T) {
	f := edited(t)
	f.mustRun("refresh")

	f.Write("b.txt", "and another\n")
	w, _ := f.decode("refresh")

	if w.Generation.Seq != 2 {
		t.Errorf("seq = %d, want 2", w.Generation.Seq)
	}
	if _, ok := w.files()["b.txt"]; !ok {
		t.Errorf("files = %+v, want the new file in it", w.Files)
	}
}

// Bare zen-review is the refresh command until the TUI takes the slot, and it
// has to be the same function rather than a copy of its body.
func TestBareZenReviewIsTheRefreshCommand(t *testing.T) {
	f := edited(t)

	bare := f.mustRun()
	refreshed := f.mustRun("refresh")

	if bare != refreshed {
		t.Errorf("bare and refresh disagree:\n%s\n---\n%s", bare, refreshed)
	}
	if refs := f.sessionRefs(); len(refs) != 1 {
		t.Errorf("refs = %v, want the bare invocation to have built a generation", refs)
	}
}

// An empty changeset is an empty state, not an error.
func TestNothingChangedIsNotAnError(t *testing.T) {
	f := branched(t)

	out := f.mustRun()

	if !strings.Contains(out, "no changes since origin/main") {
		t.Errorf("output = %q, want it to say the changeset is empty", out)
	}
}

// An empty list has to marshal as one. A caller iterating the result should not
// have to special-case the quiet answer.
func TestAnEmptyChangesetIsAnEmptyArray(t *testing.T) {
	f := branched(t)

	_, raw := f.decode("refresh")

	for _, want := range []string{`"files": []`, `"skipped": []`} {
		if !strings.Contains(raw, want) {
			t.Errorf("payload does not contain %s, so it marshalled null:\n%s", want, raw)
		}
	}
}

// A repository embedded in the work tree is two different things depending on
// whether it has a commit, and both have to be visible rather than dropped.
func TestAnEmbeddedRepositoryIsReportedRatherThanDropped(t *testing.T) {
	// Until it commits there is no gitlink to record, so add refuses it outright.
	// Unnamed it would take every command down with it.
	t.Run("with no commit yet, it is a skipped path", func(t *testing.T) {
		f := branched(t)
		f.Git("init", "-q", "-b", "main", "vendored")
		f.Write("plain.txt", "beside it\n")

		w, _ := f.decode()

		if len(w.Skipped) != 1 || w.Skipped[0] != "vendored/" {
			t.Errorf("skipped = %v, want [vendored/]", w.Skipped)
		}
		if _, ok := w.files()["plain.txt"]; !ok {
			t.Errorf("files = %+v, want the file beside it to have survived", w.Files)
		}
	})

	// With one, git records a mode 160000 gitlink and it diffs as an ordinary
	// added file carrying a single Subproject commit line.
	t.Run("with a commit, it is an ordinary row", func(t *testing.T) {
		f := branched(t)
		f.Git("init", "-q", "-b", "main", "vendored")
		f.Git("-C", "vendored", "config", "user.name", "Test")
		f.Git("-C", "vendored", "config", "user.email", "test@example.com")
		f.Write("vendored/f.txt", "inside\n")
		f.Git("-C", "vendored", "add", "-A")
		f.Git("-C", "vendored", "commit", "-q", "-m", "inner")

		w, _ := f.decode()

		if len(w.Skipped) != 0 {
			t.Errorf("skipped = %v, want nothing skipped once it has a commit", w.Skipped)
		}
		// No trailing slash: this is a tree entry now, not a directory listing.
		if got := w.files()["vendored"]; got != "added" {
			t.Errorf("files = %+v, want vendored as an added row", w.Files)
		}
	})
}

// A symlink is diffed rather than skipped. git stores the target path as the
// content and hands back the blob it would record on commit, so one added line
// naming the target is the whole change.
func TestAnUntrackedSymlinkIsDiffedAsItsTargetPath(t *testing.T) {
	f := branched(t)

	if err := os.Symlink("a.txt", filepath.Join(f.Dir(), "link.txt")); err != nil {
		t.Fatalf("creating the symlink: %v", err)
	}

	out := f.mustRun()

	if !strings.Contains(out, "A  link.txt") {
		t.Errorf("the symlink is missing from the changeset:\n%s", out)
	}
	if !strings.Contains(out, "+1 -0") {
		t.Errorf("output = %q, want one added line, the target path rather than its contents", out)
	}
}

// A path git could not read has to be named in both outputs.
//
// The tracked case is the dangerous one and the reason this is a list rather
// than a count: the snapshot index is seeded from HEAD, so a tracked file add
// gave up on keeps the blob that was already there and reads as unchanged
// rather than as missing.
func TestPathsGitCouldNotReadAreNamedInBothOutputs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file with no permissions, so there is nothing to skip")
	}

	for _, tc := range []struct{ name, path string }{
		{name: "untracked", path: "untracked.txt"},
		{name: "tracked, and otherwise indistinguishable from unchanged", path: "a.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := branched(t)
			f.Write(tc.path, "unreadable\n")

			locked := filepath.Join(f.Dir(), tc.path)
			if err := os.Chmod(locked, 0o000); err != nil {
				t.Fatalf("removing the permissions: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

			out := f.mustRun()
			if !strings.Contains(out, "git could not read") || !strings.Contains(out, tc.path) {
				t.Errorf("the prose does not name the path it could not read:\n%s", out)
			}

			w, _ := f.decode()
			if len(w.Skipped) != 1 || w.Skipped[0] != tc.path {
				t.Errorf("skipped = %v, want [%s]", w.Skipped, tc.path)
			}
		})
	}
}
