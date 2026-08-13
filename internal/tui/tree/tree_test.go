package tree_test

import (
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

func rows(t *testing.T, m tree.Model) []string {
	t.Helper()
	return strings.Split(ansi.Strip(m.View()), "\n")
}

func press(t *testing.T, m tree.Model, keys ...string) (tree.Model, tea.Msg) {
	t.Helper()

	var last tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		last = nil
		if cmd != nil {
			last = cmd()
		}
	}
	return m, last
}

// TestADirectoryChainIsOneRow. A directory holding one directory and nothing
// else costs a row and says nothing, and four of them in a line push every
// file off the pane.
func TestADirectoryChainIsOneRow(t *testing.T) {
	got := rows(t, pane(t, 32, 20))

	want := []string{
		"· README.md",
		"▾ assets",
		"· logo.png",
		"▾ docs/superpowers/specs",
		"✓ design.md",
		"▾ internal",
		"▾ cli",
		"· render.go",
		"▾ review",
		"~ state.go",
		"▾ tui/diffpane",
	}

	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("row %d is %q, want it to hold %q", i, got[i], w)
		}
	}
	if strings.Contains(strings.Join(got, "\n"), "▾ docs\n") {
		t.Errorf("docs kept a row of its own:\n%s", strings.Join(got, "\n"))
	}
}

// TestFoldingKeepsTheCursorWhereItWas. Folding removes rows below the one under
// the cursor, so the index it sits at still names the same row.
func TestFoldingKeepsTheCursorWhereItWas(t *testing.T) {
	m := pane(t, 32, 20)
	m, _ = press(t, m, "j", "j", "j", "j", "j")

	before := rows(t, m)[5]
	if !strings.Contains(before, "▾ internal") {
		t.Fatalf("the cursor is not on internal: %q", before)
	}

	m, _ = press(t, m, " ")
	after := rows(t, m)

	if !strings.Contains(after[5], "▸ internal") {
		t.Errorf("space did not fold internal: %q", after[5])
	}
	if !strings.Contains(after[5], "▎") {
		t.Errorf("the cursor left the row it folded: %q", after[5])
	}
	if joined := strings.Join(after, "\n"); strings.Contains(joined, "state.go") {
		t.Errorf("a folded directory kept its children on screen:\n%s", joined)
	}
}

// TestWalkingSaysWhereItLanded. The diff pane follows a file and ignores a
// directory, because blanking the pane on the way past one would punish
// walking the tree.
func TestWalkingSaysWhereItLanded(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want tea.Msg
	}{
		{"onto a directory", []string{"j"}, nil},
		{"onto a file", []string{"j", "j"}, tree.SelectedMsg{Path: "assets/logo.png"}},
		{"off the end", []string{"k"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := press(t, pane(t, 32, 20), tt.keys...)
			if got != tt.want {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestEnterOpensAFileAndFoldsADirectory. Enter reads as "go there", and on a
// directory that is the fold.
func TestEnterOpensAFileAndFoldsADirectory(t *testing.T) {
	m := pane(t, 32, 20)

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("enter on a file returned nothing")
	}
	if got := cmd(); got != (tree.OpenMsg{Path: "README.md"}) {
		t.Errorf("got %#v, want an OpenMsg for README.md", got)
	}

	m, _ = press(t, m, "j")
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("enter on a directory asked for a file to be opened")
	}
	if got := rows(t, m)[1]; !strings.Contains(got, "▸ assets") {
		t.Errorf("enter did not fold the directory: %q", got)
	}
}

// TestSelectReachesIntoAFoldedDirectory, so a selection made from outside the
// pane has a row to land on.
func TestSelectReachesIntoAFoldedDirectory(t *testing.T) {
	m := pane(t, 32, 20)
	m, _ = press(t, m, "j", "j", "j", "j", "j", " ")

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
		for i, row := range rows(t, m) {
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
