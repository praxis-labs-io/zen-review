package diffpane_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
)

// mark is an escape because a Nerd Font glyph does not survive every editor and pipe.
const mark = "\uf0da"

const twoHunks = "internal/review/state.go"

func pane(t *testing.T, path string, width, height int) diffpane.Model {
	t.Helper()
	return commented(t, path, width, height)
}

func commented(t *testing.T, path string, width, height int, comments ...store.Comment) diffpane.Model {
	t.Helper()

	c := testchangeset.Nested(t)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(width, height)
	m.SetFile(fileAt(t, c, path), comments, nil, 2)
	return m
}

func fileAt(t *testing.T, c review.Changeset, path string) *review.File {
	t.Helper()

	for i := range c.Files {
		if c.Files[i].Diff.Path == path {
			return &c.Files[i]
		}
	}
	t.Fatalf("the fixture holds no %s", path)
	return nil
}

func rows(t *testing.T, m diffpane.Model) []string {
	t.Helper()
	return strings.Split(ansi.Strip(m.View()), "\n")
}

func joined(t *testing.T, m diffpane.Model) string {
	t.Helper()
	return strings.Join(rows(t, m), "\n")
}

func press(t *testing.T, m diffpane.Model, keys ...tea.KeyPressMsg) diffpane.Model {
	t.Helper()

	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

var (
	down     = tea.KeyPressMsg{Code: 'j', Text: "j"}
	up       = tea.KeyPressMsg{Code: 'k', Text: "k"}
	space    = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	enter    = tea.KeyPressMsg{Code: tea.KeyEnter}
	halfDown = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	halfUp   = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
)

func TestAFileIsItsLines(t *testing.T) {
	got := joined(t, pane(t, "README.md", 60, 10))

	want := []string{
		"@@ -1,3 +1,3 @@",
		"  1  1   # zen-review",
		"  3    − A diff viewer with review features bolted on.",
		"     3 + A review engine with a TUI attached.",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("no row reads %q:\n%s", w, got)
		}
	}
}

func TestAFileWithNoHunksSaysWhy(t *testing.T) {
	got := rows(t, pane(t, "assets/logo.png", 60, 10))
	if !strings.Contains(got[0], "binary") {
		t.Errorf("the pane said nothing about a binary file: %q", got[0])
	}
}

func TestEveryRowIsExactlyThePane(t *testing.T) {
	for _, width := range []int{80, 40, 24, 12} {
		m := pane(t, twoHunks, width, 10)
		for i, row := range rows(t, m) {
			if got := lipgloss.Width(row); got != width {
				t.Errorf("at width %d, row %d is %d columns: %q", width, i, got, row)
			}
		}
	}
}

func TestALineTooWideIsMarkedWhereItWasCut(t *testing.T) {
	got := rows(t, pane(t, twoHunks, 40, 10))

	cut := 0
	for _, row := range got {
		if strings.Contains(row, "…") {
			cut++
		}
	}
	if cut == 0 {
		t.Errorf("every row fit at 40 columns, so nothing was clipped:\n%s", strings.Join(got, "\n"))
	}
}

func TestATabKeepsTheColumnsInStep(t *testing.T) {
	got := joined(t, pane(t, twoHunks, 80, 20))
	if strings.Contains(got, "\t") {
		t.Errorf("a raw tab reached the pane:\n%s", got)
	}
	if !strings.Contains(got, `−     Unreviewed State = "unread"`) {
		t.Errorf("the tab did not expand to the painter's width:\n%s", got)
	}
}

func TestSourceCannotWriteToTheTerminal(t *testing.T) {
	patch := "diff --git a/loud.go b/loud.go\n" +
		"index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644\n" +
		"--- a/loud.go\n" +
		"+++ b/loud.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-const old = \"quiet\"\n" +
		"+const shout = \"\x1b[31mred\x1b[0m\a\"\n"

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(60, 4)
	m.SetFile(&c.Files[0], nil, nil, 2)

	got := joined(t, m)
	if !strings.Contains(got, `const shout = "red?"`) {
		t.Errorf("the escape and the bell survived into the row:\n%q", got)
	}
}

func TestAMissingTrailingNewlineIsSaidSoAndNotShown(t *testing.T) {
	const patch = `diff --git a/eof.go b/eof.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/eof.go
+++ b/eof.go
@@ -1,1 +1,1 @@
-package eof
\ No newline at end of file
+package eof
`

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(60, 6)
	m.SetFile(&c.Files[0], nil, nil, 2)

	got := rows(t, m)
	if !strings.Contains(got[1], "− package eof") {
		t.Fatalf("the removal is not where it was expected: %q", got[1])
	}
	if !strings.Contains(got[2], `\ No newline at end of file`) {
		t.Errorf("the annotation does not hang under the line it is about: %q", got[2])
	}
	if !strings.Contains(got[3], "+ package eof") {
		t.Errorf("the annotation displaced the addition: %q", got[3])
	}
}

func TestARenameLexesEachSideByItsOwnName(t *testing.T) {
	const comment = "# the old script"

	const patch = `diff --git a/run.py b/run.go
similarity index 40%
rename from run.py
rename to run.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/run.py
+++ b/run.go
@@ -1,1 +1,1 @@
-` + comment + `
+package run
`

	s, ok := syntax.New(testtheme.Dark.Syntax)
	if !ok {
		t.Fatalf("the theme names a Chroma style Chroma does not have: %q", testtheme.Dark.Syntax)
	}

	tokens := s.Lines("run.py", comment)[0]
	if len(tokens) != 1 {
		t.Fatalf("the Python lexer no longer reads %q as one token: %+v", comment, tokens)
	}

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(60, 4)
	m.SetFile(&c.Files[0], nil, nil, 2)

	want := lipgloss.NewStyle().
		Background(testtheme.Dark.RemovedBackground).
		Foreground(tokens[0].Color).
		Render(comment)

	if !strings.Contains(m.View(), want) {
		t.Errorf("the removed row was lexed as Go rather than as the file it came from:\n%s", joined(t, m))
	}
}

func TestCodeIsHighlighted(t *testing.T) {
	s, ok := syntax.New(testtheme.Dark.Syntax)
	if !ok {
		t.Fatalf("the theme names a Chroma style Chroma does not have: %q", testtheme.Dark.Syntax)
	}

	keyword := s.Lines(twoHunks, "type State string")[0][0]
	if keyword.Color == nil {
		t.Fatalf("the style colours no keyword, so this proves nothing: %+v", keyword)
	}

	want := lipgloss.NewStyle().Foreground(keyword.Color).Render(keyword.Text)
	if got := pane(t, twoHunks, 80, 20).View(); !strings.Contains(got, want) {
		t.Errorf("no row carries the keyword colour for %q", keyword.Text)
	}
}

func TestAChangedRowIsNotPaddedInPlainSpaces(t *testing.T) {
	m := pane(t, "README.md", 60, 10)

	for _, line := range strings.Split(m.View(), "\n") {
		if !strings.Contains(ansi.Strip(line), "A review engine with a TUI attached.") {
			continue
		}
		if strings.HasSuffix(line, " ") {
			t.Errorf("the added row ends in unstyled padding: %q", line)
		}
		return
	}
	t.Error("the added row is not on the pane")
}

func TestScrollingStopsAtBothEnds(t *testing.T) {
	m := pane(t, twoHunks, 60, 4)

	top := rows(t, m)
	if !strings.Contains(top[0], "@@ -10,5") {
		t.Fatalf("the pane did not open at the top: %q", top[0])
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if got := rows(t, m); !strings.Contains(got[len(got)-1], "return Derive(files, rows), nil") {
		t.Errorf("G did not land on the last line of the file: %q", got[len(got)-1])
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = press(t, m, up, up, up, up)
	if got := rows(t, m); !strings.Contains(got[0], "@@ -10,5") {
		t.Errorf("scrolling up ran past the first line: %q", got[0])
	}
}

func TestChangingFileTakesTheReaderToTheTop(t *testing.T) {
	c := testchangeset.Nested(t)

	m := pane(t, twoHunks, 60, 4)
	m = press(t, m, down, down)

	m.SetFile(fileAt(t, c, "README.md"), nil, nil, 2)
	if got := rows(t, m)[0]; !strings.Contains(got, "@@ -1,3 +1,3 @@") {
		t.Errorf("the new file opened part-way down: %q", got)
	}
}

func TestTheGutterFitsTheLastLineAndNoMore(t *testing.T) {
	const patch = `diff --git a/near.go b/near.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/near.go
+++ b/near.go
@@ -98,2 +98,2 @@ func near() {
-	return 98
+	return 99
`

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(60, 4)
	m.SetFile(&c.Files[0], nil, nil, 2)

	got := rows(t, m)[0]
	if indent := lipgloss.Width(got[:strings.Index(got, "@@")]); indent != 9 {
		t.Errorf("the header indents %d columns, want 9: %q", indent, got)
	}
}

func TestSelectingAHunkPutsItsHeadingOnTheTopRow(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 124)

	if got := rows(t, m)[0]; !strings.Contains(got, "@@ -120,5 +120,7 @@") {
		t.Errorf("the top row is %q, want the hunk that was selected", got)
	}
}

func TestAHunkAlreadyOnScreenWholeDoesNotMoveTheWindow(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	before := rows(t, m)[0]

	m.Select(store.SideHead, 124)
	if after := rows(t, m)[0]; after != before {
		t.Errorf("the top row moved from %q to %q", before, after)
	}
}

func TestTheHeadingOfTheSelectedHunkIsTheOnlyOneMarked(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	m.Select(store.SideHead, 13)

	var marked []string
	for _, row := range rows(t, m) {
		if strings.Contains(row, mark) {
			marked = append(marked, strings.TrimSpace(row))
		}
	}
	if len(marked) != 1 {
		t.Fatalf("%d headings carry the mark, want 1: %q", len(marked), marked)
	}
	if !strings.Contains(marked[0], "@@ -10,5 +10,5 @@") {
		t.Errorf("the mark is on %q, want the hunk that was selected", marked[0])
	}
}

func TestSelectingAHunkTheFileDoesNotHoldMarksNothing(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	before := m.View()

	m.Select(store.SideHead, 9999)
	after := m.View()

	if strings.Contains(ansi.Strip(after), mark) {
		t.Errorf("a hunk the file does not hold marked a heading anyway:\n%s", after)
	}
	if after != before {
		t.Errorf("it moved the window as well:\n%s", after)
	}
}

func TestAResizeKeepsTheReaderWhereTheyScrolledTo(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 13)

	m = press(t, m, down, down, down, down, down, down, down, down)
	before := rows(t, m)[0]

	m.SetSize(60, 8)
	if after := rows(t, m)[0]; after != before {
		t.Errorf("the resize moved the top row from %q to %q", before, after)
	}
}

func TestTheFirstSizingScrollsToTheCursor(t *testing.T) {
	c := testchangeset.Nested(t)

	m := diffpane.New(testtheme.Dark)
	m.SetFile(fileAt(t, c, twoHunks), nil, nil, 2)
	m.Select(store.SideHead, 124)
	m.SetSize(60, 8)

	if got := rows(t, m)[0]; !strings.Contains(got, "@@ -120,5 +120,7 @@") {
		t.Errorf("the top row is %q, want the hunk the cursor was on", got)
	}
}

func filled(t *testing.T, m diffpane.Model) string {
	t.Helper()

	if rows := filledRows(t, m); len(rows) > 0 {
		return rows[0]
	}
	return ""
}

func filledRows(t *testing.T, m diffpane.Model) []string {
	t.Helper()

	fill := params(t, lipgloss.NewStyle().Background(testtheme.Dark.SelectedBackground))

	var out []string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, fill) {
			out = append(out, strings.TrimRight(ansi.Strip(line), " "))
		}
	}
	return out
}

func content(row string) string {
	if r := []rune(row); len(r) > 0 {
		return string(r[1:])
	}
	return row
}

func TestJMovesTheCursorAndNotTheWindow(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 13)

	before := content(rows(t, m)[0])
	m = press(t, m, down, down)

	if after := content(rows(t, m)[0]); after != before {
		t.Errorf("j scrolled the window from %q to %q", before, after)
	}
	if want := strings.TrimRight(rows(t, m)[2], " "); filled(t, m) != want {
		t.Errorf("the cursor is on %q, want two rows down, %q", filled(t, m), want)
	}
}

func TestTheWindowFollowsTheCursorOffTheEdge(t *testing.T) {
	m := pane(t, twoHunks, 60, 4)
	m.Select(store.SideHead, 13)

	first := rows(t, m)[1]
	m = press(t, m, down, down, down, down)

	got := rows(t, m)
	if strings.Contains(joined(t, m), strings.TrimRight(first, " ")) {
		t.Fatalf("the cursor walked off the pane and the window stayed:\n%s", joined(t, m))
	}
	if want := strings.TrimRight(got[3], " "); filled(t, m) != want {
		t.Errorf("the cursor is on %q, want the bottom row, %q", filled(t, m), want)
	}
}

func TestTheCursorStopsAtBothEndsOfTheFile(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	m.Select(store.SideHead, 13)

	m = press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"}, down, down)
	if want := strings.TrimRight(rows(t, m)[15], " "); filled(t, m) != want {
		t.Errorf("the cursor is on %q, want the last row, %q", filled(t, m), want)
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"}, up, up)
	if want := strings.TrimRight(rows(t, m)[0], " "); filled(t, m) != want {
		t.Errorf("the cursor is on %q, want the first row, %q", filled(t, m), want)
	}
}

func TestTheHeadingKeepsTheCaretWhileTheCursorIsInsideTheHunk(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down)

	got := rows(t, m)
	if !strings.Contains(got[0], mark) {
		t.Errorf("the heading lost the caret to the row below it: %q", got[0])
	}
	if want := strings.TrimRight(got[2], " "); filled(t, m) != want {
		t.Errorf("the fill is on %q, want the cursor's row, %q", filled(t, m), want)
	}
}

func TestTheCursorNamesTheHunkItWalkedInto(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	m.Select(store.SideHead, 13)

	side, line, ok := m.Hunk()
	if !ok || side != store.SideHead || line != 13 {
		t.Fatalf("the cursor names %s:%d (%v), want head:13", side, line, ok)
	}

	for range 8 {
		m = press(t, m, down)
	}
	if side, line, ok = m.Hunk(); !ok || side != store.SideHead || line != 124 {
		t.Errorf("the cursor names %s:%d (%v), want head:124", side, line, ok)
	}
}

func TestTheHalfPageKeyTakesTheCursorWithIt(t *testing.T) {
	m := pane(t, twoHunks, 60, 4)
	m.Select(store.SideHead, 13)

	m = press(t, m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if filled(t, m) == "" {
		t.Errorf("ctrl+d paged the cursor off the pane:\n%s", joined(t, m))
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if filled(t, m) == "" {
		t.Errorf("ctrl+u paged the cursor off the pane:\n%s", joined(t, m))
	}
}

func params(t *testing.T, s lipgloss.Style) string {
	t.Helper()

	probe := s.Render("x")
	a, b := strings.Index(probe, "["), strings.Index(probe, "m")
	if a < 0 || b < a {
		t.Fatalf("lipgloss rendered no escape for the style: %q", probe)
	}
	return probe[a+1 : b]
}

func TestTheHeadingPinsToTheTopOnceItScrollsOff(t *testing.T) {
	m := pane(t, twoHunks, 60, 4)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down, down, down)

	got := rows(t, m)
	if !strings.Contains(got[0], "@@ -10,5") {
		t.Errorf("the top row is %q, want the cursor's hunk heading", got[0])
	}
	if len(got) != 4 {
		t.Errorf("the pane drew %d rows, want 4", len(got))
	}
}

func TestTheCursorStepsOverTheBlankBetweenHunks(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	m.Select(store.SideHead, 13)

	last := func(m diffpane.Model) string { return strings.TrimRight(rows(t, m)[6], " ") }

	for range 6 {
		m = press(t, m, down)
	}
	if got := filled(t, m); got != last(m) {
		t.Fatalf("six presses landed on %q, want the first hunk's last line, %q", got, last(m))
	}

	m = press(t, m, down)
	if got := filled(t, m); !strings.Contains(got, "@@ -120,5") {
		t.Errorf("j landed on %q, want the next hunk's heading", got)
	}

	m = press(t, m, up)
	if got := filled(t, m); got != last(m) {
		t.Errorf("k landed on %q, want the line above the blank, %q", got, last(m))
	}
}

func TestThePinFollowsTheWindowAndNotTheCursor(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 124)

	m = press(t, m, tea.KeyPressMsg{Code: 'z', Text: "z"}, tea.KeyPressMsg{Code: 'b', Text: "b"})

	got := rows(t, m)
	if !strings.Contains(got[0], "@@ -10,5") {
		t.Errorf("the top row is %q, want the heading the rows under it belong to", got[0])
	}
	if !strings.Contains(got[7], "@@ -120,5") || !strings.Contains(got[7], mark) {
		t.Errorf("the cursor's own heading is not on screen with its caret: %q", got[7])
	}
}

func TestTheHeadingIsNotDrawnTwiceWhileItIsOnScreen(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down)

	seen := 0
	for _, r := range rows(t, m) {
		if strings.Contains(r, "@@ -10,5") {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("the heading is on %d rows, want 1:\n%s", seen, joined(t, m))
	}
}

func TestZPlacesTheCursorInTheWindow(t *testing.T) {
	z := tea.KeyPressMsg{Code: 'z', Text: "z"}

	at := func() diffpane.Model {
		m := pane(t, twoHunks, 60, 8)
		m.Select(store.SideHead, 124)
		return m
	}

	for _, tt := range []struct {
		name string
		keys []tea.KeyPressMsg
		row  int
	}{
		{"zt puts the cursor on the top row", []tea.KeyPressMsg{z, {Code: 't', Text: "t"}}, 0},
		{"zz centres the cursor", []tea.KeyPressMsg{z, z}, 3},
		{"zb puts the cursor on the bottom row", []tea.KeyPressMsg{z, {Code: 'b', Text: "b"}}, 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := press(t, at(), tt.keys...)
			want := strings.TrimRight(rows(t, m)[tt.row], " ")
			if got := filled(t, m); got != want {
				t.Errorf("the cursor is on %q, want row %d, %q", got, tt.row, want)
			}
		})
	}
}

func TestZOnItsOwnDoesNothing(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 124)
	m = press(t, m, down, down)

	before := joined(t, m)
	m = press(t, m, tea.KeyPressMsg{Code: 'z', Text: "z"})
	if after := joined(t, m); after != before {
		t.Errorf("z alone moved the pane:\n%s", after)
	}

	m = press(t, m, down)
	if after := joined(t, m); after != before {
		t.Errorf("the key after z was acted on as well:\n%s", after)
	}
}

func TestAShorterTerminalKeepsTheCursorOnScreen(t *testing.T) {
	m := pane(t, twoHunks, 60, 16)
	m.Select(store.SideHead, 13)
	for range 12 {
		m = press(t, m, down)
	}

	m.SetSize(60, 4)
	if filled(t, m) == "" {
		t.Errorf("the resize left the cursor off the pane:\n%s", joined(t, m))
	}
}

func TestAFileWithNoHunksLandsUnderTheCursor(t *testing.T) {
	m := pane(t, "assets/logo.png", 60, 10)
	m.Select("", 0)

	if got := filled(t, m); !strings.Contains(got, "binary") {
		t.Errorf("the cursor is on %q, want the file's only row", got)
	}
	if _, _, ok := m.Hunk(); ok {
		t.Error("a file with no hunks named one anyway")
	}
}

func TestRestorePutsTheCursorBackWithTheWindow(t *testing.T) {
	c := testchangeset.Nested(t)

	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down, down)

	at, off := m.Cursor(), m.Scroll().Offset

	m.SetFile(fileAt(t, c, twoHunks), nil, nil, 2)
	m.Select(store.SideHead, 13)
	m.Restore(at, off)

	if want := strings.TrimRight(rows(t, m)[at-off], " "); filled(t, m) != want {
		t.Errorf("the cursor came back on %q, want %q", filled(t, m), want)
	}
	if got := m.Scroll().Offset; got != off {
		t.Errorf("the window came back at %d, want %d", got, off)
	}
}

func cards(t *testing.T, path string, width, height int) diffpane.Model {
	t.Helper()
	return commented(t, path, width, height, testchangeset.NestedComments()...)
}

func under(t *testing.T, m diffpane.Model, want string) string {
	t.Helper()

	got := rows(t, m)
	for i, row := range got {
		if strings.Contains(row, want) && i+1 < len(got) {
			return got[i+1]
		}
	}
	t.Fatalf("no row reads %q:\n%s", want, strings.Join(got, "\n"))
	return ""
}

func TestACardHangsUnderTheLineItAnswers(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)

	if got := under(t, m, `Unreviewed State = "unreviewed"`); !strings.Contains(got, "◇ open") {
		t.Errorf("the row under the line it answers is %q, want the card", got)
	}
	if got := joined(t, m); !strings.Contains(got, "unreviewed is the longer word") {
		t.Errorf("the card drew no body:\n%s", got)
	}
}

func TestAResponseHangsOffTheCardOnARail(t *testing.T) {
	got := rows(t, cards(t, twoHunks, 76, 60))

	at := -1
	for i, row := range got {
		if strings.Contains(row, "\u25c8 addressed") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no card is addressed:\n%s", strings.Join(got, "\n"))
	}

	for _, want := range []struct {
		row  int
		text string
	}{
		{at + 3, "\u2502 \u256d\u2500 response"},
		{at + 4, "\u2570\u2500\u2502 It does, and hands them back in tree order."},
		{at + 5, "  \u2570\u2500"},
	} {
		if want.row >= len(got) {
			t.Fatalf("the pane stopped at row %d, before the box:\n%s", len(got), strings.Join(got, "\n"))
		}
		if !strings.Contains(got[want.row], want.text) {
			t.Errorf("row %d reads %q, want it to hold %q", want.row, got[want.row], want.text)
		}
	}
}

func TestTheResponseBoxIsTwoColumnsInsideTheCard(t *testing.T) {
	got := rows(t, cards(t, twoHunks, 76, 60))

	corner := func(row string) int { return lipgloss.Width(row[:strings.Index(row, "\u256d")]) }

	card, box := -1, -1
	for _, row := range got {
		if strings.Contains(row, "\u25c8 addressed") {
			card = corner(row)
		}
		if card >= 0 && strings.Contains(row, "\u256d\u2500 response") {
			box = corner(row)
			break
		}
	}
	if card < 0 || box < 0 {
		t.Fatalf("the card or its response did not draw:\n%s", strings.Join(got, "\n"))
	}
	if box-card != 2 {
		t.Errorf("the box starts %d columns in, want 2", box-card)
	}
}

func TestTheResponseBoxNeverLights(t *testing.T) {
	m := cards(t, twoHunks, 76, 60)
	accent := params(t, lipgloss.NewStyle().Foreground(testtheme.Dark.Accent))

	for range 60 {
		m = press(t, m, down)

		flat, raw := rows(t, m), strings.Split(m.View(), "\n")
		for i, row := range flat {
			if !strings.Contains(row, "x resolve") || i+1 >= len(flat) {
				continue
			}
			if !strings.Contains(flat[i+1], "╭─ response") {
				continue
			}

			if !strings.Contains(raw[i], accent) {
				t.Fatalf("the card is not lit, so this proves nothing:\n%s", joined(t, m))
			}
			if strings.Contains(raw[i+1], accent) {
				t.Errorf("the response box took the accent under the cursor:\n%s", joined(t, m))
			}
			return
		}
	}
	t.Fatal("the cursor never landed on the addressed card")
}

func TestACommentWithNoResponseDrawsNoBox(t *testing.T) {
	got := joined(t, commented(t, "README.md", 76, 30,
		testchangeset.Comment("aaaaaaaaaaaa", "README.md", 0, 0, "Does this still read right?")))

	if strings.Contains(got, "response") {
		t.Errorf("a comment with no response drew a box for it:\n%s", got)
	}
}

func TestAFoldedCardTakesItsResponseWithIt(t *testing.T) {
	settled := testchangeset.Responded(
		testchangeset.In(testchangeset.Comment("aaaaaaaaaaaa", "README.md", 1, 1, "Does this read right?"),
			store.CommentResolved), "It reads fine now.")

	got := joined(t, commented(t, "README.md", 76, 30, settled))
	if strings.Contains(got, "\u256d\u2500 response") {
		t.Errorf("a folded card kept its response box:\n%s", got)
	}
}

func TestARangeCardSaysWhereItStarted(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)

	got := under(t, m, "// Never off the working tree.")
	if !strings.Contains(got, "lines 124-125") {
		t.Errorf("the range card reads %q, want the run it covers", got)
	}
}

func TestACardUnderItsOwnLineSaysNoNumber(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)

	got := under(t, m, `Unreviewed State = "unreviewed"`)
	if strings.Contains(got, "13") {
		t.Errorf("the card names its own line: %q", got)
	}
}

func TestAFileCommentHeadsTheFile(t *testing.T) {
	got := rows(t, cards(t, "README.md", 76, 20))

	if !strings.Contains(got[0], "◇ open · file") {
		t.Errorf("the first row is %q, want the file's own comment", got[0])
	}
	if !strings.Contains(got[1], "Does this still read right?") {
		t.Errorf("the card drew no body: %q", got[1])
	}
}

func TestACommentTheDiffHasNoLineForStillDraws(t *testing.T) {
	got := joined(t, cards(t, twoHunks, 76, 30))

	if !strings.Contains(got, "✕ orphaned · was line 900") {
		t.Errorf("the stray did not draw, or drew as though it were placed:\n%s", got)
	}
}

func TestAResolvedCommentIsOneRowUntilSpaceOpensIt(t *testing.T) {
	m := cards(t, "README.md", 76, 20)

	got := joined(t, m)
	if !strings.Contains(got, "╭─ ◆ resolved") {
		t.Errorf("the resolved comment lost its box:\n%s", got)
	}
	if !strings.Contains(got, "▸ The old line said it better.") {
		t.Errorf("the folded row does not say which comment it stands for:\n%s", got)
	}
	if h := boxHeight(t, m, "◆ resolved"); h != 3 {
		t.Errorf("the folded card is %d rows, want a border, one row and a border", h)
	}

	m = press(t, m, down)
	for range 7 {
		m = press(t, m, down)
	}
	m = press(t, m, space)

	got = joined(t, m)
	if strings.Contains(got, "▸ The old line said it better.") {
		t.Errorf("space did not open the folded card:\n%s", got)
	}
	if !strings.Contains(got, "│ The old line said it better.") {
		t.Errorf("the opened card does not hold the body:\n%s", got)
	}
}

func boxHeight(t *testing.T, m diffpane.Model, label string) int {
	t.Helper()

	got := rows(t, m)
	for i, row := range got {
		if !strings.Contains(row, label) {
			continue
		}
		for j := i + 1; j < len(got); j++ {
			if strings.Contains(got[j], "\u2570\u2500") {
				return j - i + 1
			}
		}
	}
	return 0
}

func TestEveryCardRowIsExactlyThePane(t *testing.T) {
	for _, width := range []int{80, 40, 24, 12, 8} {
		m := cards(t, twoHunks, width, 30)
		for i, row := range rows(t, m) {
			if got := lipgloss.Width(row); got != width {
				t.Errorf("at width %d, row %d is %d columns: %q", width, i, got, row)
			}
		}
	}
}

func TestTheScrollCounterCountsCardRows(t *testing.T) {
	const deep = 60

	plain := commented(t, twoHunks, 76, deep).Scroll().Total
	withCards := cards(t, twoHunks, 76, deep).Scroll().Total

	if withCards <= plain {
		t.Fatalf("the counter reads %d with cards and %d without", withCards, plain)
	}
	if want := len(rows(t, cards(t, twoHunks, 76, deep))); withCards > want {
		t.Errorf("the counter reads %d over a pane of %d rows", withCards, want)
	}
}

func TestJStepsOverACardInOnePress(t *testing.T) {
	m := cards(t, "README.md", 76, 20)

	m = press(t, m, down)
	if got := m.Cursor(); got != 0 {
		t.Fatalf("the first j landed on row %d, want the card's own row", got)
	}

	m = press(t, m, down)
	if got := m.Cursor(); got != 3 {
		t.Errorf("the next j landed on row %d, want the row after the card", got)
	}

	m = press(t, m, up)
	if got := m.Cursor(); got != 0 {
		t.Errorf("k landed on row %d, want the card's own row again", got)
	}
}

func TestACardTakesTheAccentBorderUnderTheCursor(t *testing.T) {
	m := cards(t, "README.md", 76, 20)
	accent := params(t, lipgloss.NewStyle().Foreground(testtheme.Dark.Accent))

	if lit(m.View(), accent, "╰─") {
		t.Errorf("the card is lit with the cursor off it:\n%s", joined(t, m))
	}
	if m = press(t, m, down); !lit(m.View(), accent, "╰─") {
		t.Errorf("the card took no accent with the cursor on it:\n%s", joined(t, m))
	}
}

func lit(view, params, want string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), want) && strings.Contains(line, params) {
			return true
		}
	}
	return false
}

func TestSelectCommentLeavesTheAnchorOnScreen(t *testing.T) {
	m := cards(t, twoHunks, 76, 6)
	m.SelectComment("cccccccccccc")

	got := joined(t, m)
	if !strings.Contains(got, "lines 124-125") {
		t.Fatalf("the card is not on screen:\n%s", got)
	}
	if !strings.Contains(got, "// Ranges are read off the generation.") {
		t.Errorf("the first line the card answers scrolled away:\n%s", got)
	}
}

func TestEnterGoesToTheLineACardAnswers(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)
	m.SelectComment("cccccccccccc")
	m = press(t, m, enter)

	if got := filled(t, m); !strings.Contains(got, "// Ranges are read off the generation.") {
		t.Errorf("enter left the cursor on %q, want the run's first line", got)
	}
}

func TestAResizeKeepsTheCursorOnTheSameCard(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)
	m.SelectComment("cccccccccccc")
	was := m.Cursor()

	m.SetSize(34, 30)
	if got, ok := m.Comment(); !ok || got != "cccccccccccc" {
		t.Errorf("the resize left the cursor on %q, want the card it was on", got)
	}

	if m.Cursor() == was {
		t.Fatalf("the card is on row %d at both widths, so the narrower one wrapped nothing", was)
	}
}

func TestABaseSideCommentMatchesThroughTheOldPath(t *testing.T) {
	const patch = `diff --git a/run.py b/run.go
similarity index 40%
rename from run.py
rename to run.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/run.py
+++ b/run.go
@@ -1,1 +1,1 @@
-print("hello")
+package run
`

	c := testchangeset.Derive(t, patch)
	old := testchangeset.OnBase(testchangeset.Comment("111111111111", "run.py", 1, 1, "python is gone"))

	m := diffpane.New(testtheme.Dark)
	m.SetSize(76, 10)
	m.SetFile(&c.Files[0], []store.Comment{old}, nil, 2)

	got := joined(t, m)
	if !strings.Contains(got, "python is gone") {
		t.Fatalf("the base-side comment did not draw:\n%s", got)
	}
	if strings.Contains(got, "was line") {
		t.Errorf("it drew as a stray rather than under the line it removed:\n%s", got)
	}
}

func TestACardSaysWasOnlyOfNumbersTheCodeHasLeft(t *testing.T) {
	tests := []struct {
		name string
		c    store.Comment
		was  bool
	}{
		{
			name: "live, outside every hunk",
			c:    testchangeset.Comment("aaaaaaaaaaaa", twoHunks, 60, 60, "about line 60"),
		},
		{
			name: "orphaned",
			c: testchangeset.In(
				testchangeset.Comment("bbbbbbbbbbbb", twoHunks, 60, 60, "about line 60"),
				store.CommentOrphaned),
			was: true,
		},
		{
			name: "frozen at an older generation",
			c:    frozenAt(1, testchangeset.Comment("cccccccccccc", twoHunks, 60, 60, "about line 60")),
			was:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joined(t, commented(t, twoHunks, 76, 30, tt.c))
			if !strings.Contains(got, "line 60") {
				t.Fatalf("the card does not name the line it is about:\n%s", got)
			}
			if said := strings.Contains(got, "was line 60"); said != tt.was {
				t.Errorf("it says `was line 60`: %v, want %v:\n%s", said, tt.was, got)
			}
		})
	}
}

func frozenAt(gen int64, c store.Comment) store.Comment {
	c.GenerationID = gen
	return c
}

func TestEveryBadgeIsOneCell(t *testing.T) {
	for _, glyph := range []string{"◇", "◈", "◆", "✕", "▸", mark} {
		if got := lipgloss.Width(glyph); got != 1 {
			t.Errorf("%q measures %d cells", glyph, got)
		}
	}
}

func TestACommentFrozenAtAnOlderGenerationDrawsWhereItPointed(t *testing.T) {
	stale := testchangeset.In(
		testchangeset.Comment("aaaaaaaaaaaa", twoHunks, 13, 13, "this was about the old line"),
		store.CommentResolved)
	stale.GenerationID = 1

	c := testchangeset.Nested(t)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(76, 30)
	m.SetFile(fileAt(t, c, twoHunks), []store.Comment{stale}, nil, 2)

	if got := joined(t, m); !strings.Contains(got, "was line 13") {
		t.Errorf("the frozen comment drew as though it still pointed at line 13:\n%s", got)
	}
}

func TestACommentFrozenAtThisGenerationStaysWhereItIs(t *testing.T) {
	settled := testchangeset.In(
		testchangeset.Comment("aaaaaaaaaaaa", twoHunks, 13, 13, "just settled"),
		store.CommentResolved)

	c := testchangeset.Nested(t)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(76, 30)
	m.SetFile(fileAt(t, c, twoHunks), []store.Comment{settled}, nil, 2)

	if got := joined(t, m); strings.Contains(got, "was line") {
		t.Errorf("a comment frozen at the generation on screen drew as a stray:\n%s", got)
	}
}

func TestACardKeepsTheParagraphsOfItsBody(t *testing.T) {
	body := "the first paragraph\n\n- a bullet\n- another"
	on := testchangeset.Comment("aaaaaaaaaaaa", "README.md", 2, 2, body)

	got := rows(t, commented(t, "README.md", 76, 20, on))
	for _, want := range []string{"the first paragraph", "- a bullet", "- another"} {
		found := false
		for _, row := range got {
			if strings.Contains(row, want) && !strings.Contains(row, "?") {
				found = true
			}
		}
		if !found {
			t.Errorf("no row reads %q on its own:\n%s", want, strings.Join(got, "\n"))
		}
	}
}

func TestSelectCommentLeavesRoomForThePin(t *testing.T) {
	m := cards(t, twoHunks, 76, 4)
	m.SelectComment("bbbbbbbbbbbb")

	if got := joined(t, m); !strings.Contains(got, `Unreviewed State = "unreviewed"`) {
		t.Errorf("the pinned heading covered the line the card answers:\n%s", got)
	}
}

func TestTheHeadingHoldsThroughAPagingKey(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{halfDown, halfUp} {
		m := pane(t, twoHunks, 70, 5)
		m.Select(store.SideHead, 124)
		m = press(t, m, key)

		if got := rows(t, m)[0]; !strings.Contains(got, "@@") {
			t.Errorf("after %v the top row is %q, want a hunk heading:\n%s",
				key, got, joined(t, m))
		}
	}
}

func TestThePinDoesNotCoverTheCursor(t *testing.T) {
	m := pane(t, twoHunks, 70, 5)
	m.Select(store.SideHead, 13)
	m = press(t, m, halfDown)

	if m.Cursor() == m.Scroll().Offset {
		t.Fatalf("the cursor is on the top row at %d, where the pin draws", m.Cursor())
	}
	if got := filled(t, m); got == "" {
		t.Errorf("the pin covered the cursor's own row:\n%s", joined(t, m))
	}
}

func TestZtLandsUnderTheHeadingItAsksToSee(t *testing.T) {
	m := pane(t, twoHunks, 70, 6)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down, down, down)
	m = press(t, m, tea.KeyPressMsg{Code: 'z', Text: "z"}, tea.KeyPressMsg{Code: 't', Text: "t"})

	got := rows(t, m)
	if !strings.Contains(got[0], "@@ -10,5") {
		t.Fatalf("the top row is %q, want the pinned heading:\n%s", got[0], joined(t, m))
	}
	if filled(t, m) == "" {
		t.Errorf("zt put the cursor under the pin and lost it:\n%s", joined(t, m))
	}
}

func TestAPagingKeyParksTheCursorMidWindow(t *testing.T) {
	const height = 7

	m := pane(t, twoHunks, 62, height)
	m.Select(store.SideHead, 13)

	m = press(t, m, halfDown)
	if got := m.Scroll().Offset; got != 0 {
		t.Errorf("the first page scrolled to %d, want the window still at the top", got)
	}
	if got := m.Cursor(); got != (height-1)/2 {
		t.Errorf("the first page put the cursor on row %d, want the middle", got)
	}

	m = press(t, m, halfDown)
	if got := m.Cursor() - m.Scroll().Offset; got != (height-1)/2 {
		t.Errorf("the second page left the cursor on screen row %d, want the middle", got)
	}

	for range 6 {
		m = press(t, m, halfDown)
	}
	if got, want := m.Cursor(), m.Scroll().Total-1; got != want {
		t.Errorf("paging to the end left the cursor on row %d, want the last at %d", got, want)
	}
}

func TestAPagingKeyParksGoingUpToo(t *testing.T) {
	const height = 7

	m := pane(t, twoHunks, 62, height)
	m.Select(store.SideHead, 124)
	m = press(t, m, halfDown, halfDown, halfUp)

	if got := m.Cursor() - m.Scroll().Offset; got != (height-1)/2 {
		t.Errorf("ctrl+u left the cursor on screen row %d, want the middle", got)
	}

	for range 6 {
		m = press(t, m, halfUp)
	}
	if got := m.Cursor(); got != 0 {
		t.Errorf("paging to the top left the cursor on row %d, want the first", got)
	}
	if got := m.Scroll().Offset; got != 0 {
		t.Errorf("the window sat at %d with the cursor on the first row", got)
	}
}

func TestAOneRowPaneKeepsTheCursorOverThePin(t *testing.T) {
	m := pane(t, twoHunks, 60, 1)
	m.Select(store.SideHead, 13)

	for i := range 4 {
		m = press(t, m, down)
		if filled(t, m) == "" {
			t.Fatalf("after %d presses nothing on the pane carries the cursor: %q",
				i+1, joined(t, m))
		}
	}
}

const respondedCard = "dddddddddddd"

func replacing(t *testing.T, width, height int, block ...string) diffpane.Model {
	t.Helper()
	return blocked(t, testchangeset.NestedComments(), width, height, block...)
}

func blocked(t *testing.T, comments []store.Comment, width, height int, block ...string) diffpane.Model {
	t.Helper()

	c := testchangeset.Nested(t)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(width, height)
	m.SetFile(fileAt(t, c, twoHunks), comments, map[string][]string{respondedCard: block}, 2)
	return m
}

func TestAResponseCarriesTheCodeItReplaced(t *testing.T) {
	got := rows(t, replacing(t, 76, 60, "files := d.Files()", "sort.Strings(files)"))

	at := -1
	for i, row := range got {
		if strings.Contains(row, "It does, and hands them back in tree order.") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the response drew no words:\n%s", strings.Join(got, "\n"))
	}

	for _, want := range []struct {
		row  int
		text string
	}{
		{at + 2, "files := d.Files()"},
		{at + 3, "sort.Strings(files)"},
	} {
		if want.row >= len(got) {
			t.Fatalf("the pane stopped at row %d, before the block:\n%s", len(got), strings.Join(got, "\n"))
		}
		if !strings.Contains(got[want.row], want.text) {
			t.Errorf("row %d reads %q, want it to hold %q", want.row, got[want.row], want.text)
		}
	}
}

func TestABlockIsTruncatedUntilTheKeyIsPressed(t *testing.T) {
	block := []string{"one", "two", "three", "four", "five"}
	got := joined(t, replacing(t, 76, 60, block...))

	for _, want := range block[:3] {
		if !strings.Contains(got, want) {
			t.Errorf("the block dropped %q, want the first three:\n%s", want, got)
		}
	}
	for _, gone := range block[3:] {
		if strings.Contains(got, gone) {
			t.Errorf("the block drew %q, want it behind the key:\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "… 2 more") {
		t.Errorf("the block dropped two lines without saying so:\n%s", got)
	}
}

func TestExpandDrawsTheWholeBlock(t *testing.T) {
	block := []string{"one", "two", "three", "four", "five"}
	m := replacing(t, 76, 60, block...)

	for range 60 {
		if id, ok := m.Comment(); ok && id == respondedCard {
			break
		}
		m = press(t, m, down)
	}
	if id, ok := m.Comment(); !ok || id != respondedCard {
		t.Fatalf("the cursor never reached the answered card")
	}

	m.Expand()
	got := joined(t, m)
	for _, want := range block {
		if !strings.Contains(got, want) {
			t.Errorf("the expanded block dropped %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "… 2 more") {
		t.Errorf("the expanded block still counts what it is not hiding:\n%s", got)
	}
	if !strings.Contains(got, "> less") {
		t.Errorf("the footer still offers more:\n%s", got)
	}

	m.Expand()
	if got := joined(t, m); !strings.Contains(got, "… 2 more") {
		t.Errorf("a second call does not put the block back:\n%s", got)
	}
}

func TestABareAddressGrowsABoxForTheBlock(t *testing.T) {
	bare := testchangeset.In(
		testchangeset.Comment(respondedCard, twoHunks, 126, 126, "Derive takes the rows now."),
		store.CommentAddressed)

	got := joined(t, blocked(t, []store.Comment{bare}, 76, 40, "files := d.Files()"))
	if !strings.Contains(got, "╭─ response") {
		t.Errorf("a bare address drew no box for the block:\n%s", got)
	}
	if !strings.Contains(got, "files := d.Files()") {
		t.Errorf("the box drew no block:\n%s", got)
	}
}

func TestAFoldedCardTakesItsBlockWithIt(t *testing.T) {
	settled := testchangeset.Responded(
		testchangeset.In(testchangeset.Comment(respondedCard, twoHunks, 126, 126, "Derive takes the rows now."),
			store.CommentResolved), "It does.")

	got := joined(t, blocked(t, []store.Comment{settled}, 76, 40, "files := d.Files()"))
	if strings.Contains(got, "files := d.Files()") {
		t.Errorf("a folded card kept its block:\n%s", got)
	}
}

func TestABlockStaysInsideTheBox(t *testing.T) {
	long := strings.Repeat("wide", 40)
	m := replacing(t, 76, 60, "\tif x {", long)

	for _, row := range rows(t, m) {
		if got := lipgloss.Width(row); got > 76 {
			t.Errorf("a row is %d cells wide, want no more than 76: %q", got, row)
		}
	}

	got := joined(t, m)
	if !strings.Contains(got, "    if x {") {
		t.Errorf("the tab was not expanded:\n%s", got)
	}
	if strings.Contains(got, long) {
		t.Errorf("the long line ran past the border rather than being clipped:\n%s", got)
	}
}

func TestTheBlockIsPaintedAsTheRemovalsItIs(t *testing.T) {
	m := replacing(t, 76, 60, "files := d.Files()")
	removed := params(t, lipgloss.NewStyle().Background(testtheme.Dark.RemovedBackground))

	for i, row := range rows(t, m) {
		if !strings.Contains(row, "files := d.Files()") {
			continue
		}
		if !strings.Contains(row, "−") {
			t.Errorf("the block row carries no removal marker: %q", row)
		}
		if raw := strings.Split(m.View(), "\n")[i]; !strings.Contains(raw, removed) {
			t.Errorf("the block row is not painted on the removed tint: %q", raw)
		}
		return
	}
	t.Fatalf("the block did not draw:\n%s", joined(t, m))
}

func TestABlankLineInABlockStillFills(t *testing.T) {
	m := replacing(t, 76, 60, "type cutter struct {", "", "}")
	removed := params(t, lipgloss.NewStyle().Background(testtheme.Dark.RemovedBackground))

	flat, raw := rows(t, m), strings.Split(m.View(), "\n")
	for i, row := range flat {
		if !strings.Contains(row, "type cutter struct {") {
			continue
		}
		if !strings.Contains(raw[i+1], removed) {
			t.Errorf("the blank row lost the tint: %q", raw[i+1])
		}
		if got := lipgloss.Width(flat[i+1]); got != lipgloss.Width(flat[i]) {
			t.Errorf("the blank row is %d cells and the one above is %d, want them level",
				got, lipgloss.Width(flat[i]))
		}
		return
	}
	t.Fatalf("the block did not draw:\n%s", joined(t, m))
}

func TestABlockEndingInABlankStillCountsIt(t *testing.T) {
	m := replacing(t, 76, 60, "one", "two", "three", "")

	got := joined(t, m)
	if !strings.Contains(got, "… 1 more") {
		t.Errorf("a four line block drew no count:\n%s", got)
	}
	if strings.Contains(got, "> less") {
		t.Errorf("the footer says the block is open when a row is held back:\n%s", got)
	}
}

func TestABareCardStandsWithoutAFill(t *testing.T) {
	const path = "internal/review/state.go"

	c := testchangeset.Nested(t)
	m := diffpane.New(testtheme.Bare)

	m.SetSize(18, 20)
	m.SetFile(fileAt(t, c, path), testchangeset.NestedComments(), nil, 2)

	seen := map[string]bool{}
	for range 40 {
		if row, ok := bareCardRow(m); ok {
			seen[row] = true
		}
		m = press(t, m, down)
	}

	if len(seen) == 0 {
		t.Fatalf("the fixture drew no bare card at this width:\n%s", ansi.Strip(m.View()))
	}
	if len(seen) == 1 {
		for row := range seen {
			t.Errorf("the card is drawn the same whether the cursor is on it or not:\n%q", row)
		}
	}
}

func bareCardRow(m diffpane.Model) (string, bool) {
	for _, row := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(row), "◇ open") {
			return row, true
		}
	}
	return "", false
}
