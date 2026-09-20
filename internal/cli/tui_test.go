package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/config"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/testrepo"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
)

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

func TestAReloadRefusedByAnotherInstanceIsOvertaken(t *testing.T) {
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

	other, err := review.Open(t.Context(), repo.Dir(), review.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	}()
	repo.Write("a.txt", "three\n")
	if _, err := other.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	_, stale := src.at(first.Generation)
	broken := errors.New("git is gone")

	for _, tt := range []struct {
		name string
		err  error
		want error
	}{
		{"the ref moved under the build", errOvertaken, app.ErrOvertaken},
		{"the generation moved under the read", stale, app.ErrOvertaken},
		{"anything else", broken, broken},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatal("the case has no error to map")
			}
			if got := overtaken(tt.err); !errors.Is(got, tt.want) {
				t.Errorf("overtaken(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestTheReaderChecksForAReleaseUnlessConfigSaysNot(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		check   bool
		warning string
	}{
		{name: "no config", check: true},
		{name: "turned off", body: `{"update_check": false}`},
		{name: "a key it does not know", body: `{"update_checks": false}`, warning: "the release check is off"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(config.DirEnv, dir)
			if tt.body != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			l := launch()
			if (l.Check != nil) != tt.check {
				t.Errorf("check set = %v, want %v", l.Check != nil, tt.check)
			}
			switch {
			case tt.warning == "" && l.Warning != nil:
				t.Errorf("warning = %v, want none", l.Warning)
			case tt.warning != "" && (l.Warning == nil || !strings.Contains(l.Warning.Error(), tt.warning)):
				t.Errorf("warning = %v, want it to say %q", l.Warning, tt.warning)
			}
		})
	}
}

func TestMockupRefusesWhatItCannotAnswer(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		isTTY bool
		says  string
	}{
		{"down a pipe", nil, false, "no terminal to open it on"},
		{"with --json", []string{"--json"}, true, "does not take --json"},
		{"with --base", []string{"--base", "main"}, true, "does not take --base"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewRoot()
			if err := root.ParseFlags(append([]string{"--mockup"}, tt.args...)); err != nil {
				t.Fatal(err)
			}

			err := runMockup(root, tt.isTTY)
			if err == nil {
				t.Fatal("the call went ahead")
			}
			if !strings.Contains(err.Error(), tt.says) {
				t.Errorf("err = %v, want it to say %q", err, tt.says)
			}
		})
	}
}
