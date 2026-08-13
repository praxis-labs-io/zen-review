package diffpane_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/testchangeset"
	"github.com/zen-review/zen-review/internal/tui/diffpane"
)

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

func press(t *testing.T, m diffpane.Model, keys ...tea.KeyPressMsg) diffpane.Model {
	t.Helper()

	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

// TestAFileIsItsHunkHeaders. This is the folded view the painter unfolds, not a
// placeholder, so it goes through the same painter.
func TestAFileIsItsHunkHeaders(t *testing.T) {
	got := rows(t, pane(t, "internal/review/state.go", 60, 10))

	want := []string{"@@ -10,5 +10,5 @@ package review", "@@ -120,5 +120,7 @@"}
	for i, w := range want {
		found := false
		for _, row := range got {
			if strings.Contains(row, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no row holds header %d, %q:\n%s", i, w, strings.Join(got, "\n"))
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

// TestEveryRowIsExactlyThePane, at widths where a hunk header cannot fit. A
// pane clips overflow silently, losing trailing cells mid-cell with no
// ellipsis, and a width test on the unclipped row still passes.
func TestEveryRowIsExactlyThePane(t *testing.T) {
	for _, width := range []int{80, 40, 24, 12} {
		m := pane(t, "internal/review/state.go", width, 10)
		for i, row := range rows(t, m) {
			if got := lipgloss.Width(row); got != width {
				t.Errorf("at width %d, row %d is %d columns: %q", width, i, got, row)
			}
		}
	}
}

// TestALongHeaderIsMarkedWhereItWasCut, so the reader can tell a clipped header
// from a short one.
func TestALongHeaderIsMarkedWhereItWasCut(t *testing.T) {
	m := pane(t, "internal/review/state.go", 40, 10)

	joined := strings.Join(rows(t, m), "\n")
	if !strings.Contains(joined, "…") {
		t.Errorf("a header wider than the pane was cut without a mark:\n%s", joined)
	}
}

// TestScrollingStopsAtBothEnds, so a key held down cannot walk the content off
// the pane.
func TestScrollingStopsAtBothEnds(t *testing.T) {
	down := tea.KeyPressMsg{Code: 'j', Text: "j"}
	up := tea.KeyPressMsg{Code: 'k', Text: "k"}

	// Two hunks with a blank line between them is three lines, in a pane with
	// room for two.
	m := pane(t, "internal/review/state.go", 60, 2)

	top := rows(t, m)
	if !strings.Contains(top[0], "@@ -10,5") {
		t.Fatalf("the pane did not open at the top: %q", top[0])
	}

	m = press(t, m, down, down, down, down)
	if got := rows(t, m); !strings.Contains(got[1], "@@ -120,5") {
		t.Errorf("scrolling down ran past the last line: %q", got)
	}

	m = press(t, m, up, up, up, up)
	if got := rows(t, m); !strings.Contains(got[0], "@@ -10,5") {
		t.Errorf("scrolling up ran past the first line: %q", got)
	}
}

// TestChangingFileTakesTheReaderToTheTop. A pane left at the old offset opens a
// new file part-way down it.
func TestChangingFileTakesTheReaderToTheTop(t *testing.T) {
	c := testchangeset.Nested(t)

	m := pane(t, "internal/review/state.go", 60, 2)
	m = press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"}, tea.KeyPressMsg{Code: 'j', Text: "j"})

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
	if indent := len(got) - len(strings.TrimLeft(got, " ")); indent != 9 {
		t.Errorf("the header indents %d columns, want 9: %q", indent, got)
	}
}
