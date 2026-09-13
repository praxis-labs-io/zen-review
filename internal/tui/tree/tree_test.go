package tree_test

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
	"github.com/praxis-labs-io/zen-review/internal/tui/tree"
)

func pane(t *testing.T, width, height int) tree.Model {
	t.Helper()
	return themed(t, testtheme.Dark, width, height)
}

func themed(t *testing.T, th theme.Theme, width, height int) tree.Model {
	t.Helper()

	m := tree.New(th, testchangeset.Nested(t))
	m.SetSize(width, height)
	m.Focus()
	return m
}

func lines(t *testing.T, m tree.Model) []string {
	t.Helper()
	return strings.Split(ansi.Strip(m.View()), "\n")
}

func rows(t *testing.T, m tree.Model) []string {
	t.Helper()
	return lines(t, m)[topPadLines:]
}

const topPadLines = 1

func fill(c color.Color) string {
	rendered := lipgloss.NewStyle().Background(c).Render("x")
	return rendered[len("\x1b["):strings.Index(rendered, "m")]
}

func cursored(t *testing.T, m tree.Model, i int) bool {
	t.Helper()
	raw := strings.Split(m.View(), "\n")[topPadLines:]
	return strings.Contains(raw[i], fill(testtheme.Dark.SelectedBackground))
}

func press(t *testing.T, m tree.Model, keys ...string) (tree.Model, tea.Cmd) {
	t.Helper()

	var last tea.Cmd
	for _, k := range keys {
		m, last = m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
	}
	return m, last
}

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

func TestALevelSortsLikeAFileTree(t *testing.T) {
	patch := ""
	for _, p := range []string{
		"go.sum", "CLAUDE.md", ".gitignore",
		"internal/b.go", "cmd/a.go", ".github/workflows/ci.yml",
	} {
		patch += "diff --git a/" + p + " b/" + p + "\n" +
			"--- a/" + p + "\n+++ b/" + p + "\n@@ -1 +1 @@\n-old\n+new\n"
	}

	m := tree.New(testtheme.Dark, testchangeset.Derive(t, patch))
	m.SetSize(40, 20)

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

func TestTheCursorSurvivesATerminalWithoutColour(t *testing.T) {
	for _, tc := range []struct {
		name string
		th   theme.Theme
	}{
		{"palette reported", testtheme.Dark},
		{"nothing answered", testtheme.Bare},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := press(t, themed(t, tc.th, 32, 20), "j")

			on := lipgloss.NewStyle().
				Background(tc.th.SelectedBackground).Foreground(tc.th.Text).Bold(true).Render("logo.png")
			off := lipgloss.NewStyle().Foreground(tc.th.Text).Bold(true).Render("README.md")

			view := m.View()
			if !strings.Contains(view, on) {
				t.Errorf("the row under the cursor is not bold, so a terminal without colour shows no cursor")
			}
			if strings.Contains(view, off) {
				t.Errorf("a row the cursor is not on is bold")
			}
		})
	}
}

func TestOneRowShowsTheRowAndNotAPad(t *testing.T) {
	const patch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,0 +1,1 @@
+one
`

	m := tree.New(testtheme.Dark, testchangeset.Derive(t, patch))
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

func TestALongOmissionReasonDoesNotEatTheFilename(t *testing.T) {
	const patch = `diff --git a/old_name_here.go b/new_name_here.go
similarity index 100%
rename from old_name_here.go
rename to new_name_here.go
`

	m := tree.New(testtheme.Dark, testchangeset.Derive(t, patch))
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

func TestAControlCharacterInAPathCannotBreakTheRow(t *testing.T) {
	const patch = "diff --git \"a/we\\ntwo.go\" \"b/we\\ntwo.go\"\n" +
		"new file mode 100644\n" +
		"index 0000000000000000000000000000000000000000..66a52ee7a1d803dc57859c3e95ac9dcdc87c0164\n" +
		"--- /dev/null\n" +
		"+++ \"b/we\\ntwo.go\"\n" +
		"@@ -0,0 +1 @@\n" +
		"+package two\n"

	m := tree.New(testtheme.Dark, testchangeset.Derive(t, patch))
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

func TestTheTreeDrawsTheOrderItWasGiven(t *testing.T) {
	c := testchangeset.Nested(t)
	slices.Reverse(c.Files)

	m := tree.New(testtheme.Dark, c)
	m.SetSize(40, 20)

	want := []string{"README.md", "internal", "docs", "assets"}

	var got []string
	for _, r := range rows(t, m) {
		for _, w := range want {
			if strings.Contains(r, w) {
				got = append(got, w)
			}
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("the tree drew %v, want the order it was handed, %v", got, want)
	}
}

const branchedPatch = `diff --git a/docs/README.md b/docs/README.md
--- a/docs/README.md
+++ b/docs/README.md
@@ -1,0 +1,1 @@
+read me
diff --git a/docs/superpowers/specs/design.md b/docs/superpowers/specs/design.md
--- a/docs/superpowers/specs/design.md
+++ b/docs/superpowers/specs/design.md
@@ -1,0 +1,1 @@
+design
`

const prunedPatch = `diff --git a/docs/superpowers/specs/design.md b/docs/superpowers/specs/design.md
--- a/docs/superpowers/specs/design.md
+++ b/docs/superpowers/specs/design.md
@@ -1,0 +1,1 @@
+design
`

func TestARebuildKeepsWhatTheReaderFolded(t *testing.T) {
	m := pane(t, 40, 20)

	if !fold(t, &m, "assets") {
		t.Fatal("the fixture has no assets directory")
	}
	if got := rows(t, m)[1]; strings.Contains(got, "logo.png") {
		t.Fatalf("assets did not fold: %q", got)
	}

	m.SetChangeset(testchangeset.Nested(t))
	if got := rows(t, m)[1]; strings.Contains(got, "logo.png") {
		t.Errorf("the rebuild unfolded assets: %q", got)
	}
}

func TestAFoldSurvivesTheChainCollapsingUnderIt(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		fold     string
	}{
		{"the chain collapses", branchedPatch, prunedPatch, "docs"},
		{"the chain comes apart", prunedPatch, branchedPatch, "docs/superpowers/specs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tree.New(testtheme.Dark, testchangeset.Derive(t, tt.from))
			m.SetSize(40, 20)

			if !fold(t, &m, tt.fold) {
				t.Fatalf("no row named %s:\n%s", tt.fold, strings.Join(rows(t, m), "\n"))
			}
			if got := drawn(t, m); len(got) != 1 {
				t.Fatalf("%s did not fold to one row:\n%s", tt.fold, strings.Join(got, "\n"))
			}

			m.SetChangeset(testchangeset.Derive(t, tt.to))
			if got := drawn(t, m); len(got) != 1 {
				t.Errorf("the rebuild unfolded %s:\n%s", tt.fold, strings.Join(got, "\n"))
			}
		})
	}
}

func TestARebuildStaysOnTheFileTheCursorWasOn(t *testing.T) {
	m := pane(t, 40, 20)
	if !m.Select("internal/cli/render.go") {
		t.Fatal("the fixture has no internal/cli/render.go")
	}

	m.SetChangeset(testchangeset.Nested(t))
	if got := m.Path(); got != "internal/cli/render.go" {
		t.Errorf("the cursor is on %q after a rebuild", got)
	}
}

func TestARebuildOntoAShorterListDrawsRows(t *testing.T) {
	m := pane(t, 40, 6)
	m, _ = press(t, m, "G")

	m.SetChangeset(testchangeset.Derive(t, prunedPatch))
	if got := strings.TrimSpace(strings.Join(rows(t, m), "")); got == "" {
		t.Errorf("the pane drew nothing after a rebuild onto a shorter list:\n%q", lines(t, m))
	}
	for i, line := range lines(t, m) {
		if got := lipgloss.Width(line); got != 40 {
			t.Errorf("row %d is %d wide, want 40: %q", i, got, line)
		}
	}
}

func fold(t *testing.T, m *tree.Model, name string) bool {
	t.Helper()

	for i, row := range rows(t, *m) {
		if !strings.Contains(row, name+" ") && strings.TrimSpace(row) != name {
			continue
		}
		for range i {
			*m, _ = press(t, *m, "j")
		}
		*m, _ = press(t, *m, " ")
		return true
	}
	return false
}

func drawn(t *testing.T, m tree.Model) []string {
	t.Helper()

	var out []string
	for _, row := range rows(t, m) {
		if strings.TrimSpace(row) != "" {
			out = append(out, row)
		}
	}
	return out
}
