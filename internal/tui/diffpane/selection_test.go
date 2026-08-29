package diffpane_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

var (
	sel    = tea.KeyPressMsg{Code: 'v', Text: "v"}
	cancel = tea.KeyPressMsg{Code: tea.KeyEscape}
)

// selected is the pane's spans as the cases below read them, side first.
func selected(t *testing.T, m diffpane.Model) []string {
	t.Helper()
	return spans(m.Selected())
}

// spans is what Selected and Line hand back, written out, and nil for neither.
func spans(as []review.Anchor, on bool) []string {
	if !on {
		return nil
	}

	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, fmt.Sprintf("%s %d:%d", a.Side, a.Range.Start, a.Range.End))
	}
	return out
}

// equal is two lists of spans, which is all these cases compare.
func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// atFirstHunk is the pane on the fixture's mixed hunk, cursor three rows down
// on the last context line above the change. Nothing is selected yet.
func atFirstHunk(t *testing.T, comments ...store.Comment) diffpane.Model {
	t.Helper()

	m := commented(t, twoHunks, 70, 20, comments...)
	m.Select(store.SideHead, 13)
	return press(t, m, down, down, down)
}

// TestVFillsTheRowsItCovers. The whole point of the key is on screen and only
// in colour, so this reads the raw frame rather than a golden.
func TestVFillsTheRowsItCovers(t *testing.T) {
	m := press(t, atFirstHunk(t), sel, down, down, down)

	want := []string{"12  12   const (", `−     Unreviewed State = "unread"`,
		`+     Unreviewed State = "unreviewed"`, `Partial    State = "partial"`}

	got := filledRows(t, m)
	if len(got) != len(want) {
		t.Fatalf("filled rows = %v, want the four the selection covers", got)
	}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Errorf("filled row %d = %q, want it to hold %q", i, got[i], want[i])
		}
	}
}

// TestOnlyCodeFillsUnderASelection. A heading and the blank between two hunks
// are the pane's own rows, and neither is a line anything marks.
func TestOnlyCodeFillsUnderASelection(t *testing.T) {
	m := press(t, atFirstHunk(t), sel, down, down, down, down, down, down)

	got := filledRows(t, m)
	if len(got) != 6 {
		t.Fatalf("filled rows = %v, want the six code rows the span covers", got)
	}
	for _, line := range got {
		if strings.Contains(line, "@@") || strings.TrimSpace(line) == "" {
			t.Errorf("a row that is not code drew filled: %q", line)
		}
	}
}

// TestAPagingKeyFillsEverythingItCrossed. point repaints the row it left and
// the row it took, which is every row a j moved over and not a ctrl+d.
func TestAPagingKeyFillsEverythingItCrossed(t *testing.T) {
	m := press(t, atFirstHunk(t), sel, halfDown)

	got := filledRows(t, m)
	if len(got) != 9 {
		t.Fatalf("filled rows = %v, want every code row the page crossed", got)
	}
}

// TestASelectionComesBackOffTwoKeys. esc is the way out, and v is the same key
// that opened it.
func TestASelectionComesBackOffTwoKeys(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  tea.KeyPressMsg
	}{{"esc", cancel}, {"v again", sel}} {
		t.Run(tt.name, func(t *testing.T) {
			m := press(t, atFirstHunk(t), sel, down, down)
			if len(filledRows(t, m)) != 3 {
				t.Fatalf("the selection did not draw: %v", filledRows(t, m))
			}

			m = press(t, m, tt.key)
			if got := filledRows(t, m); len(got) != 1 {
				t.Errorf("filled rows = %v, want the cursor's alone", got)
			}
			if _, on := m.Selected(); on {
				t.Error("the pane still reports a selection")
			}
		})
	}
}

// TestASelectionNamesBothSidesItCovers. A reader dragging over a rewritten
// block read the removal as well as what replaced it.
func TestASelectionNamesBothSidesItCovers(t *testing.T) {
	m := press(t, atFirstHunk(t), sel, down, down, down)

	want := []string{"head 12:14", "base 12:14"}
	if got := selected(t, m); !equal(got, want) {
		t.Errorf("spans = %v, want %v", got, want)
	}
}

// TestASelectionCrossesAHunk, and says so as one span per side. review is what
// cuts each back to the lines a hunk holds.
func TestASelectionCrossesAHunk(t *testing.T) {
	m := press(t, atFirstHunk(t), down, down, sel, down, down, down, down, down, down, down)

	want := []string{"head 13:124", "base 14:123"}
	if got := selected(t, m); !equal(got, want) {
		t.Errorf("spans = %v, want %v", got, want)
	}
}

// TestNothingSelectedIsNotAnEmptySelection. r has to tell a press that aimed
// from one that did not, and both hold no spans.
func TestNothingSelectedIsNotAnEmptySelection(t *testing.T) {
	if _, on := atFirstHunk(t).Selected(); on {
		t.Error("a pane nobody pressed v on reports a selection")
	}
}

// TestAResizeKeepsTheSelectionOnItsLines. A card's height moves with the width,
// so every row after it renumbers and a stored row index lands on other code.
func TestAResizeKeepsTheSelectionOnItsLines(t *testing.T) {
	card := testchangeset.Comment("bbbbbbbbbbbb", twoHunks, 11, 11,
		"this card sits above the selection and wraps to a different number of rows at every width it is drawn at")

	// Selected in the hunk below the card, which is where a row index goes wrong
	// by however many rows the card's body gained.
	m := commented(t, twoHunks, 70, 24, card)
	m.Select(store.SideHead, 124)

	m = press(t, m, down, sel, down)
	want := selected(t, m)

	m.SetSize(40, 24)

	if got := selected(t, m); !equal(got, want) {
		t.Errorf("spans = %v after the resize, want %v", got, want)
	}
}

// TestARelayoutKeepsTheSelectionOnScreen. layout repaints with no cursor, so a
// span it did not know about draws as bare code while Selected still names it.
func TestARelayoutKeepsTheSelectionOnScreen(t *testing.T) {
	card := testchangeset.Comment("bbbbbbbbbbbb", twoHunks, 11, 11,
		"this card sits above the selection and wraps to a different number of rows at every width it is drawn at")

	for _, tt := range []struct {
		name  string
		open  func(m diffpane.Model) diffpane.Model
		relay func(m *diffpane.Model)
	}{
		{
			"a resize",
			func(m diffpane.Model) diffpane.Model {
				m.Select(store.SideHead, 124)
				return press(t, m, down, sel, down)
			},
			func(m *diffpane.Model) { m.SetSize(40, 24) },
		},
		{
			// Folded from the card itself, which the cursor reaches while the
			// selection is open because a card is one stop like any other row.
			"folding a card inside the span",
			func(m diffpane.Model) diffpane.Model {
				m.Select(store.SideHead, 13)
				return press(t, m, down, sel, down, down)
			},
			func(m *diffpane.Model) { *m = press(t, *m, space) },
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.open(commented(t, twoHunks, 70, 24, card))

			if got := filledRows(t, m); len(got) != 2 {
				t.Fatalf("filled rows = %v, want the two the selection covers", got)
			}

			tt.relay(&m)

			if got := filledRows(t, m); len(got) != 2 {
				t.Errorf("filled rows = %v after %s, want the selection still drawn", got, tt.name)
			}
		})
	}
}

// TestASpanOverNoCodeNamesNothing. A heading is one stop the cursor lands on, so
// v there is a press a reader makes, and an empty list of anchors is not a scope.
func TestASpanOverNoCodeNamesNothing(t *testing.T) {
	m := commented(t, twoHunks, 70, 20)
	m.Select(store.SideHead, 13)

	if as, on := press(t, m, sel).Selected(); on {
		t.Errorf("a selection over a heading alone reports %v", as)
	}
}

// TestTheCursorsOwnRowNamesItsLines, which is what c scopes to with nothing
// selected. A context row is on both sides and a changed row is on one.
func TestTheCursorsOwnRowNamesItsLines(t *testing.T) {
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, "unreviewed is the clearer word.")

	for _, tt := range []struct {
		name string
		at   func(m diffpane.Model) diffpane.Model
		want []string
	}{
		{"a heading", func(m diffpane.Model) diffpane.Model { return m }, nil},
		{"a context row", func(m diffpane.Model) diffpane.Model {
			return press(t, m, down, down, down)
		}, []string{"head 12:12", "base 12:12"}},
		{"a removal", func(m diffpane.Model) diffpane.Model {
			return press(t, m, down, down, down, down)
		}, []string{"base 13:13"}},
		{"an addition", func(m diffpane.Model) diffpane.Model {
			return press(t, m, down, down, down, down, down)
		}, []string{"head 13:13"}},
		{"a card", func(m diffpane.Model) diffpane.Model {
			m.SelectComment("cccccccccccc")
			return m
		}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := commented(t, twoHunks, 70, 20, card)
			m.Select(store.SideHead, 13)

			if got := spans(tt.at(m).Line()); !equal(got, tt.want) {
				t.Errorf("the row names %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTheRootMovingTheCursorClearsTheSelection. A span anchored in a file the
// reader has left is one nobody can see the ends of.
func TestTheRootMovingTheCursorClearsTheSelection(t *testing.T) {
	card := testchangeset.Comment("bbbbbbbbbbbb", twoHunks, 13, 13, "unreviewed is the clearer word.")

	for _, tt := range []struct {
		name string
		move func(m *diffpane.Model)
	}{
		{"a hunk", func(m *diffpane.Model) { m.Select(store.SideHead, 124) }},
		{"a comment", func(m *diffpane.Model) { m.SelectComment("bbbbbbbbbbbb") }},
		{"a file", func(m *diffpane.Model) { m.SetFile(nil, nil, nil, 2) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := press(t, atFirstHunk(t, card), sel, down, down)
			tt.move(&m)

			if _, on := m.Selected(); on {
				t.Errorf("the selection outlived the cursor moving to %s", tt.name)
			}
		})
	}
}

// A theme that could offer no surface leaves the fill nil, and the fill is the
// only thing marking the rows of a selection the cursor is not on. The reader
// presses v, moves down, and has to be able to see what the range covers before
// pressing c. The bar cannot stand in: it says which row the next key moves from.
//
// The two panes end with the cursor on the same row, so the bar is in the same
// place and the selection is the only thing left to tell them apart.
func TestASelectionStandsWithoutAFill(t *testing.T) {
	bare := func(t *testing.T) diffpane.Model {
		t.Helper()

		m := diffpane.New(theme.Terminal(theme.Surface{}))
		m.SetSize(60, 10)
		m.SetFile(fileAt(t, testchangeset.Nested(t), "internal/review/state.go"), nil, nil, 2)
		m.Select(store.SideHead, 13)
		return m
	}

	loose := press(t, bare(t), down, down, down)
	held := press(t, bare(t), down, sel, down, down)

	if _, on := held.Selected(); !on {
		t.Fatal("the keys opened no selection, so the case never ran")
	}
	if loose.Cursor() != held.Cursor() {
		t.Fatalf("the cursor is on %d and %d, so the two frames differ for another reason",
			loose.Cursor(), held.Cursor())
	}

	if held.View() == loose.View() {
		t.Errorf("the pane draws identically with a selection open, so nothing says what it covers:\n%s",
			ansi.Strip(held.View()))
	}
}
