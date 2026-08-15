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

	c := testchangeset.Nested(t)
	m := diffpane.New(theme.RosePineMoon)
	m.SetSize(width, height)
	m.SetFile(fileAt(t, c, path))
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
	down = tea.KeyPressMsg{Code: 'j', Text: "j"}
	up   = tea.KeyPressMsg{Code: 'k', Text: "k"}
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
	m.SetFile(&c.Files[0])

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
	m.SetFile(&c.Files[0])

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
	m.SetFile(&c.Files[0])

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

	m.SetFile(fileAt(t, c, "README.md"))
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
	m.SetFile(&c.Files[0])

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
	m.SetFile(fileAt(t, c, twoHunks))
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

	fill := params(t, lipgloss.NewStyle().Background(theme.RosePineMoon.SelectedBackground))

	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, fill) {
			return strings.TrimRight(ansi.Strip(line), " ")
		}
	}
	return ""
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
	m.SetFile(fileAt(t, c, twoHunks))
	m.Select(store.SideHead, 13)
	m.Restore(at, off)

	if want := strings.TrimRight(rows(t, m)[at-off], " "); filled(t, m) != want {
		t.Errorf("the cursor came back on %q, want %q", filled(t, m), want)
	}
	if got := m.Scroll().Offset; got != off {
		t.Errorf("the window came back at %d, want %d", got, off)
	}
}
