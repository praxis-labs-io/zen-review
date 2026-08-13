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
	"github.com/zen-review/zen-review/internal/testchangeset"
	"github.com/zen-review/zen-review/internal/tui/diffpane"
)

// The fixture's two-hunk file, which is what the scroll and the fold are
// measured against. Sixteen rows: a header and six lines, a blank, a header and
// seven lines.
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
	down  = tea.KeyPressMsg{Code: 'j', Text: "j"}
	up    = tea.KeyPressMsg{Code: 'k', Text: "k"}
	space = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
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

// TestSpaceFoldsTheHunkTheWindowOpensIn, leaving its header and taking its
// lines away.
func TestSpaceFoldsTheHunkTheWindowOpensIn(t *testing.T) {
	m := press(t, pane(t, twoHunks, 60, 20), space)

	got := joined(t, m)
	if strings.Contains(got, `Unreviewed State = "unread"`) {
		t.Errorf("the folded hunk kept its lines:\n%s", got)
	}
	if !strings.Contains(got, "▸ @@ -10,5") {
		t.Errorf("the folded hunk lost its header:\n%s", got)
	}
	if !strings.Contains(got, "▾ @@ -120,5") {
		t.Errorf("the hunk below it folded too:\n%s", got)
	}
	if !strings.Contains(got, "return Derive(files, rows), nil") {
		t.Errorf("the hunk below it lost its lines:\n%s", got)
	}
}

// TestFoldingAndUnfoldingLeaveTheReaderOnTheSameHunk, including on the last
// hunk of a file, where the window has to run past the end for the header it
// folded to reach the top row.
func TestFoldingAndUnfoldingLeaveTheReaderOnTheSameHunk(t *testing.T) {
	m := pane(t, twoHunks, 60, 8)
	m = press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})

	m = press(t, m, space)
	if got := rows(t, m)[0]; !strings.Contains(got, "▸ @@ -120,5") {
		t.Fatalf("the folded hunk is not on the top row: %q", got)
	}

	m = press(t, m, space)
	if got := rows(t, m)[0]; !strings.Contains(got, "▾ @@ -120,5") {
		t.Errorf("the second press acted on a different hunk: %q", got)
	}
	if got := joined(t, m); !strings.Contains(got, "return Derive(files, rows), nil") {
		t.Errorf("unfolding did not bring the lines back:\n%s", got)
	}
}

// TestAFoldSurvivesWalkingAwayFromTheFile. A fold is the reader saying they are
// done with a hunk for now, and a pane that forgets it on the way past the next
// file punishes looking at anything else.
func TestAFoldSurvivesWalkingAwayFromTheFile(t *testing.T) {
	c := testchangeset.Nested(t)

	m := pane(t, twoHunks, 60, 20)
	m = press(t, m, space)

	m.SetFile(fileAt(t, c, "README.md"))
	m.SetFile(fileAt(t, c, twoHunks))

	if got := joined(t, m); !strings.Contains(got, "▸ @@ -10,5") {
		t.Errorf("the fold was forgotten:\n%s", got)
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
	if indent := len(got) - len(strings.TrimLeft(got, " ")); indent != 9 {
		t.Errorf("the header indents %d columns, want 9: %q", indent, got)
	}
}
