package diffpane_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zen-kit/zen-kit/syntax"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/testchangeset"
	"github.com/zen-review/zen-review/internal/tui/diffpane"
)

// mark is the caret the pane puts on the heading the ring is on, written as an
// escape: a Nerd Font glyph does not survive every editor and pipe, and an empty
// string is contained in every row there is.
const mark = "\uf0da"

// The fixture's two-hunk file, which is what the scrolling is measured against.
// Sixteen rows: a header and six lines, a blank, a header and seven lines.
const twoHunks = "internal/review/state.go"

func pane(t *testing.T, path string, width, height int) diffpane.Model {
	t.Helper()
	return commented(t, path, width, height)
}

// commented is a pane over the fixture with comments on it, which is what every
// card assertion drives. No comments is the same pane with none of them matching.
func commented(t *testing.T, path string, width, height int, comments ...store.Comment) diffpane.Model {
	t.Helper()

	c := testchangeset.Nested(t)
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(width, height)
	m.SetFile(fileAt(t, c, path), comments, 2)
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

// TestAFileIsItsLines: the numbers on both sides, the marker between them, and
// the code. The header alone was the placeholder this replaces.
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

// TestAFileWithNoHunksSaysWhy. A binary file is one thing to read, and a pane
// left blank on it reads as a pane that failed.
func TestAFileWithNoHunksSaysWhy(t *testing.T) {
	got := rows(t, pane(t, "assets/logo.png", 60, 10))
	if !strings.Contains(got[0], "binary") {
		t.Errorf("the pane said nothing about a binary file: %q", got[0])
	}
}

// TestEveryRowIsExactlyThePane, at widths where a line of code cannot fit. A
// pane clips overflow silently, losing trailing cells mid-cell with no
// ellipsis, and a width test on the unclipped row still passes.
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

// TestALineTooWideIsMarkedWhereItWasCut, so the reader can tell a clipped line
// from a short one.
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

// TestATabKeepsTheColumnsInStep. A raw tab is a variable number of cells, and
// one anywhere in a line puts every column after it out of step with the line
// above. The fixture's Go lines are tab-indented.
func TestATabKeepsTheColumnsInStep(t *testing.T) {
	got := joined(t, pane(t, twoHunks, 80, 20))
	if strings.Contains(got, "\t") {
		t.Errorf("a raw tab reached the pane:\n%s", got)
	}
	if !strings.Contains(got, `−     Unreviewed State = "unread"`) {
		t.Errorf("the tab did not expand to the painter's width:\n%s", got)
	}
}

// TestSourceCannotWriteToTheTerminal. A repository holds whatever it holds, and
// this one holds escape sequences in its own test fixtures. An escape reaching
// the pane is run by the terminal and takes the row's width arithmetic with it.
func TestSourceCannotWriteToTheTerminal(t *testing.T) {
	patch := "diff --git a/loud.go b/loud.go\n" +
		"index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644\n" +
		"--- a/loud.go\n" +
		"+++ b/loud.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-const old = \"quiet\"\n" +
		"+const shout = \"\x1b[31mred\x1b[0m\a\"\n"

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(60, 4)
	m.SetFile(&c.Files[0], nil, 2)

	got := joined(t, m)
	if !strings.Contains(got, `const shout = "red?"`) {
		t.Errorf("the escape and the bell survived into the row:\n%q", got)
	}
}

// TestAMissingTrailingNewlineIsSaidSoAndNotShown. Git carries it as an
// annotation on the line rather than as a line, and a pane that drops it draws
// a removal and an addition of identical text with nothing to tell the reader
// what moved.
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
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(60, 6)
	m.SetFile(&c.Files[0], nil, 2)

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

// TestARenameLexesEachSideByItsOwnName. A rename that changes the extension is
// a different language on the base, and lexing its removals as the head's
// colours them by a grammar they were never written in.
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

	s, ok := syntax.New(theme.RosePineMoon.Syntax)
	if !ok {
		t.Fatalf("the theme names a Chroma style Chroma does not have: %q", theme.RosePineMoon.Syntax)
	}

	// Python reads the line as one comment. Go has no comment starting with a
	// hash, so it breaks the same text into a run per word and paints the hash
	// as the error it is, which is why one run of the whole line is the proof.
	tokens := s.Lines("run.py", comment)[0]
	if len(tokens) != 1 {
		t.Fatalf("the Python lexer no longer reads %q as one token: %+v", comment, tokens)
	}

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(60, 4)
	m.SetFile(&c.Files[0], nil, 2)

	want := lipgloss.NewStyle().
		Background(theme.RosePineMoon.RemovedBackground).
		Foreground(tokens[0].Color).
		Render(comment)

	if !strings.Contains(m.View(), want) {
		t.Errorf("the removed row was lexed as Go rather than as the file it came from:\n%s", joined(t, m))
	}
}

// TestCodeIsHighlighted, on a context row so the kind's own tint is not in the
// way. A stripped frame cannot see a colour, so this reads the raw one.
func TestCodeIsHighlighted(t *testing.T) {
	s, ok := syntax.New(theme.RosePineMoon.Syntax)
	if !ok {
		t.Fatalf("the theme names a Chroma style Chroma does not have: %q", theme.RosePineMoon.Syntax)
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

// TestAChangedRowIsNotPaddedInPlainSpaces. Every styled run ends in a reset
// that clears the background with it, so the pane's own padding on the end of a
// tinted row would tear a hole in it. The painter runs a tint out to the full
// width for that reason, and the pane finishes only the rows it left short.
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

// TestScrollingStopsAtBothEnds, so a key held down cannot walk the content off
// the pane.
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

// TestChangingFileTakesTheReaderToTheTop. A pane left at the old offset opens a
// new file part-way down it.
func TestChangingFileTakesTheReaderToTheTop(t *testing.T) {
	c := testchangeset.Nested(t)

	m := pane(t, twoHunks, 60, 4)
	m = press(t, m, down, down)

	m.SetFile(fileAt(t, c, "README.md"), nil, 2)
	if got := rows(t, m)[0]; !strings.Contains(got, "@@ -1,3 +1,3 @@") {
		t.Errorf("the new file opened part-way down: %q", got)
	}
}

// TestTheGutterFitsTheLastLineAndNoMore. Start plus Lines is the line after the
// hunk, not its last, so a file ending at 99 sized off 100 buys a third column
// it never fills and shifts every row of the file right of its neighbours.
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
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(60, 4)
	m.SetFile(&c.Files[0], nil, 2)

	// The hunk's last line is 99, so two columns hold it. paint.HunkHeader
	// indents to gutter*2+5, which is 9 at a gutter of 2 and 11 at 3.
	got := rows(t, m)[0]
	if indent := lipgloss.Width(got[:strings.Index(got, "@@")]); indent != 9 {
		t.Errorf("the header indents %d columns, want 9: %q", indent, got)
	}
}

// TestSelectingAHunkPutsItsHeadingOnTheTopRow. A key that lands on a block is
// taking the reader somewhere, and the shortest scroll leaves the heading
// wherever the last one happened to end.
func TestSelectingAHunkPutsItsHeadingOnTheTopRow(t *testing.T) {
	// Eight rows over sixteen, so the second hunk can reach the top row with
	// room to spare below it.
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 124)

	if got := rows(t, m)[0]; !strings.Contains(got, "@@ -120,5 +120,7 @@") {
		t.Errorf("the top row is %q, want the hunk that was selected", got)
	}
}

// TestAHunkAlreadyOnScreenWholeDoesNotMoveTheWindow. Moving a block the reader
// can already read is movement for nothing, and it costs them the lines above
// it they were using for context.
func TestAHunkAlreadyOnScreenWholeDoesNotMoveTheWindow(t *testing.T) {
	// Tall enough for the whole file, so neither hunk has anywhere to go.
	m := pane(t, twoHunks, 60, 20)
	before := rows(t, m)[0]

	// The top row rather than the whole frame: Select marks the heading it lands
	// on, so the frame changes whether the window moved or not.
	m.Select(store.SideHead, 124)
	if after := rows(t, m)[0]; after != before {
		t.Errorf("the top row moved from %q to %q", before, after)
	}
}

// TestTheHeadingOfTheSelectedHunkIsTheOnlyOneMarked, so the mark answers which
// hunk rather than which file.
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

// TestSelectingAHunkTheFileDoesNotHoldMarksNothing, which is the state the pane
// is in between a file arriving and the root naming a hunk in it.
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

// TestAResizeKeepsTheReaderWhereTheyScrolledTo. The first sizing puts the reader
// on the cursor, because the size arrives after the model is built. Every one
// after it is the terminal changing shape under someone who has scrolled
// somewhere, and yanking them back to the cursor throws that away.
func TestAResizeKeepsTheReaderWhereTheyScrolledTo(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 13)

	// Down into the second hunk, well past the heading the cursor is on.
	m = press(t, m, down, down, down, down, down, down, down, down)
	before := rows(t, m)[0]

	m.SetSize(60, 8)
	if after := rows(t, m)[0]; after != before {
		t.Errorf("the resize moved the top row from %q to %q", before, after)
	}
}

// TestTheFirstSizingScrollsToTheCursor, which is the case the reader opening on
// a hunk part way down a file depends on.
func TestTheFirstSizingScrollsToTheCursor(t *testing.T) {
	c := testchangeset.Nested(t)

	// Sized after the cursor is set, the way the root builds it: New, then the
	// terminal says how big it is.
	m := diffpane.New(theme.RosePineMoon)
	m.SetFile(fileAt(t, c, twoHunks), nil, 2)
	m.Select(store.SideHead, 124)
	m.SetSize(60, 8)

	if got := rows(t, m)[0]; !strings.Contains(got, "@@ -120,5 +120,7 @@") {
		t.Errorf("the top row is %q, want the hunk the cursor was on", got)
	}
}

// filled is the row wearing the cursor's fill, stripped and unpadded, and empty
// when none does. Its parameters match bare: lipgloss puts the foreground first.
func filled(t *testing.T, m diffpane.Model) string {
	t.Helper()

	if rows := filledRows(t, m); len(rows) > 0 {
		return rows[0]
	}
	return ""
}

// filledRows is every row wearing that fill, in the order the pane draws them, which
// is what a selection covering a run of rows is asserted against.
func filledRows(t *testing.T, m diffpane.Model) []string {
	t.Helper()

	fill := params(t, lipgloss.NewStyle().Background(theme.RosePineMoon.SelectedBackground))

	var out []string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, fill) {
			out = append(out, strings.TrimRight(ansi.Strip(line), " "))
		}
	}
	return out
}

// TestJMovesTheCursorAndNotTheWindow, while the row it lands on is on screen. A
// pane that scrolls on every j takes the reader off the line they are reading.
func TestJMovesTheCursorAndNotTheWindow(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 13)

	before := rows(t, m)[0]
	m = press(t, m, down, down)

	if after := rows(t, m)[0]; after != before {
		t.Errorf("j scrolled the window from %q to %q", before, after)
	}
	if want := strings.TrimRight(rows(t, m)[2], " "); filled(t, m) != want {
		t.Errorf("the cursor is on %q, want two rows down, %q", filled(t, m), want)
	}
}

// TestTheWindowFollowsTheCursorOffTheEdge, by as little as it takes. The reader
// is already looking at the row; the window is what has fallen behind.
func TestTheWindowFollowsTheCursorOffTheEdge(t *testing.T) {
	m := pane(t, twoHunks, 60, 4)
	m.Select(store.SideHead, 13)

	// The row under the heading, which scrolling has to carry off the top. The
	// heading itself pins there, so it cannot say whether the window moved.
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

// TestTheCursorStopsAtBothEndsOfTheFile, so a key held down cannot walk it off.
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

// TestTheHeadingKeepsTheCaretWhileTheCursorIsInsideTheHunk. Once j walks off the
// heading, the caret and the fill are on different rows and both are wanted.
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

// TestTheCursorNamesTheHunkItWalkedInto, which is how the root keeps the ring on
// what the reader is looking at.
func TestTheCursorNamesTheHunkItWalkedInto(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	m.Select(store.SideHead, 13)

	side, line, ok := m.Hunk()
	if !ok || side != store.SideHead || line != 13 {
		t.Fatalf("the cursor names %s:%d (%v), want head:13", side, line, ok)
	}

	// The heading, its six lines, and the blank between the two hunks.
	for range 8 {
		m = press(t, m, down)
	}
	if side, line, ok = m.Hunk(); !ok || side != store.SideHead || line != 124 {
		t.Errorf("the cursor names %s:%d (%v), want head:124", side, line, ok)
	}
}

// TestTheHalfPageKeyTakesTheCursorWithIt. One left behind is paged off the pane,
// and the next j hauls the window back to it.
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

// params is the escape parameters a style renders with, stripped of the escape
// around them: lipgloss packs foreground and background into one run.
func params(t *testing.T, s lipgloss.Style) string {
	t.Helper()

	probe := s.Render("x")
	a, b := strings.Index(probe, "["), strings.Index(probe, "m")
	if a < 0 || b < a {
		t.Fatalf("lipgloss rendered no escape for the style: %q", probe)
	}
	return probe[a+1 : b]
}

// TestTheHeadingPinsToTheTopOnceItScrollsOff. Deep in a long hunk there is
// otherwise nothing on screen saying which hunk a mark would take.
func TestTheHeadingPinsToTheTopOnceItScrollsOff(t *testing.T) {
	m := pane(t, twoHunks, 60, 4)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down, down, down)

	got := rows(t, m)
	if !strings.Contains(got[0], "@@ -10,5") {
		t.Errorf("the top row is %q, want the cursor's hunk heading", got[0])
	}
	// The pin covers a row rather than adding one. A pane taller than its window
	// is a frame that overruns the one below it.
	if len(got) != 4 {
		t.Errorf("the pane drew %d rows, want 4", len(got))
	}
}

// TestTheCursorStepsOverTheBlankBetweenHunks, going either way. The blank is
// the pane's own spacing and there is nothing on it to put a cursor on.
func TestTheCursorStepsOverTheBlankBetweenHunks(t *testing.T) {
	m := pane(t, twoHunks, 60, 20)
	m.Select(store.SideHead, 13)

	// Row six is the first hunk's last line, seven the blank, eight the heading.
	// Read live: the row gains a caret as the cursor arrives on it.
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

// TestThePinFollowsTheWindowAndNotTheCursor. A heading names the lines under
// it, so pinning the cursor's would label them with a hunk they are not in.
func TestThePinFollowsTheWindowAndNotTheCursor(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 124)

	// zb drops the second hunk's heading to the bottom row, so the window shows
	// the tail of the first hunk with the cursor still in the second.
	m = press(t, m, tea.KeyPressMsg{Code: 'z', Text: "z"}, tea.KeyPressMsg{Code: 'b', Text: "b"})

	got := rows(t, m)
	if !strings.Contains(got[0], "@@ -10,5") {
		t.Errorf("the top row is %q, want the heading the rows under it belong to", got[0])
	}
	if !strings.Contains(got[7], "@@ -120,5") || !strings.Contains(got[7], mark) {
		t.Errorf("the cursor's own heading is not on screen with its caret: %q", got[7])
	}
}

// TestTheHeadingIsNotDrawnTwiceWhileItIsOnScreen, which is what a pin reading
// the cursor without reading the window would do.
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

// TestZPlacesTheCursorInTheWindow. These move the window under the cursor
// rather than the cursor through the file, which is why they are not ring keys.
func TestZPlacesTheCursorInTheWindow(t *testing.T) {
	z := tea.KeyPressMsg{Code: 'z', Text: "z"}

	// The second heading, row eight of sixteen over an eight-row window: the one
	// place all three have somewhere to go, since the window clamps at both ends.
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

// TestZOnItsOwnDoesNothing, and spends the key after it rather than leaving the
// pane armed for a press the reader has forgotten about.
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

// TestAShorterTerminalKeepsTheCursorOnScreen. A window that only clamps leaves
// the cursor below it, and a mark then takes a hunk nothing on screen names.
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

// TestAFileWithNoHunksLandsUnderTheCursor. It is one stop on the ring like any
// other, and a pane with no fill on it reads as a pane the ring skipped.
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

// TestRestorePutsTheCursorBackWithTheWindow. Landing takes the reader to the
// heading, and a reload that changed nothing owes them their own row back.
func TestRestorePutsTheCursorBackWithTheWindow(t *testing.T) {
	c := testchangeset.Nested(t)

	m := pane(t, twoHunks, 60, 8)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down, down)

	at, off := m.Cursor(), m.Scroll().Offset

	// What a reload of the same bytes does: the file back in the pane, the
	// cursor on the heading, then the reader's own place handed back.
	m.SetFile(fileAt(t, c, twoHunks), nil, 2)
	m.Select(store.SideHead, 13)
	m.Restore(at, off)

	if want := strings.TrimRight(rows(t, m)[at-off], " "); filled(t, m) != want {
		t.Errorf("the cursor came back on %q, want %q", filled(t, m), want)
	}
	if got := m.Scroll().Offset; got != off {
		t.Errorf("the window came back at %d, want %d", got, off)
	}
}

// cards is the fixture's comments, which every card assertion drives.
func cards(t *testing.T, path string, width, height int) diffpane.Model {
	t.Helper()
	return commented(t, path, width, height, testchangeset.NestedComments()...)
}

// under is the row after the one containing want, which is where a card hangs.
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

// TestACardHangsUnderTheLineItAnswers. A comment about a line the reader cannot
// see beside it is an assertion about nothing.
func TestACardHangsUnderTheLineItAnswers(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)

	if got := under(t, m, `Unreviewed State = "unreviewed"`); !strings.Contains(got, "◇ open") {
		t.Errorf("the row under the line it answers is %q, want the card", got)
	}
	if got := joined(t, m); !strings.Contains(got, "unreviewed is the longer word") {
		t.Errorf("the card drew no body:\n%s", got)
	}
}

// TestAResponseHangsOffTheCardOnARail. A box says the words below are not the
// reader's, where a change of weight inside one border says only that somebody
// trailed off.
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

	// The card's own two borders and its one row of body, then the rail: down
	// past the box's top border, into the elbow on its first row of words.
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

// TestTheResponseBoxIsTwoColumnsInsideTheCard. The rail is drawn in the gap, so
// a box the card's own width has nowhere to hang from.
func TestTheResponseBoxIsTwoColumnsInsideTheCard(t *testing.T) {
	got := rows(t, cards(t, twoHunks, 76, 60))

	// Measured in cells, not bytes: the rail's own glyphs are three bytes each.
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

// TestTheResponseBoxNeverLights. A lit border says a key reaches here, and
// nothing reaches a response: no cursor stops on it and there is nothing to do
// on it. A stripped frame cannot see a colour, so this reads the escapes.
func TestTheResponseBoxNeverLights(t *testing.T) {
	m := cards(t, twoHunks, 76, 60)
	accent := params(t, lipgloss.NewStyle().Foreground(theme.RosePineMoon.Accent))

	for range 60 {
		m = press(t, m, down)

		flat, raw := rows(t, m), strings.Split(m.View(), "\n")
		for i, row := range flat {
			// The addressed card is the one with a box under it, and the row is
			// its footer, which only a lit card draws.
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

// TestACommentWithNoResponseDrawsNoBox. Every state but addressed reaches the
// card with nothing to say, and an empty box claims words it has none of.
func TestACommentWithNoResponseDrawsNoBox(t *testing.T) {
	got := joined(t, commented(t, "README.md", 76, 30,
		testchangeset.Comment("aaaaaaaaaaaa", "README.md", 0, 0, "Does this still read right?")))

	if strings.Contains(got, "response") {
		t.Errorf("a comment with no response drew a box for it:\n%s", got)
	}
}

// TestAFoldedCardTakesItsResponseWithIt. One row is what folding means, and a
// box still hanging off it says the card is open.
func TestAFoldedCardTakesItsResponseWithIt(t *testing.T) {
	settled := testchangeset.Responded(
		testchangeset.In(testchangeset.Comment("aaaaaaaaaaaa", "README.md", 1, 1, "Does this read right?"),
			store.CommentResolved), "It reads fine now.")

	got := joined(t, commented(t, "README.md", 76, 30, settled))
	if strings.Contains(got, "\u256d\u2500 response") {
		t.Errorf("a folded card kept its response box:\n%s", got)
	}
}

// TestARangeCardSaysWhereItStarted. It hangs under the last line of the run, and
// nothing on that row can say the run began two lines above.
func TestARangeCardSaysWhereItStarted(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)

	got := under(t, m, "// Never off the working tree.")
	if !strings.Contains(got, "lines 124-125") {
		t.Errorf("the range card reads %q, want the run it covers", got)
	}
}

// TestACardUnderItsOwnLineSaysNoNumber. The gutter beside it already has one,
// and a card repeating it is a label saying what the reader can see.
func TestACardUnderItsOwnLineSaysNoNumber(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)

	got := under(t, m, `Unreviewed State = "unreviewed"`)
	if strings.Contains(got, "13") {
		t.Errorf("the card names its own line: %q", got)
	}
}

// TestAFileCommentHeadsTheFile. It names the whole file rather than a line in
// it, the way a whole-file reviewed range covers one.
func TestAFileCommentHeadsTheFile(t *testing.T) {
	got := rows(t, cards(t, "README.md", 76, 20))

	if !strings.Contains(got[0], "◇ open · file") {
		t.Errorf("the first row is %q, want the file's own comment", got[0])
	}
	if !strings.Contains(got[1], "Does this still read right?") {
		t.Errorf("the card drew no body: %q", got[1])
	}
}

// TestACommentTheDiffHasNoLineForStillDraws, and says so rather than hanging
// under a line it was never about. Dropping it loses what was asked.
func TestACommentTheDiffHasNoLineForStillDraws(t *testing.T) {
	got := joined(t, cards(t, twoHunks, 76, 30))

	if !strings.Contains(got, "✕ orphaned · was line 900") {
		t.Errorf("the stray did not draw, or drew as though it were placed:\n%s", got)
	}
}

// TestAResolvedCommentIsOneRowUntilSpaceOpensIt. Settled work burying live work
// is what folding is for, and a fold with no way back loses a mistaken resolve.
func TestAResolvedCommentIsOneRowUntilSpaceOpensIt(t *testing.T) {
	m := cards(t, "README.md", 76, 20)

	// It keeps its box. Without one it is a line of grey text in a column of
	// diff, which is what the diff's own notes look like.
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

	// Onto its row, then open it. The file card is the first stop, the heading
	// and the five lines the next six, and the resolved card the one after.
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

// boxHeight is how many rows a card spans, found from the border its label is
// in down to the one that closes it, and 0 when the label is not on screen.
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

// TestEveryCardRowIsExactlyThePane, at widths where a card loses its border. A
// pane clips silently and a width test on the unclipped row still passes.
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

// TestTheScrollCounterCountsCardRows. add appends one entry per call and every
// offset assumes row equals line, so one multi-line push and the counter lies.
//
// The pane is deep enough to draw every row, because the ceiling is what the
// pane drew and a counter over a clipped view proves nothing.
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

// TestJStepsOverACardInOnePress. A card is one block and one stop; walking its
// border and its prose a row at a time is a tax on the burn-down.
func TestJStepsOverACardInOnePress(t *testing.T) {
	m := cards(t, "README.md", 76, 20)

	m = press(t, m, down)
	if got := m.Cursor(); got != 0 {
		t.Fatalf("the first j landed on row %d, want the card's own row", got)
	}

	// Three rows of card, and the next press clears all of them.
	m = press(t, m, down)
	if got := m.Cursor(); got != 3 {
		t.Errorf("the next j landed on row %d, want the row after the card", got)
	}

	m = press(t, m, up)
	if got := m.Cursor(); got != 0 {
		t.Errorf("k landed on row %d, want the card's own row again", got)
	}
}

// TestACardTakesTheAccentBorderUnderTheCursor. A stripped golden cannot see a
// colour, and the border is the only thing saying where the keys are.
func TestACardTakesTheAccentBorderUnderTheCursor(t *testing.T) {
	m := cards(t, "README.md", 76, 20)
	accent := params(t, lipgloss.NewStyle().Foreground(theme.RosePineMoon.Accent))

	// The bottom border, because the top one carries the badge and an open
	// comment's badge is the accent whether the cursor is on it or not.
	if lit(m.View(), accent, "╰─") {
		t.Errorf("the card is lit with the cursor off it:\n%s", joined(t, m))
	}
	if m = press(t, m, down); !lit(m.View(), accent, "╰─") {
		t.Errorf("the card took no accent with the cursor on it:\n%s", joined(t, m))
	}
}

// lit is whether a row holding want carries the escape parameters.
func lit(view, params, want string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), want) && strings.Contains(line, params) {
			return true
		}
	}
	return false
}

// TestSelectCommentLeavesTheAnchorOnScreen. A block that answers the line above
// it cannot go to the top row, or the code it answers scrolls away.
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

// TestEnterGoesToTheLineACardAnswers, which on a range is where the run starts
// rather than the line the card happens to hang under.
func TestEnterGoesToTheLineACardAnswers(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)
	m.SelectComment("cccccccccccc")
	m = press(t, m, enter)

	if got := filled(t, m); !strings.Contains(got, "// Ranges are read off the generation.") {
		t.Errorf("enter left the cursor on %q, want the run's first line", got)
	}
}

// TestAResizeKeepsTheCursorOnTheSameCard. A card's height moves with the width,
// so every row index after one moves with it and a stored row is the wrong card.
func TestAResizeKeepsTheCursorOnTheSameCard(t *testing.T) {
	m := cards(t, twoHunks, 76, 30)
	m.SelectComment("cccccccccccc")
	was := m.Cursor()

	m.SetSize(34, 30)
	if got, ok := m.Comment(); !ok || got != "cccccccccccc" {
		t.Errorf("the resize left the cursor on %q, want the card it was on", got)
	}

	// Without this the test passes on a row that never moved, which proves
	// nothing about carrying the cursor over a relayout.
	if m.Cursor() == was {
		t.Fatalf("the card is on row %d at both widths, so the narrower one wrapped nothing", was)
	}
}

// TestABaseSideCommentMatchesThroughTheOldPath. A rename gives the file a name
// the base side never had, and matching on the new one loses the comment.
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

	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(76, 10)
	m.SetFile(&c.Files[0], []store.Comment{old}, 2)

	got := joined(t, m)
	if !strings.Contains(got, "python is gone") {
		t.Fatalf("the base-side comment did not draw:\n%s", got)
	}
	if strings.Contains(got, "was line") {
		t.Errorf("it drew as a stray rather than under the line it removed:\n%s", got)
	}
}

// TestEveryBadgeIsOneCell. A two-cell glyph puts every row after it out of step
// where a font missing a one-cell one only draws a box, so this is measured
// rather than assumed.
func TestEveryBadgeIsOneCell(t *testing.T) {
	for _, glyph := range []string{"◇", "◈", "◆", "✕", "▸", mark} {
		if got := lipgloss.Width(glyph); got != 1 {
			t.Errorf("%q measures %d cells", glyph, got)
		}
	}
}

// TestACommentFrozenAtAnOlderGenerationDrawsWhereItPointed. It stopped moving
// and kept the anchor it stopped at, so those line numbers now name whatever is
// there rather than the code it was about.
func TestACommentFrozenAtAnOlderGenerationDrawsWhereItPointed(t *testing.T) {
	stale := testchangeset.In(
		testchangeset.Comment("aaaaaaaaaaaa", twoHunks, 13, 13, "this was about the old line"),
		store.CommentResolved)
	stale.GenerationID = 1

	c := testchangeset.Nested(t)
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(76, 30)
	m.SetFile(fileAt(t, c, twoHunks), []store.Comment{stale}, 2)

	if got := joined(t, m); !strings.Contains(got, "was line 13") {
		t.Errorf("the frozen comment drew as though it still pointed at line 13:\n%s", got)
	}
}

// TestACommentFrozenAtThisGenerationStaysWhereItIs, so resolving one does not
// make its card jump to the foot of the file under the reader.
func TestACommentFrozenAtThisGenerationStaysWhereItIs(t *testing.T) {
	settled := testchangeset.In(
		testchangeset.Comment("aaaaaaaaaaaa", twoHunks, 13, 13, "just settled"),
		store.CommentResolved)

	c := testchangeset.Nested(t)
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(76, 30)
	m.SetFile(fileAt(t, c, twoHunks), []store.Comment{settled}, 2)

	if got := joined(t, m); strings.Contains(got, "was line") {
		t.Errorf("a comment frozen at the generation on screen drew as a stray:\n%s", got)
	}
}

// TestACardKeepsTheParagraphsOfItsBody. comp.Safe reads a newline as the control
// character it is, so sanitizing a whole body first collapses it into one run.
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

// TestSelectCommentLeavesRoomForThePin. The heading pins to the top row, so an
// anchor put there is covered by the thing meant to keep it in context.
func TestSelectCommentLeavesRoomForThePin(t *testing.T) {
	// Four rows: a taller window is clamped off the anchor by the card's own
	// last row and never lands the offset on it.
	m := cards(t, twoHunks, 76, 4)
	m.SelectComment("bbbbbbbbbbbb")

	if got := joined(t, m); !strings.Contains(got, `Unreviewed State = "unreviewed"`) {
		t.Errorf("the pinned heading covered the line the card answers:\n%s", got)
	}
}

// TestTheHeadingHoldsThroughAPagingKey. ctrl+d moves the cursor and the window
// by the same amount, so a pin that stood down for a cursor on the top row
// stood down on every press and the rows on screen went unlabelled.
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

// TestThePinDoesNotCoverTheCursor. It owns the top line, so the window opens a
// row higher rather than drawing the heading over the row the reader is on.
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

// TestZtLandsUnderTheHeadingItAsksToSee. The top row is the pin's, so a cursor
// put there is drawn over by the very heading zt was pressed to keep in view.
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

// TestAPagingKeyParksTheCursorMidWindow, so the file runs past a cursor that
// stays put and the eye keeps one place to read from.
//
// Three phases: the cursor reaches the middle without the window moving, then
// the window carries it, then the window stops and it goes on to the last row.
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

// TestAPagingKeyParksGoingUpToo, and lands the cursor on the first row once the
// window has run out of file above it.
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

// TestAOneRowPaneKeepsTheCursorOverThePin. The pane gets exactly one row at the
// app's own minimum height, and there the pin and the cursor want the same line.
// The heading is a label; a reader who cannot see their own row has lost more.
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
