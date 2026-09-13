package diffpane_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
)

const splitRule = "│"

var selectKey = tea.KeyPressMsg{Code: 'v', Text: "v"}

const splitWide = 80

func splitting(t *testing.T, m diffpane.Model) bool {
	t.Helper()
	return strings.Contains(strings.Join(rows(t, m), "\n"), splitRule)
}

func split(t *testing.T, path string, width, height int) diffpane.Model {
	t.Helper()

	m := pane(t, path, width, height)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("the pane refused side-by-side at width %d, %d columns short", width, short)
	}
	if !splitting(t, m) {
		t.Fatal("the pane took the toggle and drew one column")
	}
	return m
}

func onto(t *testing.T, m diffpane.Model, want int) diffpane.Model {
	t.Helper()

	for m.Cursor() < want {
		was := m.Cursor()
		if m = press(t, m, down); m.Cursor() <= was {
			break
		}
	}
	if m.Cursor() != want {
		t.Fatalf("the cursor is on row %d, want %d", m.Cursor(), want)
	}
	return m
}

func paired(t *testing.T, m diffpane.Model) int {
	t.Helper()

	for i, row := range rows(t, m) {
		if strings.Contains(row, "−") && strings.Contains(row, "+") {
			return i
		}
	}
	return -1
}

func TestEverySideBySideRowIsExactlyThePane(t *testing.T) {
	for _, width := range []int{splitWide, splitWide + 1, 96, 121} {
		m := split(t, twoHunks, width, 16)

		for i, row := range rows(t, m) {
			if got := lipgloss.Width(row); got != width {
				t.Errorf("width %d: row %d is %d cells, want %d: %q", width, i, got, width, row)
			}
		}
	}
}

func TestAPairedRowCarriesBothNumbers(t *testing.T) {
	m := split(t, twoHunks, splitWide, 16)

	at := paired(t, m)
	if at < 0 {
		t.Fatalf("no row pairs a removal against an addition:\n%s", strings.Join(rows(t, m), "\n"))
	}
	found := rows(t, m)[at]

	if got := strings.Count(found, "13"); got != 2 {
		t.Errorf("want the line number in both columns, got %d in %q", got, found)
	}
}

func TestAnUnpairedChangeFacesABlank(t *testing.T) {
	const patch = `diff --git a/one.go b/one.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/one.go
+++ b/one.go
@@ -1,2 +1,4 @@
 package one
-const a = 1
+const a = 2
+const b = 3
+const c = 4
`

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(splitWide, 10)
	m.SetFile(&c.Files[0], nil, nil, 2)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused side-by-side, %d columns short", short)
	}

	var blanks int
	for _, row := range rows(t, m) {
		half, _, _ := strings.Cut(row, splitRule)
		if strings.Contains(row, "+ const b") || strings.Contains(row, "+ const c") {
			if strings.TrimSpace(half) != "" {
				t.Errorf("an unpaired addition has something in its base column: %q", row)
			}
			blanks++
		}
	}
	if blanks != 2 {
		t.Errorf("found %d unpaired additions, want the fixture's 2", blanks)
	}
}

func TestADeletionOnlyRowNamesTheBaseAlone(t *testing.T) {
	const patch = `diff --git a/gone.go b/gone.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/gone.go
+++ b/gone.go
@@ -1,3 +1,1 @@
 package gone
-const a = 1
-const b = 2
`

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(splitWide, 10)
	m.SetFile(&c.Files[0], nil, nil, 2)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused side-by-side, %d columns short", short)
	}

	if !m.Column(store.SideBase) {
		t.Fatal("the pane would not go into the base column")
	}

	m = onto(t, m, 2)
	m = press(t, m, selectKey, down)

	got, ok := m.Selected()
	if !ok {
		t.Fatal("the selection names nothing")
	}
	if len(got) != 1 || got[0].Side != store.SideBase {
		t.Fatalf("want one base-side anchor, got %+v", got)
	}
	if got[0].Range.Start != 2 || got[0].Range.End != 3 {
		t.Errorf("anchor covers %d..%d, want the two removed lines 2..3", got[0].Range.Start, got[0].Range.End)
	}
}

func TestTheCursorHoldsItsLineAcrossTheToggle(t *testing.T) {
	m := pane(t, twoHunks, splitWide, 16)

	m = press(t, m, down, down, down)
	before, ok := m.Line()
	if !ok {
		t.Fatal("the cursor is not on a line to begin with")
	}

	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused side-by-side, %d columns short", short)
	}
	after, ok := m.Line()
	if !ok {
		t.Fatal("the cursor landed on nothing after the toggle")
	}
	if before[0].Side != after[0].Side || before[0].Range.Start != after[0].Range.Start {
		t.Errorf("the cursor moved from %+v to %+v", before[0], after[0])
	}

	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused to turn side-by-side off, %d columns short", short)
	}
	back, ok := m.Line()
	if !ok {
		t.Fatal("the cursor landed on nothing coming back")
	}
	if back[0].Side != before[0].Side || back[0].Range.Start != before[0].Range.Start {
		t.Errorf("the cursor came back to %+v, want %+v", back[0], before[0])
	}
}

func TestANarrowPaneRefusesAndAWiderOneRelents(t *testing.T) {
	m := pane(t, twoHunks, 40, 16)

	if short := m.ToggleSplit(); short <= 0 {
		t.Fatal("a forty-column pane took side-by-side")
	}
	if splitting(t, m) {
		t.Error("a refused toggle drew two columns anyway")
	}

	m.SetSize(splitWide, 16)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("the widened pane still refused, %d columns short", short)
	}
	if !splitting(t, m) {
		t.Errorf("no rule between the columns:\n%s", strings.Join(rows(t, m), "\n"))
	}

	m.SetSize(40, 16)
	if splitting(t, m) {
		t.Errorf("still two columns at forty:\n%s", strings.Join(rows(t, m), "\n"))
	}

	m.SetSize(splitWide, 16)
	if !splitting(t, m) {
		t.Error("widening did not bring side-by-side back, so the answer was lost")
	}
}

func TestTwoChangeBlocksPairSeparately(t *testing.T) {
	const patch = `diff --git a/two.go b/two.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/two.go
+++ b/two.go
@@ -1,4 +1,3 @@
-const a = 1
-const b = 2
+const a = 9
-const d = 4
+const e = 5
`

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(splitWide, 10)
	m.SetFile(&c.Files[0], nil, nil, 2)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused side-by-side, %d columns short", short)
	}

	for _, row := range rows(t, m) {
		if strings.Contains(row, "const b") && strings.Contains(row, "const e") {
			t.Errorf("a removal paired with an addition from the block after it: %q", row)
		}
	}

	var facing bool
	for _, row := range rows(t, m) {
		if strings.Contains(row, "const d") && strings.Contains(row, "const e") {
			facing = true
		}
	}
	if !facing {
		t.Errorf("the second block did not pair:\n%s", strings.Join(rows(t, m), "\n"))
	}
}

func TestOnlyTheFocusedColumnLights(t *testing.T) {
	m := split(t, twoHunks, splitWide, 16)

	at := paired(t, m)
	if at < 0 {
		t.Fatalf("no paired row:\n%s", strings.Join(rows(t, m), "\n"))
	}
	m = onto(t, m, at)

	fill := params(t, lipgloss.NewStyle().Background(testtheme.Dark.SelectedBackground))
	lit := func() (string, string) {
		t.Helper()
		row := strings.Split(m.View(), "\n")[at]
		left, right, ok := strings.Cut(row, splitRule)
		if !ok {
			t.Fatalf("no rule in the cursor's row: %q", row)
		}
		return left, right
	}

	left, right := lit()
	if strings.Contains(left, fill) {
		t.Error("the base column lights with the cursor in the head")
	}
	if !strings.Contains(right, fill) {
		t.Error("the head column does not light with the cursor in it")
	}

	if !m.Column(store.SideBase) {
		t.Fatal("the pane would not go into the base column")
	}
	left, right = lit()
	if !strings.Contains(left, fill) {
		t.Error("the base column does not light with the cursor in it")
	}
	if strings.Contains(right, fill) {
		t.Error("the head column lights with the cursor in the base")
	}
}

func TestTheFocusedColumnScopesTheAnchor(t *testing.T) {
	m := split(t, twoHunks, splitWide, 16)

	at := paired(t, m)
	if at < 0 {
		t.Fatalf("no paired row:\n%s", strings.Join(rows(t, m), "\n"))
	}
	m = onto(t, m, at)

	head, ok := m.Line()
	if !ok {
		t.Fatal("the head column names nothing on a paired row")
	}
	if len(head) != 1 || head[0].Side != store.SideHead {
		t.Fatalf("want the head alone, got %+v", head)
	}

	if !m.Column(store.SideBase) {
		t.Fatal("the pane would not go into the base column")
	}
	base, ok := m.Line()
	if !ok {
		t.Fatal("the base column names nothing on a paired row")
	}
	if len(base) != 1 || base[0].Side != store.SideBase {
		t.Fatalf("want the base alone, got %+v", base)
	}
}

func TestAUnifiedRowStillNamesBothSides(t *testing.T) {
	m := pane(t, twoHunks, splitWide, 16)

	m = onto(t, m, 1)
	got, ok := m.Line()
	if !ok {
		t.Fatal("the row names nothing")
	}
	if len(got) != 2 {
		t.Errorf("want both sides off a context row, got %+v", got)
	}
}

func TestTheCursorSkipsARowItsColumnHasNoLineOn(t *testing.T) {
	const patch = `diff --git a/skip.go b/skip.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/skip.go
+++ b/skip.go
@@ -1,2 +1,4 @@
 package skip
-const a = 1
+const a = 2
+const b = 3
+const c = 4
`

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(splitWide, 10)
	m.SetFile(&c.Files[0], nil, nil, 2)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused side-by-side, %d columns short", short)
	}
	if !m.Column(store.SideBase) {
		t.Fatal("the pane would not go into the base column")
	}

	m = onto(t, m, 2)

	m = press(t, m, down)
	if got := m.Cursor(); got != 2 {
		t.Errorf("the cursor moved to row %d over rows the base column has no line on", got)
	}

	if !m.Column(store.SideHead) {
		t.Fatal("the pane would not go into the head column")
	}
	m = press(t, m, down)
	if got := m.Cursor(); got != 3 {
		t.Errorf("the cursor is on row %d, want the next addition at 3", got)
	}
}

func TestChangingColumnBringsTheCursorWithIt(t *testing.T) {
	m := split(t, twoHunks, splitWide, 16)

	at := -1
	for i, row := range rows(t, m) {
		if half, _, _ := strings.Cut(row, splitRule); strings.TrimSpace(half) == "" && strings.Contains(row, "+") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no head-only row:\n%s", strings.Join(rows(t, m), "\n"))
	}

	m = onto(t, m, at)
	if !m.Column(store.SideBase) {
		t.Fatal("the pane would not go into the base column")
	}
	if m.Cursor() == at {
		t.Fatalf("the cursor stayed on row %d, which the base column has no line on", at)
	}

	got, ok := m.Line()
	if !ok {
		t.Fatal("the cursor landed on a row naming nothing")
	}
	if got[0].Side != store.SideBase {
		t.Errorf("the cursor landed on %+v, want a row with a base line", got[0])
	}
}

const barGlyph = "▌"

func TestOnlyTheCursorRowCarriesTheBar(t *testing.T) {
	m := pane(t, twoHunks, 60, 10)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, selectKey, down, down)

	if lit := filled(t, m); len(lit) < 2 {
		t.Fatalf("the selection covers %d rows, want a run of them", len(lit))
	}

	var barred []int
	for i, row := range rows(t, m) {
		if strings.HasPrefix(row, barGlyph) {
			barred = append(barred, i)
		}
	}
	if len(barred) != 1 {
		t.Errorf("%d rows carry the bar, want the cursor's alone: %v", len(barred), barred)
	}
	if len(barred) == 1 && barred[0] != m.Cursor() {
		t.Errorf("the bar is on row %d and the cursor on %d", barred[0], m.Cursor())
	}
}

func TestTheBarSitsInTheFocusedColumn(t *testing.T) {
	m := split(t, twoHunks, splitWide, 16)
	m = onto(t, m, paired(t, m))

	head := rows(t, m)[m.Cursor()]
	if strings.HasPrefix(head, barGlyph) {
		t.Errorf("the bar is at the pane edge with the cursor in the head column: %q", head)
	}
	if left, right, _ := strings.Cut(head, splitRule); !strings.HasPrefix(right, barGlyph) || strings.Contains(left, barGlyph) {
		t.Errorf("the bar is not at the head column's edge: %q", head)
	}

	if !m.Column(store.SideBase) {
		t.Fatal("the pane would not go into the base column")
	}
	base := rows(t, m)[m.Cursor()]
	if !strings.HasPrefix(base, barGlyph) {
		t.Errorf("the bar is not at the base column's edge: %q", base)
	}
}

func TestTheToggleFromAHeadingKeepsTheHunk(t *testing.T) {
	f := fileAt(t, testchangeset.Nested(t), twoHunks)
	side, line := f.Hunks[1].Name()

	m := pane(t, twoHunks, splitWide, 16)
	m.Select(side, line)
	if _, at, ok := m.Hunk(); !ok || at != line {
		t.Fatal("the cursor is not on the second hunk's heading to begin with")
	}

	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused side-by-side, %d columns short", short)
	}
	if _, at, ok := m.Hunk(); !ok || at != line {
		t.Errorf("the toggle left the cursor on the hunk at %d, want %d", at, line)
	}
}

func TestAColumnStepStaysInTheHunk(t *testing.T) {
	const patch = `diff --git a/two.go b/two.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/two.go
+++ b/two.go
@@ -1,2 +1,3 @@
 package two
-const a = 1
+const a = 2
+const b = 3
@@ -10,2 +11,2 @@
 func f() {
-	return 1
+	return 2
`

	c := testchangeset.Derive(t, patch)
	m := diffpane.New(testtheme.Dark)
	m.SetSize(splitWide, 16)
	m.SetFile(&c.Files[0], nil, nil, 2)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("refused side-by-side, %d columns short", short)
	}

	m = onto(t, m, 3)
	_, was, ok := m.Hunk()
	if !ok {
		t.Fatal("the cursor is in no hunk to begin with")
	}

	if !m.Column(store.SideBase) {
		t.Fatal("the pane would not go into the base column")
	}
	if _, at, ok := m.Hunk(); !ok || at != was {
		t.Errorf("h took the cursor to the hunk at %d, want %d", at, was)
	}
}
