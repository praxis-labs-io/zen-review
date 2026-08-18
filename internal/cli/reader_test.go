package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/testrepo"
	"github.com/zen-review/zen-review/internal/tui/app"
	"github.com/zen-review/zen-review/internal/tui/theme"
)

// reader is app.Model over a live session, with the runtime's job done by hand:
// a command the model returns is run and its message delivered, which is the
// difference between what a reader sees and what Update happened to return.
//
// The screen harness in app's tests answers to a fake Source. This one is the
// real reloader over a real repository, which is what a fake cannot be checked
// against.
type reader struct {
	t    *testing.T
	m    tea.Model
	src  *reloader
	repo *testrepo.Repo

	closed bool
}

// driving opens the session, takes the opening changeset off the reload key the
// way runTUI does, and sizes the terminal. Init returns nil, so the size only
// ever arrives as a message.
func driving(t *testing.T, repo *testrepo.Repo) *reader {
	t.Helper()

	s, err := review.Open(t.Context(), repo.Dir(), review.Options{})
	if err != nil {
		t.Fatal(err)
	}

	// The cleanup goes on before anything that can fail, or a session opened and
	// then fataled past stays open while the repository is removed under it.
	src := &reloader{ctx: t.Context(), s: s}
	r := &reader{t: t, src: src, repo: repo}
	t.Cleanup(r.release)

	first, err := src.Reload()
	if err != nil {
		t.Fatal(err)
	}
	r.m = app.New(theme.RosePineMoon, src, s.Repo(), first)

	r.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	return r
}

func (r *reader) press(keys ...string) *reader {
	r.t.Helper()
	for _, k := range keys {
		r.send(keystroke(k))
	}
	return r
}

// typing puts a whole body in the box at once. A paste arrives as its own
// message rather than as keys, which is the route the root already has for it.
func (r *reader) typing(text string) *reader {
	r.t.Helper()
	r.send(tea.PasteMsg{Content: text})
	return r
}

func (r *reader) send(msg tea.Msg) {
	r.t.Helper()

	// A press past the read-back is refused by the reloader as a toast nothing
	// here reads, so the test would go green on a session that wrote nothing.
	if r.closed {
		r.t.Fatal("the session was closed by a read-back, and this press writes nothing")
	}

	var cmd tea.Cmd
	r.m, cmd = r.m.Update(msg)
	r.drain(cmd)
}

// drain runs a command and delivers what it returned, and whatever that returns
// in turn. Every write here comes back as one, so a test that stopped at Update
// would assert on a transaction that never ran.
func (r *reader) drain(cmd tea.Cmd) {
	r.t.Helper()

	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}

		// Update ignores a batch, so every command inside one would be dropped
		// and the drive would report writes it never made.
		if b, ok := out.(tea.BatchMsg); ok {
			r.t.Fatalf("the model batched %d commands, which this harness runs one at a time", len(b))
		}
		r.m, cmd = r.m.Update(out)
	}
}

// frame is the screen with the escapes stripped.
func (r *reader) frame() string {
	r.t.Helper()
	return ansi.Strip(r.m.View().Content)
}

// release lets the session go, the way runTUI's deferred close does.
func (r *reader) release() {
	r.t.Helper()

	if r.closed {
		return
	}
	r.closed = true
	if err := r.src.close(io.Discard); err != nil {
		r.t.Error(err)
	}
}

// readBack closes the session and runs the command against the same database,
// which is the second process a hook or a script would be.
func (r *reader) readBack(args ...string) back {
	r.t.Helper()
	r.release()
	r.t.Chdir(r.repo.Dir())

	cmd := NewRoot()
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetArgs(append(append([]string{}, args...), "--json"))

	if err := cmd.Execute(); err != nil {
		r.t.Fatalf("zen-review %v --json: %v\n%s%s", args, err, out.String(), errs.String())
	}
	if errs.Len() != 0 {
		r.t.Errorf("zen-review %v --json wrote to stderr on success: %q", args, errs.String())
	}

	var b back
	if err := json.Unmarshal(out.Bytes(), &b); err != nil {
		r.t.Fatalf("zen-review %v --json did not write JSON: %v\n%s", args, err, out.String())
	}
	return b
}

// back is what the three read commands answer with, narrowed to what these
// tests assert. The keys are spelled again rather than shared with the code that
// writes them, because a test reading the same struct cannot catch a rename.
type back struct {
	Comments []struct {
		Path  string `json:"path"`
		Side  string `json:"side"`
		Scope string `json:"scope"`
		Start int    `json:"start"`
		End   int    `json:"end"`
		State string `json:"state"`
		Body  string `json:"body"`
	} `json:"comments"`

	Files []struct {
		Path  string `json:"path"`
		State string `json:"state"`
		Hunks []struct {
			Side  string `json:"side"`
			Line  int    `json:"line"`
			State string `json:"state"`
		} `json:"hunks"`
	} `json:"files"`

	Summary string `json:"summary"`
}

// pair is two one-line files changed on a branch, so each hunk covers one line
// and an anchor read back is unambiguous.
func pair(t *testing.T) *testrepo.Repo {
	t.Helper()

	repo := testrepo.New(t)
	repo.Write("alpha.txt", "one\n")
	repo.Write("beta.txt", "one\n")
	repo.Commit("first")
	repo.TrackOrigin("main")

	repo.Git("checkout", "-q", "-b", "feature")
	repo.Write("alpha.txt", "two\n")
	repo.Write("beta.txt", "two\n")
	return repo
}

// keystroke reads a key the way key.Matches does: Text is what a printable key
// carries, and a name alone is what everything else answers to. Only the names
// these tests press have a case; the panic is the next one's instruction.
func keystroke(k string) tea.KeyPressMsg {
	if k == "ctrl+s" {
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	}

	r := []rune(k)
	if len(r) != 1 {
		panic("keystroke: " + k + " needs a case of its own")
	}
	return tea.KeyPressMsg{Code: r[0], Text: k}
}

// lines is a file of numbered lines, which is what makes a translated anchor
// readable when it moves.
func lines(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// rewritten is the twenty lines with line 10 changed, which is the one-hunk
// changeset that branch carries.
func rewritten() string {
	return strings.Replace(lines(1, 20), "line 10\n", "line rewritten\n", 1)
}

// TestASessionDrivenFromTheReaderReadsBackOutOfTheCLI is the milestone's
// done-when: a full session run from the keys, read back out by a second
// process. Every other test of these seams runs against the fake Source, and a
// fake can be wrong the same way twice.
func TestASessionDrivenFromTheReaderReadsBackOutOfTheCLI(t *testing.T) {
	r := driving(t, pair(t))

	// The pane opens on the heading, where c scopes to the hunk. Two j put the
	// cursor on the added line, where it scopes to that line alone.
	r.press("c").typing("alpha wants a word").press("ctrl+s")
	r.press("r")
	r.press("j", "j")
	r.press("c").typing("beta wants a word").press("ctrl+s")
	r.press("R")
	r.press("C").typing("held the store changes until the migration lands").press("ctrl+s")

	// The ring walks what is unresolved and wraps, so ] from beta's own card
	// comes round to the one on alpha.
	r.press("]", "x")

	// The reader draws what the write handed back: ] brought the card up and x
	// settled it, both on screen before anything goes near the database.
	if got := r.frame(); !strings.Contains(got, "alpha wants a word") ||
		!strings.Contains(got, "resolved") {
		t.Errorf("the settled card is not on screen:\n%s", got)
	}

	comments := r.readBack("comments")
	if len(comments.Comments) != 2 {
		t.Fatalf("the session holds %d comments, want the two c wrote:\n%+v",
			len(comments.Comments), comments.Comments)
	}

	want := []struct {
		path, side, scope, state, body string
		start, end                     int
	}{
		{"alpha.txt", "head", "range", "resolved", "alpha wants a word", 1, 1},
		{"beta.txt", "head", "line", "open", "beta wants a word", 1, 1},
	}
	for i, w := range want {
		got := comments.Comments[i]
		if got.Path != w.path || got.Side != w.side || got.Scope != w.scope ||
			got.State != w.state || got.Body != w.body ||
			got.Start != w.start || got.End != w.end {
			t.Errorf("the comment on %s came back as %+v, want %+v", w.path, got, w)
		}
	}

	files := r.readBack("files")
	if len(files.Files) != 2 {
		t.Fatalf("the changeset holds %d files, want alpha.txt and beta.txt", len(files.Files))
	}
	for _, f := range files.Files {
		if f.State != "reviewed" {
			t.Errorf("%s reads %s after r and R, want reviewed", f.Path, f.State)
		}
		if len(f.Hunks) != 1 || f.Hunks[0].State != "reviewed" {
			t.Errorf("%s came back with hunks %+v, want the one, reviewed", f.Path, f.Hunks)
		}
	}

	if got := r.readBack("summary").Summary; got != "held the store changes until the migration lands" {
		t.Errorf("C wrote the note as %q", got)
	}
}

// TestACommentFromTheReaderMovesWithARealRefresh. An anchor translates through a
// diff of two generation trees, and s is the key that builds one. Nothing short
// of a real repository under it checks that the reader's comment and the
// translation agree about where the comment was.
func TestACommentFromTheReaderMovesWithARealRefresh(t *testing.T) {
	repo := testrepo.New(t)
	repo.Write("gamma.txt", lines(1, 20))
	repo.Commit("first")
	repo.TrackOrigin("main")

	repo.Git("checkout", "-q", "-b", "feature")
	repo.Write("gamma.txt", rewritten())

	r := driving(t, repo)
	r.press("c").typing("this one has to survive the rewrite").press("ctrl+s")

	// Five lines above everything the hunk covers, so the anchor translates
	// rather than being cut.
	repo.Write("gamma.txt", lines(-4, 0)+rewritten())
	r.press("s")

	got := r.readBack("comments")
	if len(got.Comments) != 1 {
		t.Fatalf("the session holds %d comments, want the one c wrote:\n%+v",
			len(got.Comments), got.Comments)
	}

	// The pane opens on the heading, so c took the hunk, whose anchor is the one
	// line the hunk changes: head 10, and 15 once five lines go in above it.
	c := got.Comments[0]
	if c.State != "open" {
		t.Errorf("the comment came back %s, want open: the rewrite was above every line it covers", c.State)
	}
	if c.Start != 15 || c.End != 15 {
		t.Errorf("the comment came back at %d-%d, want 15-15", c.Start, c.End)
	}
}
