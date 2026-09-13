package app_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

type screen struct {
	t   *testing.T
	m   tea.Model
	src *source
}

type source struct {
	at    app.Reload
	err   error
	calls int

	wrote []string

	wroteErr   error
	moveTo     func(review.Note) (review.Note, bool)
	candidates review.BaseCandidates
	baseReads  int

	bodies  map[string]review.Body
	bodyErr error

	read []string
}

func (s *source) Reload() (app.Reload, error) {
	s.calls++
	if s.err != nil {
		return app.Reload{}, s.err
	}
	return s.at, nil
}

func (s *source) Candidates() (review.BaseCandidates, error) {
	s.baseReads++
	if s.err != nil {
		return review.BaseCandidates{}, s.err
	}
	return s.candidates, nil
}

func (s *source) SetBase(ref string) (app.Reload, error) {
	if s.wroteErr != nil {
		return app.Reload{}, s.wroteErr
	}
	s.wrote = append(s.wrote, "SetBase "+ref)
	s.at.Base = review.Base{Ref: ref, SHA: "chosen-base"}
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

func (s *source) AddComment(g review.Generation, n review.Note) (app.Reload, error) {
	where := n.Path
	if n.Scope != store.ScopeFile {
		where = fmt.Sprintf("%s %s:%d-%d", n.Path, n.Side, n.Range.Start, n.Range.End)
	}
	return s.write(fmt.Sprintf("AddComment %s %s %s gen=%d", where, n.Scope, strconv.Quote(n.Body), g.Seq))
}

func (s *source) Reanchor(n review.Note, _, _ review.Generation) (review.Note, bool, error) {
	if s.moveTo == nil {
		return n, true, nil
	}
	now, held := s.moveTo(n)
	return now, held, nil
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

func (s *source) SetSummary(text string) (string, error) {
	if s.wroteErr != nil {
		return "", s.wroteErr
	}
	s.wrote = append(s.wrote, "SetSummary "+strconv.Quote(text))
	s.at.Summary = text
	return text, nil
}

func (s *source) Body(_ review.Generation, path string) (review.Body, error) {
	if s.bodyErr != nil {
		return review.Body{}, s.bodyErr
	}
	s.read = append(s.read, path)
	return s.bodies[path], nil
}

func (s *source) write(call string) (app.Reload, error) {
	if s.wroteErr != nil {
		return app.Reload{}, s.wroteErr
	}
	s.wrote = append(s.wrote, call)
	return s.at, nil
}

func name(h review.Hunk) string {
	side, line := h.Name()
	return fmt.Sprintf("%s:%d", side, line)
}

func open(t *testing.T, width, height int) *screen {
	t.Helper()
	return named(t, "zen-review", width, height)
}

func themed(t *testing.T, th theme.Theme, width, height int) *screen {
	t.Helper()

	r := app.Reload{
		Base:       review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"},
		Generation: review.Generation{ID: 2, Seq: 2},
		Changeset:  testchangeset.Nested(t),
	}

	src := &source{at: r}
	s := &screen{t: t, m: app.New(th, src, "zen-review", r), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

func named(t *testing.T, repo string, width, height int) *screen {
	t.Helper()
	return build(t, repo, testchangeset.Nested(t), width, height)
}

func over(t *testing.T, c review.Changeset, width, height int) *screen {
	t.Helper()
	return build(t, "zen-review", c, width, height)
}

func commented(t *testing.T, width, height int, comments ...store.Comment) *screen {
	t.Helper()
	return with(t, "zen-review", testchangeset.Nested(t), comments, "", width, height)
}

func previewing(t *testing.T, path string, lines, width, height int) *screen {
	t.Helper()

	s := open(t, width, height)
	s.src.bodies = map[string]review.Body{path: testchangeset.Body(lines)}
	return s
}

func replacing(t *testing.T, width, height int, block ...string) *screen {
	t.Helper()

	r := app.Reload{
		Base:       review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"},
		Generation: review.Generation{ID: 2, Seq: 2},
		Changeset:  testchangeset.Nested(t),
		Comments:   testchangeset.NestedComments(),
		Replaced:   map[string][]string{respondedCard: block},
	}

	src := &source{at: r}
	s := &screen{t: t, m: app.New(testtheme.Dark, src, "zen-review", r), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

func noting(t *testing.T, summary string, width, height int) *screen {
	t.Helper()
	return with(t, "zen-review", testchangeset.Nested(t), nil, summary, width, height)
}

func build(t *testing.T, repo string, c review.Changeset, width, height int) *screen {
	t.Helper()
	return with(t, repo, c, nil, "", width, height)
}

func measured(t *testing.T, base review.Base, c review.Changeset, width, height int) *screen {
	t.Helper()

	r := app.Reload{Base: base, Generation: review.Generation{ID: 2, Seq: 2}, Changeset: c}
	src := &source{at: r}
	s := &screen{t: t, m: app.New(testtheme.Dark, src, "zen-review", r), src: src}
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
	s := &screen{t: t, m: app.New(testtheme.Dark, src, repo, r), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

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

func (s *screen) hold(msg tea.Msg) tea.Cmd {
	s.t.Helper()

	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
	return cmd
}

func (s *screen) drain(cmd tea.Cmd) {
	s.t.Helper()

	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}

		if batch, ok := out.(tea.BatchMsg); ok {
			for _, next := range batch {
				s.drain(next)
			}
			return
		}
		s.m, cmd = s.m.Update(out)
	}
}

func (s *screen) frame() string {
	s.t.Helper()
	return ansi.Strip(s.raw())
}

func (s *screen) raw() string {
	s.t.Helper()
	return s.m.View().Content
}

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

func (s *screen) treeRow(i int) string {
	s.t.Helper()

	runes := []rune(s.lines()[i])
	return strings.TrimSpace(string(runes[1 : s.treeColumns()-1]))
}

func (s *screen) lines() []string {
	s.t.Helper()
	return strings.Split(s.frame(), "\n")
}

func (s *screen) title() string {
	s.t.Helper()
	return s.lines()[0]
}

func (s *screen) bar() string {
	s.t.Helper()

	lines := s.lines()
	return lines[len(lines)-1]
}

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

func (s *screen) resolving(comments ...store.Comment) *screen {
	s.t.Helper()

	s.src.at.Comments = comments
	return s
}

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
