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

// bodyLines is how long the two-hunk file is in the fixture. Past its last hunk,
// which ends at 126, so the rows below it are rows the diff never showed.
const bodyLines = 130

// tall is a pane deep enough to draw the whole of that file, so a row assertion
// does not have to scroll to the row it is about.
const tall = bodyLines + 16

// preview is a pane already showing the whole file, the text asked for and
// handed over the way the root does it.
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

// rowOf is the index of the one row holding text, and fails the test when the
// pane drew it twice or not at all.
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

// TestPreviewFillsTheGapsWithTheFile. Three lines of context are what the key
// exists to get past, so the rows the diff never showed are the whole assertion.
func TestPreviewFillsTheGapsWithTheFile(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	// The file opens on its own first line, and the heading of the first hunk
	// arrives after the nine lines above it rather than on row nought.
	if got := rowOf(t, m, "line 1 of the file"); got != 0 {
		t.Errorf("the file opens on row %d, want 0", got)
	}

	head := rowOf(t, m, "@@ -10,5 +10,5 @@")
	if last := rowOf(t, m, "line 9 of the file"); last != head-1 {
		t.Errorf("line 9 is on row %d and the first heading on %d, want it just above",
			last, head)
	}

	// The lines between the two hunks, which the diff showed none of, and the
	// ones below the last, which it had no reason to reach.
	for _, line := range []string{"line 15 of the file", "line 119 of the file", "line 130 of the file"} {
		rowOf(t, m, line)
	}
}

// TestPreviewNumbersBothSides. A gap row carries the base number the hunks above
// it have moved it to, or the two gutters disagree about the same line.
func TestPreviewNumbersBothSides(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	// Above every hunk the two sides are still in step.
	if row := rows(t, m)[rowOf(t, m, "line 1 of the file")]; !strings.Contains(row, "  1   1 ") {
		t.Errorf("line 1 is not numbered 1 on both sides: %q", row)
	}

	// The second hunk adds two lines, so everything under it sits two further on
	// than the base it came from.
	if row := rows(t, m)[rowOf(t, m, "line 127 of the file")]; !strings.Contains(row, "125 127 ") {
		t.Errorf("head line 127 does not read as base line 125: %q", row)
	}
}

// TestPreviewIsTakenBack. The mode lasts the run and the key that set it is the
// key that clears it, so the rows have to come all the way back.
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

// TestPreviewCarriesTheCursor. The two modes do not number their rows the same,
// so a stored row index would land the reader on something else.
func TestPreviewCarriesTheCursor(t *testing.T) {
	m := pane(t, twoHunks, 60, tall)
	m.Select(store.SideHead, 13)

	// Down off the heading and onto the first line the hunk holds.
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

// TestPreviewLinesBelongToNoHunk. r takes the hunk the cursor is in, and a line
// two hundred rows away from one is not in it.
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

// TestPreviewWidensTheGutter. The file runs past its last hunk, and a gutter
// sized off the hunks puts every number below them out of its column.
func TestPreviewWidensTheGutter(t *testing.T) {
	short := preview(t, twoHunks, 60, tall, bodyLines)
	long := preview(t, twoHunks, 60, tall, 1300)

	at := func(m diffpane.Model) int {
		return strings.Index(rows(t, m)[0], "line 1 of the file")
	}
	// A digit each, and there are two gutters.
	if at(long) != at(short)+2 {
		t.Errorf("the code starts at column %d over 1300 lines and %d over %d, want two further",
			at(long), at(short), bodyLines)
	}
}

// TestPreviewRefusesAFileWithNoLines. A binary file has nothing to fill in, and
// a key that appeared to do nothing is worse than one that says why.
func TestPreviewRefusesAFileWithNoLines(t *testing.T) {
	m := pane(t, "assets/logo.png", 60, 10)
	if m.TogglePreview() {
		t.Error("the pane took the whole of a binary file")
	}
	if _, asked := m.NeedsBody(); asked {
		t.Error("the pane asked for the text of a binary file")
	}
}

// TestPreviewAsksOncePerFile. The text is the bytes of one generation, so a
// second press is the cache and not a second git call.
func TestPreviewAsksOncePerFile(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	m.TogglePreview()
	m.TogglePreview()
	if path, asked := m.NeedsBody(); asked {
		t.Errorf("the pane asked for %s again", path)
	}
}

// TestPreviewStandsDownOnAnEmptyBody. A blob the repository could not read comes
// back empty, and the mode cannot be left on over nothing.
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

	// And it is not asked for again: the answer was read, not missed.
	if _, asked := m.NeedsBody(); asked {
		t.Error("the pane asked for a body it has already been told is empty")
	}
}

// A row wider than the pane loses its trailing columns with no ellipsis, and a
// width test still passes. Every gap row is a row that has to fit.
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

// TestPreviewPairsSideBySide. The two modes are orthogonal, so the gap lines take
// both columns the way every other context line does.
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

// TestPreviewKeepsACardUnderItsLine. The card hangs on the row it answers, and
// filling the gaps renumbers every row after it.
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

// TestPreviewHangsACardUnderAFilledInLine. A comment on a line outside every
// hunk is one the diff had no row for, and the unplaced pass sends those to the
// foot of the file. Preview draws that line, so the card belongs under it.
func TestPreviewHangsACardUnderAFilledInLine(t *testing.T) {
	c := testchangeset.Comment("gggggggggggg", twoHunks, 20, 20, "This run is doing two jobs.")
	m := commented(t, twoHunks, 60, tall, c)

	// With the hunks alone it goes to the foot, naming the line it is about. Not
	// `was line 20`: the line is in the file, and the diff is not showing it.
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

	// The line is on screen now, so the gutter beside it carries the number and the
	// label has nothing left to say at all.
	if got := joined(t, m); strings.Contains(got, "line 20 ─") || strings.Contains(got, "· line 20") {
		t.Errorf("the card still names a line the row above it already has:\n%s", got)
	}
}

// TestPreviewKeepsTheWindowOnAFilledInCard. The card answers the line above it,
// so topping it would scroll that line away. Under preview the rows between are
// the file, which is what broke the arithmetic that found the line.
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

// TestPreviewCarriesASelectionOnItsLines. A selection is held as a row, and the
// two modes do not number those the same. Left alone it measures from whatever
// row took the number, and the comment goes against lines nobody picked.
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

	// And back out again.
	m.TogglePreview()
	if back, still := m.Selected(); !still || !reflect.DeepEqual(back, was) {
		t.Errorf("the selection came back as %+v, want %+v", back, was)
	}
}

// TestPreviewDropsASelectionItCannotPlace. A dropped selection costs a keypress.
// A moved one costs the comment, so the anchor is cleared rather than guessed at.
func TestPreviewDropsASelectionItCannotPlace(t *testing.T) {
	m := preview(t, twoHunks, 60, tall, bodyLines)

	// Anchored on a line only preview has a row for.
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
