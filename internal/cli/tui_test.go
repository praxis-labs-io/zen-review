package cli

import (
	"io"
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/testrepo"
)

// TestInteractive. A pipe and a CI runner get the printed changeset, because a
// program waiting for keys on the other end of a pipe is a program that hangs.
func TestInteractive(t *testing.T) {
	tests := []struct {
		name   string
		asJSON bool
		isTTY  bool
		want   bool
	}{
		{"a terminal", false, true, true},
		{"a pipe", false, false, false},
		{"--json on a terminal", true, true, false},
		{"--json down a pipe", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := interactive(tt.asJSON, tt.isTTY); got != tt.want {
				t.Errorf("interactive(asJSON=%v, isTTY=%v) = %v, want %v",
					tt.asJSON, tt.isTTY, got, tt.want)
			}
		})
	}
}

// TestTheReloaderBringsBackWhatARefreshBuilt. The reader's key and the refresh
// subcommand run the same path, so a behaviour cannot be reachable by one and
// not the other.
func TestTheReloaderBringsBackWhatARefreshBuilt(t *testing.T) {
	repo := testrepo.New(t)
	repo.Write("a.txt", "one\n")
	repo.Commit("first")
	repo.TrackOrigin("main")
	repo.Git("checkout", "-q", "-b", "feature")
	repo.Write("a.txt", "two\n")

	s, err := review.Open(t.Context(), repo.Dir(), review.Options{})
	if err != nil {
		t.Fatal(err)
	}

	src := &reloader{ctx: t.Context(), s: s}
	defer func() {
		if err := src.close(io.Discard); err != nil {
			t.Error(err)
		}
	}()

	first, err := src.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation.Seq != 1 {
		t.Errorf("the first reload built generation %d, want 1", first.Generation.Seq)
	}
	if len(first.Changeset.Files) != 1 {
		t.Errorf("the changeset holds %d files, want a.txt alone", len(first.Changeset.Files))
	}
	if first.Base.Ref == "" || first.Base.SHA == "" {
		t.Errorf("the reload carries no base: %+v", first.Base)
	}

	// Nothing moved, so the refresh hands back the generation it already had
	// rather than building one. That equality is what the bar reads to say so.
	again, err := src.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if again.Generation.ID != first.Generation.ID {
		t.Errorf("a reload over an unchanged work tree built generation %d over %d",
			again.Generation.ID, first.Generation.ID)
	}

	repo.Write("a.txt", "three\n")
	moved, err := src.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if moved.Generation.ID == first.Generation.ID {
		t.Error("a reload over a moved work tree built no generation")
	}
}

// TestAReloadQueuedPastTheCloseDoesNothing. Bubble Tea does not wait for a
// command it started, so s then q leaves one queued behind the close. A refresh
// there runs `git add -A` over the work tree for an answer nobody is left to
// read.
func TestAReloadQueuedPastTheCloseDoesNothing(t *testing.T) {
	repo := testrepo.New(t)
	repo.Write("a.txt", "one\n")
	repo.Commit("first")
	repo.TrackOrigin("main")
	repo.Git("checkout", "-q", "-b", "feature")
	repo.Write("a.txt", "two\n")

	s, err := review.Open(t.Context(), repo.Dir(), review.Options{})
	if err != nil {
		t.Fatal(err)
	}

	src := &reloader{ctx: t.Context(), s: s}
	if err := src.close(io.Discard); err != nil {
		t.Fatal(err)
	}

	if _, err := src.Reload(); err == nil {
		t.Error("a reload after the close went ahead")
	}
}
