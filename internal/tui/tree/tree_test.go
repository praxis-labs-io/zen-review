package tree_test

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/testchangeset"
	"github.com/zen-review/zen-review/internal/tui/tree"
)

func pane(t *testing.T, width, height int) tree.Model {
	t.Helper()

	m := tree.New(theme.RosePineMoon, testchangeset.Nested(t))
	m.SetSize(width, height)
	m.Focus()
	return m
}

// lines is every line the pane drew, the blank pads at the ends included.
func lines(t *testing.T, m tree.Model) []string {
	t.Helper()
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// rows drops the pad above the list, so a test indexes the tree the way it
// reads. Every caller sits at the top of a list that fits.
func rows(t *testing.T, m tree.Model) []string {
	t.Helper()
	return lines(t, m)[topPadLines:]
}

// topPadLines mirrors the pane's own blank line above the first row.
const topPadLines = 1

// fill is the SGR parameters lipgloss writes for a background colour.
//
// The cursor row is a fill and nothing else, so a test looks for the colour
// inside whatever style the row is wearing. A stripped frame cannot see it, and
// there is no glyph left to look for.
func fill(c color.Color) string {
	rendered := lipgloss.NewStyle().Background(c).Render("x")
	return rendered[len("\x1b["):strings.Index(rendered, "m")]
}

// cursored reports whether a row is the one under the cursor.
func cursored(t *testing.T, m tree.Model, i int) bool {
	t.Helper()
	raw := strings.Split(m.View(), "\n")[topPadLines:]
	return strings.Contains(raw[i], fill(theme.RosePineMoon.SelectedBackground))
}

func press(t *testing.T, m tree.Model, keys ...string) (tree.Model, tea.Cmd) {
	t.Helper()

	var last tea.Cmd
	for _, k := range keys {
		m, last = m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
	}
	return m, last
}

// TestADirectoryChainIsOneRow. A directory holding one directory and nothing
// else costs a row and says nothing, and four of them in a line push every
// file off the pane.
func TestADirectoryChainIsOneRow(t *testing.T) {
	got := rows(t, pane(t, 32, 20))

	want := []string{
		" assets",
		"○ logo.png",
		" docs/superpowers/specs",
		"● design.md",
		" internal",
		" cli",
		"○ render.go",
		" review",
		"⊙ state.go",
		" tui/diffpane",
		"○ painting_the_unif",
		"○ README.md",
	}

	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("row %d is %q, want it to hold %q", i, got[i], w)
		}
	}
	if strings.Contains(strings.Join(got, "\n"), " docs\n") {
		t.Errorf("docs kept a row of its own:\n%s", strings.Join(got, "\n"))
	}
}

// TestALevelSortsLikeAFileTree: directories above files, by byte within each
// group, which is zen-octo's rule. Git's order is the order it walked the index
// in, which drops a root file above every directory holding the rest.
func TestALevelSortsLikeAFileTree(t *testing.T) {
	// Deliberately the wrong way round in the patch, so a tree that kept git's
	// order fails every line of this.
	patch := ""
	for _, p := range []string{
		"go.sum", "CLAUDE.md", ".gitignore",
		"internal/b.go", "cmd/a.go", ".github/workflows/ci.yml",
	} {
		patch += "diff --git a/" + p + " b/" + p + "\n" +
			"--- a/" + p + "\n+++ b/" + p + "\n@@ -1 +1 @@\n-old\n+new\n"
	}

	m := tree.New(theme.RosePineMoon, testchangeset.Derive(t, patch))
	m.SetSize(40, 20)

	// Byte order does the work: "." sorts below every letter and upper case
	// below lower, so the dotted names lead each group with no rule of their own.
	want := []string{
		".github/workflows", "ci.yml",
		"cmd", "a.go",
		"internal", "b.go",
		".gitignore",
		"CLAUDE.md",
		"go.sum",
	}

	got := rows(t, m)
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("row %d is %q, want it to hold %q", i, got[i], w)
		}
	}
}

// TestTheCursorSurvivesATerminalWithoutColour. The row is a filled background,
// and a terminal that drops colour drops the fill with it: the reader presses j
// and watches nothing move. Bold is not a colour and comes through.
func TestTheCursorSurvivesATerminalWithoutColour(t *testing.T) {
	th := theme.RosePineMoon
	m, _ := press(t, pane(t, 32, 20), "j")

	on := lipgloss.NewStyle().
		Background(th.SelectedBackground).Foreground(th.Text).Bold(true).Render("logo.png")
	off := lipgloss.NewStyle().Foreground(th.Text).Bold(true).Render("README.md")

	view := m.View()
	if !strings.Contains(view, on) {
		t.Errorf("the row under the cursor is not bold, so a terminal without colour shows no cursor")
	}
	if strings.Contains(view, off) {
		t.Errorf("a row the cursor is not on is bold")
	}
}

// TestOneRowShowsTheRowAndNotAPad. The pads are the ends of the list, and a
// pane with a single line has no room for a pad and the row it belongs to.
// Snapping to one there draws an empty tree on the smallest terminal that runs.
func TestOneRowShowsTheRowAndNotAPad(t *testing.T) {
	const patch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,0 +1,1 @@
+one
`

	m := tree.New(theme.RosePineMoon, testchangeset.Derive(t, patch))
	m.SetSize(32, 1)
	m.Focus()

	got := strings.Split(ansi.Strip(m.View()), "\n")
	if len(got) != 1 {
		t.Fatalf("a pane one line high drew %d lines", len(got))
	}
	if !strings.Contains(got[0], "a.go") {
		t.Errorf("the one line is %q, want the file on it", got[0])
	}
}

// TestFoldingKeepsTheCursorWhereItWas. Folding removes rows below the one under
// the cursor, so the index it sits at still names the same row.
func TestFoldingKeepsTheCursorWhereItWas(t *testing.T) {
	m := pane(t, 32, 20)
	m, _ = press(t, m, "j", "j", "j", "j")

	before := rows(t, m)[4]
	if !strings.Contains(before, " internal") {
		t.Fatalf("the cursor is not on internal: %q", before)
	}

	m, _ = press(t, m, " ")
	after := rows(t, m)

	if !strings.Contains(after[4], " internal") {
		t.Errorf("space did not fold internal: %q", after[4])
	}
	if !cursored(t, m, 4) {
		t.Errorf("the cursor left the row it folded: %q", after[4])
	}
	if joined := strings.Join(after, "\n"); strings.Contains(joined, "state.go") {
		t.Errorf("a folded directory kept its children on screen:\n%s", joined)
	}
}

// TestWalkingSaysWhereItLanded. The path is read off the model rather than sent
// in a message, so the root cannot be handed a stale one by two commands that
// landed out of order.
func TestWalkingSaysWhereItLanded(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{"onto a file", []string{"j"}, "assets/logo.png"},
		{"onto a directory", []string{"j", "j"}, ""},
		{"off the end", []string{"k"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, cmd := press(t, pane(t, 32, 20), tt.keys...)
			if got := m.Path(); got != tt.want {
				t.Errorf("the cursor is on %q, want %q", got, tt.want)
			}
			if cmd != nil {
				t.Errorf("walking the tree returned a command, which cannot be ordered")
			}
		})
	}
}

// TestEnterOpensAFileAndFoldsADirectory. Enter reads as "go there", and on a
// directory that is the fold.
func TestEnterOpensAFileAndFoldsADirectory(t *testing.T) {
	m := pane(t, 32, 20)

	m, _ = press(t, m, "j")
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("enter on a file returned nothing")
	}
	if got := cmd(); got != (tree.OpenMsg{}) {
		t.Errorf("got %#v, want an OpenMsg", got)
	}
	if got := m.Path(); got != "assets/logo.png" {
		t.Errorf("the root would open %q", got)
	}

	m, _ = press(t, m, "k")
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("enter on a directory asked for a file to be opened")
	}
	if got := rows(t, m)[0]; !strings.Contains(got, " assets") {
		t.Errorf("enter did not fold the directory: %q", got)
	}
}

// TestSelectReachesIntoAFoldedDirectory, so a selection made from outside the
// pane has a row to land on.
func TestSelectReachesIntoAFoldedDirectory(t *testing.T) {
	m := pane(t, 32, 20)
	m, _ = press(t, m, "j", "j", "j", "j", " ")

	if joined := strings.Join(rows(t, m), "\n"); strings.Contains(joined, "state.go") {
		t.Fatalf("internal did not fold:\n%s", joined)
	}

	if !m.Select("internal/review/state.go") {
		t.Fatalf("Select did not find a path the changeset holds")
	}
	if got := m.Path(); got != "internal/review/state.go" {
		t.Errorf("the cursor is on %q", got)
	}
	if m.Select("nowhere/at/all.go") {
		t.Errorf("Select claimed to find a path the changeset does not hold")
	}
}

// TestEveryRowIsExactlyThePane, at a width where the longest path cannot fit.
// A pane clips overflow silently, so a row that overran would look tidy in a
// golden and lose its trailing cells on the screen.
func TestEveryRowIsExactlyThePane(t *testing.T) {
	for _, width := range []int{32, 24, 16, 8} {
		m := pane(t, width, 20)
		for i, row := range lines(t, m) {
			if got := lipgloss.Width(row); got != width {
				t.Errorf("at width %d, row %d is %d columns: %q", width, i, got, row)
			}
		}
	}
}

// TestTheChurnSurvivesANarrowPane. A clipped "+12 -3" misstates the file, and a
// clipped path still names it, so the name is what gives up the columns.
func TestTheChurnSurvivesANarrowPane(t *testing.T) {
	m := pane(t, 24, 20)
	for _, row := range rows(t, m) {
		if !strings.Contains(row, "state.go") {
			continue
		}
		if !strings.HasSuffix(strings.TrimRight(row, " "), "+3 -1") {
			t.Errorf("the churn did not survive the clip: %q", row)
		}
		return
	}
	t.Fatalf("no row holds state.go")
}

// TestALongOmissionReasonDoesNotEatTheFilename. "renamed, contents unchanged"
// is 27 columns, and a 32-column pane spending all of them on the reason names
// no file at all: two renames become the same row.
func TestALongOmissionReasonDoesNotEatTheFilename(t *testing.T) {
	const patch = `diff --git a/old_name_here.go b/new_name_here.go
similarity index 100%
rename from old_name_here.go
rename to new_name_here.go
`

	m := tree.New(theme.RosePineMoon, testchangeset.Derive(t, patch))
	m.SetSize(32, 4)
	m.Focus()

	got := rows(t, m)[0]
	if !strings.Contains(got, "new_name") {
		t.Errorf("the reason took the filename's columns: %q", got)
	}
	if w := lipgloss.Width(got); w != 32 {
		t.Errorf("the row is %d columns, want 32: %q", w, got)
	}
}

// TestAControlCharacterInAPathCannotBreakTheRow. Git allows a newline and an
// escape sequence in a filename, and the parser unquotes both. A newline
// splits the row in two and puts the other pane out of step with it; an escape
// is run by the terminal.
func TestAControlCharacterInAPathCannotBreakTheRow(t *testing.T) {
	const patch = "diff --git \"a/we\\ntwo.go\" \"b/we\\ntwo.go\"\n" +
		"new file mode 100644\n" +
		"index 0000000000000000000000000000000000000000..66a52ee7a1d803dc57859c3e95ac9dcdc87c0164\n" +
		"--- /dev/null\n" +
		"+++ \"b/we\\ntwo.go\"\n" +
		"@@ -0,0 +1 @@\n" +
		"+package two\n"

	m := tree.New(theme.RosePineMoon, testchangeset.Derive(t, patch))
	m.SetSize(32, 4)
	m.Focus()

	view := m.View()
	if got := len(strings.Split(view, "\n")); got != 4 {
		t.Errorf("the pane drew %d lines into a height of 4:\n%q", got, view)
	}
	for i, row := range lines(t, m) {
		if w := lipgloss.Width(row); w != 32 {
			t.Errorf("row %d is %d columns: %q", i, w, row)
		}
	}
	if !strings.Contains(ansi.Strip(view), "we?two.go") {
		t.Errorf("the newline was not escaped for display:\n%q", ansi.Strip(view))
	}
}
