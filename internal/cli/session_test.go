package cli_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTwoInvocationsShareOneSession(t *testing.T) {
	f := edited(t)

	first, _ := f.decode("status")
	second, _ := f.decode("status")

	if first.Session != second.Session {
		t.Errorf("session = %s then %s, want one session for one branch", first.Session, second.Session)
	}
	if first.Kind != "branch" || first.Branch != "feature" {
		t.Errorf("session is keyed on %s/%s, want the branch", first.Kind, first.Branch)
	}
}

func TestTheBaseSticksUntilAnotherIsPassed(t *testing.T) {
	f := newFixture(t)
	f.Write("a.txt", "one\n")
	first := f.Commit("first")
	f.TrackOrigin("HEAD")
	f.Write("a.txt", "two\n")
	f.Commit("second")
	f.Git("checkout", "-q", "-b", "feature")
	f.Write("a.txt", "three\n")

	f.mustRun("--base", first)

	kept, _ := f.decode("status")
	if kept.Base.Ref != first {
		t.Errorf("base = %s, want the flag from the run before to have stuck", kept.Base.Ref)
	}

	f.mustRun("status", "--base", "origin/main")

	replaced, _ := f.decode("status")
	if replaced.Base.Ref != "origin/main" {
		t.Errorf("base = %s, want the new flag to have replaced it", replaced.Base.Ref)
	}
}

func TestTheBaseIsTheMergeBaseAndNotTheTip(t *testing.T) {
	f := branched(t)
	f.Write("work.txt", "the branch's own work\n")
	f.Commit("on the feature branch")

	f.Git("checkout", "-q", "main")
	f.Write("upstream.txt", "landed elsewhere\n")
	tip := f.Commit("upstream")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")

	w, _ := f.decode("status")

	want := f.Git("merge-base", "origin/main", "HEAD")
	if w.Base.SHA != want {
		t.Errorf("base.sha = %s, want the merge base %s", w.Base.SHA, want)
	}
	if w.Base.SHA == tip {
		t.Error("base.sha is origin/main's tip, so the changeset would carry commits this branch never made")
	}
}

func TestRunningFromASubdirectoryFindsTheSameSession(t *testing.T) {
	f := edited(t)
	f.Write("nested/deep/b.txt", "two\n")

	top, _ := f.decode("status")

	nested, _ := f.decodeFrom(filepath.Join(f.Dir(), "nested", "deep"), "status")

	if nested.Session != top.Session {
		t.Errorf("session = %s from a subdirectory, want %s", nested.Session, top.Session)
	}
}

func TestALinkedWorktreeSharesTheDatabaseAndNotTheSession(t *testing.T) {
	f := edited(t)
	f.mustRun("refresh")

	parent, _ := f.decode("status")

	tree := filepath.Join(filepath.Dir(f.Dir()), "linked")
	f.Git("worktree", "add", "-q", "-b", "in-the-worktree", tree, "main")
	if err := os.WriteFile(filepath.Join(tree, "c.txt"), []byte("in the worktree\n"), 0o644); err != nil {
		t.Fatalf("writing in the worktree: %v", err)
	}

	linked, _ := f.decodeFrom(tree, "refresh")

	if linked.Session == parent.Session {
		t.Errorf("the worktree's branch reused the parent's session %s", parent.Session)
	}

	if refs := f.sessionRefs(); len(refs) != 2 {
		t.Errorf("refs = %v, want one per session in the shared repository", refs)
	}
}

func TestADetachedHeadIsItsOwnKindOfSession(t *testing.T) {
	f := branched(t)
	f.Write("b.txt", "two\n")
	f.Commit("on the branch")
	f.Git("checkout", "-q", "--detach")
	f.Write("c.txt", "while detached\n")

	w, _ := f.decode("status")

	if w.Kind != "detached" {
		t.Errorf("kind = %q, want detached", w.Kind)
	}
	if w.Branch != "" {
		t.Errorf("branch = %q, want nothing to key on", w.Branch)
	}
}
