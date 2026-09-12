package diffpane_test

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
)

const bodyLines = 130

const tall = bodyLines + 16

func preview(t *testing.T, path string, width, height, lines int) diffpane.Model {
	t.Helper()

	m := pane(t, path, width, height)
	if !m.TogglePreview() {
		t.Fatalf("the pane refused the whole of %s", path)
	}

	want, asked := m.NeedsBody()
	if !asked || want != path {
		t.Fatalf("the pane asked for %q (asked: %v), want %s", want, asked, path)
	}
	if !m.SetBody(path, testchangeset.Body(lines)) {
		t.Fatal("the pane took the text and drew none of it")
	}
	return m
}

func rowOf(t *testing.T, m diffpane.Model, text string) int {
	t.Helper()

	at := -1
	for i, row := range rows(t, m) {
		if !strings.Contains(row, text) {
			continue
		}
		if at >= 0 {
			t.Fatalf("two rows read %q: %d and %d", text, at, i)
		}
		at = i
	}
	if at < 0 {
		t.Fatalf("no row reads %q:\n%s", text, joined(t, m))
	}
	return at
}

func TestPreviewFillsTheGapsWithTheFile(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	if got := rowOf(t, m, "line 1 of the file"); got != 0 {
		t.Errorf("the file opens on row %d, want 0", got)
	}

	head := rowOf(t, m, "@@ -10,5 +10,5 @@")
	if last := rowOf(t, m, "line 9 of the file"); last != head-2 {
		t.Errorf("line 9 is on row %d and the first heading on %d, want a row between",
			last, head)
	}

	for _, line := range []string{"line 15 of the file", "line 119 of the file", "line 130 of the file"} {
		rowOf(t, m, line)
	}
}

func TestPreviewNumbersBothSides(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	if row := rows(t, m)[rowOf(t, m, "line 1 of the file")]; !strings.Contains(row, "  1   1 ") {
		t.Errorf("line 1 is not numbered 1 on both sides: %q", row)
	}

	if row := rows(t, m)[rowOf(t, m, "line 127 of the file")]; !strings.Contains(row, "125 127 ") {
		t.Errorf("head line 127 does not read as base line 125: %q", row)
	}
}

func TestPreviewIsTakenBack(t *testing.T) {
	m := pane(t, twoHunks, 60, tall)
	was := joined(t, m)

	m = preview(t, twoHunks, 60, tall, bodyLines)
	if joined(t, m) == was {
		t.Fatal("the pane drew the same rows with the whole file on")
	}

	if !m.TogglePreview() {
		t.Fatal("the pane refused to take the whole file back")
	}
	if got := joined(t, m); got != was {
		t.Errorf("the rows did not come back:\n%s", got)
	}
}

func TestPreviewCarriesTheCursor(t *testing.T) {
	m := pane(t, twoHunks, 60, tall)
	m.Select(store.SideHead, 13)

	m = press(t, m, down)
	line := rows(t, m)[m.Cursor()]

	m.TogglePreview()
	m.SetBody(twoHunks, testchangeset.Body(bodyLines))
	if got := rows(t, m)[m.Cursor()]; got != line {
		t.Errorf("the cursor moved to %q, want %q", got, line)
	}

	m.TogglePreview()
	if got := rows(t, m)[m.Cursor()]; got != line {
		t.Errorf("the cursor came back on %q, want %q", got, line)
	}
}

func TestPreviewLinesBelongToNoHunk(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	m.Restore(rowOf(t, m, "line 130 of the file"), 0)
	if side, line, ok := m.Hunk(); ok {
		t.Errorf("a row below the last hunk reads as %s:%d", side, line)
	}

	m.Restore(rowOf(t, m, `Unreviewed State = "unreviewed"`), 0)
	if _, _, ok := m.Hunk(); !ok {
		t.Error("a row inside a hunk reads as being in none")
	}
}

func TestPreviewWidensTheGutter(t *testing.T) {
	short := preview(t, twoHunks, 60, tall, bodyLines)
	long := preview(t, twoHunks, 60, tall, 1300)

	at := func(m diffpane.Model) int {
		return strings.Index(rows(t, m)[0], "line 1 of the file")
	}
	if at(long) != at(short)+2 {
		t.Errorf("the code starts at column %d over 1300 lines and %d over %d, want two further",
			at(long), at(short), bodyLines)
	}
}

func TestPreviewRefusesAFileWithNoLines(t *testing.T) {
	m := pane(t, "assets/logo.png", 60, 10)
	if m.TogglePreview() {
		t.Error("the pane took the whole of a binary file")
	}
	if _, asked := m.NeedsBody(); asked {
		t.Error("the pane asked for the text of a binary file")
	}
}

func TestPreviewAsksOncePerFile(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	m.TogglePreview()
	m.TogglePreview()
	if path, asked := m.NeedsBody(); asked {
		t.Errorf("the pane asked for %s again", path)
	}
}

func TestPreviewStandsDownOnAnEmptyBody(t *testing.T) {
	m := pane(t, twoHunks, 60, tall)
	was := joined(t, m)

	m.TogglePreview()
	if m.SetBody(twoHunks, testchangeset.Body(0)) {
		t.Error("the pane reported drawing text it was handed none of")
	}
	if got := joined(t, m); got != was {
		t.Errorf("the rows moved for an empty body:\n%s", got)
	}

	if _, asked := m.NeedsBody(); asked {
		t.Error("the pane asked for a body it has already been told is empty")
	}
}

func TestEveryPreviewRowIsExactlyThePane(t *testing.T) {
	for _, width := range []int{40, 60, 61} {
		m := preview(t, twoHunks, width, tall, bodyLines)

		for i, row := range rows(t, m) {
			if got := lipgloss.Width(row); got != width {
				t.Errorf("width %d: row %d is %d cells, want %d: %q", width, i, got, width, row)
			}
		}
	}
}

func TestPreviewPairsSideBySide(t *testing.T) {
	m := preview(t, twoHunks, splitWide, tall, bodyLines)
	if short := m.ToggleSplit(); short > 0 {
		t.Fatalf("the pane refused side-by-side at width %d, %d columns short", splitWide, short)
	}

	row := rows(t, m)[rowOf(t, m, "line 1 of the file")]
	if !strings.Contains(row, splitRule) {
		t.Fatalf("a gap row drew no rule between the columns: %q", row)
	}
	if got := strings.Count(row, "line 1 of the file"); got != 2 {
		t.Errorf("a gap row names its line %d times, want one per column: %q", got, row)
	}
}

func TestPreviewKeepsACardUnderItsLine(t *testing.T) {
	c := testchangeset.Comment("bbbbbbbbbbbb", twoHunks, 13, 13, "The longer word is the clearer one.")
	m := commented(t, twoHunks, 60, tall, c)

	m.TogglePreview()
	m.SetBody(twoHunks, testchangeset.Body(bodyLines))

	line := rowOf(t, m, `Unreviewed State = "unreviewed"`)
	card := rowOf(t, m, "The longer word is the clearer one.")
	if card <= line {
		t.Fatalf("the card is on row %d and the line it answers on %d", card, line)
	}
	if card-line > 3 {
		t.Errorf("the card is %d rows under the line it answers", card-line)
	}
}

func TestPreviewHangsACardUnderAFilledInLine(t *testing.T) {
	c := testchangeset.Comment("gggggggggggg", twoHunks, 20, 20, "This run is doing two jobs.")
	m := commented(t, twoHunks, 60, tall, c)

	foot := rowOf(t, m, "This run is doing two jobs.")
	got := joined(t, m)
	if !strings.Contains(got, "line 20") {
		t.Errorf("the card at the foot does not name the line it is about:\n%s", got)
	}
	if strings.Contains(got, "was line") {
		t.Errorf("the card says the line has gone:\n%s", got)
	}

	m.TogglePreview()
	m.SetBody(twoHunks, testchangeset.Body(bodyLines))

	line := rowOf(t, m, "line 20 of the file")
	card := rowOf(t, m, "This run is doing two jobs.")
	switch {
	case card <= line:
		t.Fatalf("the card is on row %d and the line it answers on %d", card, line)
	case card-line > 3:
		t.Errorf("the card is %d rows under the line it answers", card-line)
	case card == foot:
		t.Error("the card stayed at the foot of the file")
	}

	if got := joined(t, m); strings.Contains(got, "line 20 ─") || strings.Contains(got, "· line 20") {
		t.Errorf("the card still names a line the row above it already has:\n%s", got)
	}
}

func TestPreviewKeepsTheWindowOnAFilledInCard(t *testing.T) {
	c := testchangeset.Comment("gggggggggggg", twoHunks, 60, 60, "Why is this here?")
	m := commented(t, twoHunks, 60, 12, c)

	m.TogglePreview()
	m.SetBody(twoHunks, testchangeset.Body(bodyLines))
	m.SelectComment("gggggggggggg")

	got := joined(t, m)
	for _, want := range []string{"line 60 of the file", "Why is this here?"} {
		if !strings.Contains(got, want) {
			t.Errorf("the window does not hold %q:\n%s", want, got)
		}
	}
}

func TestPreviewCarriesASelectionOnItsLines(t *testing.T) {
	m := pane(t, twoHunks, 60, tall)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, selectKey)

	was, open := m.Selected()
	if !open {
		t.Fatal("v opened no selection")
	}

	m.TogglePreview()
	m.SetBody(twoHunks, testchangeset.Body(bodyLines))

	got, still := m.Selected()
	if !still {
		t.Fatal("the selection went when the whole file came in")
	}
	if !reflect.DeepEqual(got, was) {
		t.Errorf("the selection is %+v, want the %+v it was anchored on", got, was)
	}

	m.TogglePreview()
	if back, still := m.Selected(); !still || !reflect.DeepEqual(back, was) {
		t.Errorf("the selection came back as %+v, want %+v", back, was)
	}
}

func TestPreviewDropsASelectionItCannotPlace(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	m.Restore(rowOf(t, m, "line 60 of the file"), 0)
	m = press(t, m, selectKey)
	if _, open := m.Selected(); !open {
		t.Fatal("v opened no selection on a filled-in line")
	}

	m.TogglePreview()
	if got, open := m.Selected(); open {
		t.Errorf("the selection survived as %+v on a line the hunks have no row for", got)
	}
}

func TestPreviewFramesEveryHunk(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)
	rows := rows(t, m)

	blank := func(i int) bool { return strings.TrimSpace(rows[i]) == "" }

	for _, h := range []string{"@@ -10,5 +10,5 @@", "@@ -120,5 +120,7 @@"} {
		at := rowOf(t, m, h)
		if !blank(at - 1) {
			t.Errorf("%s has no frame above it: %q", h, rows[at-1])
		}
	}

	for _, pair := range [][2]string{
		{`Partial    State = "partial"`, "line 15 of the file"},
		{"return Derive(files, rows), nil", "line 127 of the file"},
	} {
		last, next := rowOf(t, m, pair[0]), rowOf(t, m, pair[1])
		if next != last+2 || !blank(last+1) {
			t.Errorf("the file picks up on row %d after a hunk ending on %d, want a frame between",
				next, last)
		}
	}
}

func TestPreviewFramesNothingTwice(t *testing.T) {
	m := preview(t, "internal/cli/render.go", 60, tall, 40)

	rows := rows(t, m)
	if strings.TrimSpace(rows[0]) == "" {
		t.Errorf("the pane opens on a blank row:\n%s", joined(t, m))
	}
	content := len(rows)
	for content > 0 && strings.TrimSpace(rows[content-1]) == "" {
		content--
	}
	for i := 1; i < content; i++ {
		if strings.TrimSpace(rows[i]) == "" && strings.TrimSpace(rows[i-1]) == "" {
			t.Fatalf("rows %d and %d are both blank:\n%s", i-1, i, joined(t, m))
		}
	}
}
