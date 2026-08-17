package app_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/testchangeset"
	"github.com/zen-review/zen-review/internal/tui/app"
)

// screen is a model with the runtime's job done by hand: a command a model
// returns is run and its message delivered, which is the difference between
// testing what the reader sees and testing what Update happened to return.
type screen struct {
	t   *testing.T
	m   tea.Model
	src *source
}

// source is the reload key with the git taken out: whatever the next press
// should bring back, and a count of the presses that took it.
//
// It answers with the same reload every time unless a test moves it, which is
// the work tree not having changed.
type source struct {
	at    app.Reload
	err   error
	calls int

	// wrote is what the write methods were asked to do, in order. A golden
	// cannot show that r named the right hunk, and that is the whole invariant.
	wrote []string

	// wroteErr is what the next write comes back with, where err is the
	// reload's. The two keys fail for different reasons and are asserted apart.
	wroteErr error
}

func (s *source) Reload() (app.Reload, error) {
	s.calls++
	if s.err != nil {
		return app.Reload{}, s.err
	}
	return s.at, nil
}

func (s *source) MarkHunk(g review.Generation, path string, h review.Hunk) (app.Reload, error) {
	return s.write(fmt.Sprintf("MarkHunk %s %s gen=%d", path, name(h), g.Seq))
}

func (s *source) UnmarkHunk(g review.Generation, path string, h review.Hunk) (app.Reload, error) {
	return s.write(fmt.Sprintf("UnmarkHunk %s %s gen=%d", path, name(h), g.Seq))
}

func (s *source) MarkFile(g review.Generation, f review.File) (app.Reload, error) {
	return s.write(fmt.Sprintf("MarkFile %s gen=%d", f.Diff.Path, g.Seq))
}

func (s *source) UnmarkFile(g review.Generation, f review.File) (app.Reload, error) {
	return s.write(fmt.Sprintf("UnmarkFile %s gen=%d", f.Diff.Path, g.Seq))
}

// AddComment records where the comment was aimed as well as what it says. A
// golden cannot show that c named the right lines, and that is the invariant.
func (s *source) AddComment(g review.Generation, n review.Note) (app.Reload, error) {
	where := n.Path
	if n.Scope != store.ScopeFile {
		where = fmt.Sprintf("%s %s:%d-%d", n.Path, n.Side, n.Range.Start, n.Range.End)
	}
	return s.write(fmt.Sprintf("AddComment %s %s %s gen=%d", where, n.Scope, strconv.Quote(n.Body), g.Seq))
}

func (s *source) ResolveComment(g review.Generation, id string) (app.Reload, error) {
	return s.write(fmt.Sprintf("ResolveComment %s gen=%d", id, g.Seq))
}

func (s *source) EditComment(g review.Generation, id, body string) (app.Reload, error) {
	return s.write(fmt.Sprintf("EditComment %s %s gen=%d", id, strconv.Quote(body), g.Seq))
}

func (s *source) DeleteComment(g review.Generation, id string) (app.Reload, error) {
	return s.write(fmt.Sprintf("DeleteComment %s gen=%d", id, g.Seq))
}

// SetSummary records the call and answers with the text, which is what a session
// hands back once it has stored it.
func (s *source) SetSummary(text string) (string, error) {
	if s.wroteErr != nil {
		return "", s.wroteErr
	}
	s.wrote = append(s.wrote, "SetSummary "+strconv.Quote(text))
	s.at.Summary = text
	return text, nil
}

// write records the call and answers with whatever the test pointed it at. A
// write that failed records nothing: the transaction did not happen.
func (s *source) write(call string) (app.Reload, error) {
	if s.wroteErr != nil {
		return app.Reload{}, s.wroteErr
	}
	s.wrote = append(s.wrote, call)
	return s.at, nil
}

// name is a hunk the way review names one, which is how a test says which.
func name(h review.Hunk) string {
	side, line := h.Name()
	return fmt.Sprintf("%s:%d", side, line)
}

func open(t *testing.T, width, height int) *screen {
	t.Helper()
	return named(t, "zen-review", width, height)
}

// named opens the reader on a repository of a given name, for the assertions
// about the heading.
func named(t *testing.T, repo string, width, height int) *screen {
	t.Helper()
	return build(t, repo, testchangeset.Nested(t), width, height)
}

// over opens the reader on a changeset of the test's own, for the assertions
// about how far down the review is.
func over(t *testing.T, c review.Changeset, width, height int) *screen {
	t.Helper()
	return build(t, "zen-review", c, width, height)
}

// commented opens the reader over the fixture with comments on it, which is
// what the card and comment-ring assertions drive.
func commented(t *testing.T, width, height int, comments ...store.Comment) *screen {
	t.Helper()
	return with(t, "zen-review", testchangeset.Nested(t), comments, "", width, height)
}

// noting opens the reader on a session that already has a note, which is what a
// session resumed days later looks like and what C opens over.
func noting(t *testing.T, summary string, width, height int) *screen {
	t.Helper()
	return with(t, "zen-review", testchangeset.Nested(t), nil, summary, width, height)
}

func build(t *testing.T, repo string, c review.Changeset, width, height int) *screen {
	t.Helper()
	return with(t, repo, c, nil, "", width, height)
}

// measured opens the reader from a base of the caller's choosing, which is what
// the empty-state and fallback assertions drive.
func measured(t *testing.T, base review.Base, c review.Changeset, width, height int) *screen {
	t.Helper()

	r := app.Reload{Base: base, Generation: review.Generation{ID: 2, Seq: 2}, Changeset: c}
	src := &source{at: r}
	s := &screen{t: t, m: app.New(theme.RosePineMoon, src, "zen-review", r), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

func with(t *testing.T, repo string, c review.Changeset, comments []store.Comment,
	summary string, width, height int,
) *screen {
	t.Helper()

	r := app.Reload{
		Base:       review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"},
		Generation: review.Generation{ID: 2, Seq: 2},
		Changeset:  c,
		Comments:   comments,
		Summary:    summary,
	}

	src := &source{at: r}
	s := &screen{t: t, m: app.New(theme.RosePineMoon, src, repo, r), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

// press types a key, the way key.Matches reads one: Text is what a printable
// key carries, and a name alone is what everything else answers to.
func (s *screen) press(keys ...string) *screen {
	s.t.Helper()
	for _, k := range keys {
		s.send(keystroke(k))
	}
	return s
}

func (s *screen) send(msg tea.Msg) {
	s.t.Helper()
	s.drain(s.hold(msg))
}

// hold applies a message and stops before running the command it returned,
// which is the only place a frame shows work still in flight. The runtime runs
// that command on a goroutine and the reader sees this frame while it does.
func (s *screen) hold(msg tea.Msg) tea.Cmd {
	s.t.Helper()

	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
	return cmd
}

// drain runs a held command and delivers what it returned, and whatever that
// returns in turn.
func (s *screen) drain(cmd tea.Cmd) {
	s.t.Helper()

	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}
		s.m, cmd = s.m.Update(out)
	}
}

// frame is the screen with the escapes stripped, which is what a golden holds:
// layout, alignment and clipping survive, and a lipgloss bump does not churn
// every file.
func (s *screen) frame() string {
	s.t.Helper()
	return ansi.Strip(s.raw())
}

// raw is the screen as the terminal gets it, for the assertions about colour
// that a stripped frame cannot carry.
func (s *screen) raw() string {
	s.t.Helper()
	return s.m.View().Content
}

// treeColumns is how wide the tree's pane came out, read off the frame rather
// than assumed: it is a share of the terminal and moves with it.
func (s *screen) treeColumns() int {
	s.t.Helper()

	for i, r := range []rune(s.lines()[0]) {
		if r == '╮' {
			return i + 1
		}
	}
	s.t.Fatalf("the frame has no tree pane:\n%s", s.frame())
	return 0
}

// treeRow is one line of the tree's column with its borders and its padding
// taken off, which is what a row of that pane actually says.
func (s *screen) treeRow(i int) string {
	s.t.Helper()

	runes := []rune(s.lines()[i])
	return strings.TrimSpace(string(runes[1 : s.treeColumns()-1]))
}

func (s *screen) lines() []string {
	s.t.Helper()
	return strings.Split(s.frame(), "\n")
}

// title is the diff pane's own border, which names the file it holds.
func (s *screen) title() string {
	s.t.Helper()
	return s.lines()[0]
}

// bar is the status line, which is the last one whatever the frame is.
func (s *screen) bar() string {
	s.t.Helper()

	lines := s.lines()
	return lines[len(lines)-1]
}

// reloading points the source at a new changeset, as a generation the refresh
// built rather than one it found already there. The next press of s brings it
// back.
func (s *screen) reloading(c review.Changeset) *screen {
	s.t.Helper()

	g := s.src.at.Generation
	s.src.at = app.Reload{
		Base:       s.src.at.Base,
		Generation: review.Generation{ID: g.ID + 1, Seq: g.Seq + 1},
		Changeset:  c,
		Summary:    s.src.at.Summary,
	}
	return s
}

// wrote points the source at what a write leaves behind, at the generation
// already on screen. A mark moves no git and builds no generation.
func (s *screen) wrote(c review.Changeset) *screen {
	s.t.Helper()

	s.src.at = app.Reload{
		Base:       s.src.at.Base,
		Generation: s.src.at.Generation,
		Changeset:  c,
		Comments:   s.src.at.Comments,
		Summary:    s.src.at.Summary,
	}
	return s
}

// resolving points the source at the comments a write leaves behind, at the
// generation already on screen. Settling one moves no git.
func (s *screen) resolving(comments ...store.Comment) *screen {
	s.t.Helper()

	s.src.at.Comments = comments
	return s
}

// calls is what the source was asked to write, in order.
func (s *screen) calls() []string {
	s.t.Helper()
	return s.src.wrote
}

func keystroke(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}

	r := []rune(k)
	if len(r) != 1 {
		panic("keystroke: " + k + " needs a case of its own")
	}
	return tea.KeyPressMsg{Code: r[0], Text: k}
}
