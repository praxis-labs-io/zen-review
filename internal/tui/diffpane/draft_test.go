package diffpane_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/diffpane"
)

// composing is a pane with the box open on the fixture's mixed hunk, over the
// head line the cursor is on.
func composing(t *testing.T, width, height int) diffpane.Model {
	t.Helper()

	m := commented(t, twoHunks, width, height)
	m.Select(store.SideHead, 13)
	m = press(t, m, down, down, down)

	if _, ok := m.Compose(store.Comment{
		Side:      store.SideHead,
		Scope:     store.ScopeLine,
		LineRange: store.LineRange{Start: 12, End: 12},
	}); !ok {
		t.Fatalf("the pane refused a box at %dx%d", width, height)
	}
	return m
}

// TestTheBoxTakesWhatIsTypedIntoIt, and nothing else on screen moves for it: a
// formatter is not the only thing that must not reshuffle the page mid-word.
func TestTheBoxTakesWhatIsTypedIntoIt(t *testing.T) {
	m := composing(t, 70, 20)
	before := len(rows(t, m))

	m = press(t, m, tea.KeyPressMsg{Code: 'h', Text: "h"}, tea.KeyPressMsg{Code: 'i', Text: "i"})

	if got := m.Draft(); got != "hi" {
		t.Errorf("the box holds %q, want the two keys", got)
	}
	if got := joined(t, m); !strings.Contains(got, "hi") {
		t.Errorf("what was typed is not drawn:\n%s", got)
	}
	if got := len(rows(t, m)); got != before {
		t.Errorf("the pane is %d rows after two keystrokes, want %d", got, before)
	}
}

// TestAPasteReachesTheBox. It arrives as a message of its own rather than as
// keys, and a pane routing only presses would drop one.
func TestAPasteReachesTheBox(t *testing.T) {
	m, _ := composing(t, 70, 20).Update(tea.PasteMsg{Content: "from somewhere else"})

	if got := m.Draft(); got != "from somewhere else" {
		t.Errorf("the box holds %q, want the paste", got)
	}
}

// TestTheBoxSurvivesAResizeWithItsWords. Its width moves with the pane's, and
// the words are the one thing a resize must not cost.
func TestTheBoxSurvivesAResizeWithItsWords(t *testing.T) {
	m := press(t, composing(t, 70, 20), tea.KeyPressMsg{Code: 'h', Text: "h"})
	m.SetSize(50, 20)

	if got := m.Draft(); got != "h" {
		t.Errorf("the box holds %q after the resize", got)
	}
	if got := joined(t, m); !strings.Contains(got, "◇ new") {
		t.Errorf("the box is gone after the resize:\n%s", got)
	}
	for _, line := range rows(t, m) {
		if w := len([]rune(line)); w > 50 {
			t.Errorf("a row is %d columns after the resize: %q", w, line)
		}
	}
}

// TestClosingTheBoxLeavesTheCursorOnTheCode it hung off, which is where the
// reader was before they reached for the key.
func TestClosingTheBoxLeavesTheCursorOnTheCode(t *testing.T) {
	m := composing(t, 70, 20)
	was := m.Cursor()

	m.CloseDraft()

	if m.Composing() {
		t.Fatal("the box is still up")
	}
	if got := joined(t, m); strings.Contains(got, "◇ new") {
		t.Errorf("the box is still drawn:\n%s", got)
	}
	if got := m.Cursor(); got != was-1 {
		t.Errorf("the cursor is on row %d, want %d, the line the box hung under", got, was-1)
	}
}

// TestABoxRefusesAPaneWithNoRoomForIt. Every key goes into it while it is up,
// so one that cannot be drawn is a reader typing into nothing.
func TestABoxRefusesAPaneWithNoRoomForIt(t *testing.T) {
	for _, tt := range []struct{ width, height int }{{18, 20}, {70, 4}} {
		m := commented(t, twoHunks, tt.width, tt.height)
		m.Select(store.SideHead, 13)

		if _, ok := m.Compose(store.Comment{
			Side: store.SideHead, Scope: store.ScopeLine,
			LineRange: store.LineRange{Start: 12, End: 12},
		}); ok {
			t.Errorf("a %dx%d pane took a box", tt.width, tt.height)
		}
		if m.Composing() {
			t.Errorf("a %dx%d pane reports one up", tt.width, tt.height)
		}
	}
}
