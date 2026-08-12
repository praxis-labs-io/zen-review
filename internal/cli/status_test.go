package cli_test

import (
	"strings"
	"testing"
)

// Status is a read. Building is what bare zen-review and refresh do, and the
// ref is what proves the difference rather than the wording of the output.
func TestStatusWritesNothing(t *testing.T) {
	f := edited(t)

	w, _ := f.decode("status")

	if w.Generation != nil {
		t.Errorf("generation = %+v, want null on a session that never refreshed", *w.Generation)
	}
	if !w.Stale {
		t.Error("nothing has been reviewed against, so everything on disk is unseen")
	}
	if len(w.Files) != 0 {
		t.Errorf("files = %+v, want nothing until a generation exists", w.Files)
	}
	if refs := f.sessionRefs(); refs != nil {
		t.Errorf("refs = %v, want status to have written none", refs)
	}
}

func TestStatusOnAFreshSessionSaysWhatToRun(t *testing.T) {
	f := edited(t)

	out := f.mustRun("status")

	if !strings.Contains(out, "no generation yet, so run zen-review refresh") {
		t.Errorf("output does not say what to do about it:\n%s", out)
	}
}

// After a refresh the status describes the generation that was built, and the
// changeset comes back with it.
func TestStatusAfterARefreshReportsTheGeneration(t *testing.T) {
	f := edited(t)
	built, _ := f.decode("refresh")

	w, _ := f.decode("status")

	if w.Generation == nil {
		t.Fatal("status reported no generation after a refresh")
	}
	if w.Generation.Seq != built.Generation.Seq || w.Generation.Commit != built.Generation.Commit {
		t.Errorf("generation = %+v, want the one refresh built, %+v", *w.Generation, *built.Generation)
	}
	if w.Stale {
		t.Errorf("status = stale with nothing touched since the refresh, reason %q", w.StaleReason)
	}
	if _, ok := w.files()["a.txt"]; !ok {
		t.Errorf("files = %+v, want the changeset the generation holds", w.Files)
	}
}

// An edit makes the generation stale, and what comes back is what was reviewed
// rather than what is on disk now. That second half is the whole point of a
// generation and the thing a golden file cannot see.
func TestAnEditMakesTheStatusStaleAndKeepsTheReviewedContent(t *testing.T) {
	f := edited(t)
	f.mustRun("refresh")

	f.Write("b.txt", "written after the generation was built\n")

	w, _ := f.decode("status")

	if !w.Stale {
		t.Error("the work tree moved and the status does not say so")
	}
	if w.StaleReason != "tree" {
		t.Errorf("staleReason = %q, want tree", w.StaleReason)
	}
	if _, ok := w.files()["b.txt"]; ok {
		t.Errorf("files = %+v, want what was reviewed rather than what is on disk", w.Files)
	}

	out := f.mustRun("status")
	if !strings.Contains(out, "the work tree has moved") {
		t.Errorf("the prose does not say the work tree moved:\n%s", out)
	}

	// And a refresh clears it, which is the other half of the loop.
	after, _ := f.decode("refresh")
	if after.Stale {
		t.Error("a refresh did not clear the staleness it was run to clear")
	}
	if _, ok := after.files()["b.txt"]; !ok {
		t.Errorf("files = %+v, want the new file after a refresh", after.Files)
	}
}

// base_ref sticks and base_sha follows the branch, so a rebase onto a newer base
// moves the fork point. The engine reports one Stale bool over both causes; the
// two bases in the payload are what tell them apart.
func TestARebaseIsStaleForADifferentReasonThanAnEdit(t *testing.T) {
	f := branched(t)
	f.Write("work.txt", "the branch's own work\n")
	f.Commit("on the feature branch")

	built, _ := f.decode("refresh")

	// main moves on, and the branch is replayed onto it, which is what an agent
	// rebasing onto a newer origin/main leaves behind.
	f.Git("checkout", "-q", "main")
	f.Write("upstream.txt", "landed while we were working\n")
	f.Commit("upstream")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "feature")
	f.Git("rebase", "-q", "main")

	w, _ := f.decode("status")

	if !w.Stale {
		t.Fatal("the fork point moved and the status does not say so")
	}
	if w.StaleReason != "base" {
		t.Errorf("staleReason = %q, want base", w.StaleReason)
	}
	if w.Base.SHA == w.Generation.BaseSha {
		t.Error("both bases are the same, so this test proves nothing")
	}
	if w.Generation.BaseSha != built.Generation.BaseSha {
		t.Error("the generation's own base moved, and only the session's should have")
	}

	out := f.mustRun("status")
	if !strings.Contains(out, "the base moved to") {
		t.Errorf("the prose does not say the base moved:\n%s", out)
	}
	// The upstream commit is not this branch's work, and measuring from the old
	// fork point is exactly how it would look like it was.
	if _, ok := w.files()["upstream.txt"]; ok {
		t.Errorf("files = %+v, want the rebase's own commits left out", w.Files)
	}
}
