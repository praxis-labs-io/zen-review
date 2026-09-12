package diffpane_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
)

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

func TestAPasteReachesTheBox(t *testing.T) {
	m, _ := composing(t, 70, 20).Update(tea.PasteMsg{Content: "from somewhere else"})

	if got := m.Draft(); got != "from somewhere else" {
		t.Errorf("the box holds %q, want the paste", got)
	}
}

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

func TestABoxRefusesAPaneWithNoRoomForIt(t *testing.T) {
	for _, tt := range []struct{ width, height int }{{18, 20}, {70, 4}, {70, 6}, {70, 7}} {
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

func editing(t *testing.T, width, height int, c store.Comment) diffpane.Model {
	t.Helper()

	m := commented(t, twoHunks, width, height, c)
	m.Select(store.SideHead, 13)

	if _, ok := m.Edit(c); !ok {
		t.Fatalf("the pane refused a box at %dx%d", width, height)
	}
	return m
}

func TestTheBoxOverACardHoldsWhatItSaid(t *testing.T) {
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, "unreviewd is the clearer word.")
	m := editing(t, 70, 20, card)

	if got := m.Draft(); got != card.Body {
		t.Errorf("the box holds %q, want what the card said", got)
	}

	got := joined(t, m)
	if !strings.Contains(got, "◇ editing") {
		t.Errorf("the box does not say what it is doing:\n%s", got)
	}
	if n := strings.Count(got, "unreviewd"); n != 1 {
		t.Errorf("the words are drawn %d times, want once, in the box:\n%s", n, got)
	}
	if strings.Contains(got, "◇ open") {
		t.Errorf("the card is still drawn under its own box:\n%s", got)
	}
}

func TestTheBoxStandsWhereTheCardWas(t *testing.T) {
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, "unreviewd is the clearer word.")

	was := at(t, rows(t, commented(t, twoHunks, 70, 20, card)), "◇ open")
	now := at(t, rows(t, editing(t, 70, 20, card)), "◇ editing")

	if was != now {
		t.Errorf("the box is on row %d, want %d, where the card was", now, was)
	}
}

func TestClosingTheBoxPutsTheCardBack(t *testing.T) {
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, "unreviewd is the clearer word.")

	m := press(t, editing(t, 70, 20, card), tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.CloseDraft()

	got := joined(t, m)
	if strings.Contains(got, "editing") {
		t.Errorf("the box is still up:\n%s", got)
	}
	if !strings.Contains(got, "◇ open") || !strings.Contains(got, "unreviewd") {
		t.Errorf("the card did not come back:\n%s", got)
	}
}

func at(t *testing.T, lines []string, want string) int {
	t.Helper()

	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	t.Fatalf("no row holds %q:\n%s", want, strings.Join(lines, "\n"))
	return -1
}

func TestTheBoxOnARangeTallerThanTheWindow(t *testing.T) {
	m := commented(t, twoHunks, 70, 8)
	m.Select(store.SideHead, 13)

	if _, ok := m.Compose(store.Comment{
		Side:      store.SideHead,
		Scope:     store.ScopeRange,
		LineRange: store.LineRange{Start: 12, End: 14},
	}); !ok {
		t.Fatal("the pane refused a box")
	}

	got := joined(t, m)
	if !strings.Contains(got, "◇ new") || !strings.Contains(got, "ctrl+s save") {
		t.Errorf("the box is not on screen whole:\n%s", got)
	}
}

func TestACardDrawsTheBreaksTheBoxWasTypedWith(t *testing.T) {
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13,
		"the first thing\nthe second thing\nthe third")

	m := commented(t, twoHunks, 70, 20, card)
	m.SelectComment("cccccccccccc")

	rows := rows(t, m)
	first := at(t, rows, "the first thing")

	for i, want := range []string{"the second thing", "the third"} {
		if got := rows[first+1+i]; !strings.Contains(got, want) {
			t.Errorf("row %d reads %q, want %q on a line of its own", first+1+i, got, want)
		}
	}
}

func TestClosingTheBoxPutsTheCursorBackOnTheCard(t *testing.T) {
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, "unreviewd is the clearer word.")

	m := editing(t, 70, 20, card)
	m.CloseDraft()

	if got := joined(t, m); !strings.Contains(got, "x resolve") {
		t.Errorf("the card came back unlit:\n%s", got)
	}
	if id, on := m.Comment(); !on || id != "cccccccccccc" {
		t.Errorf("the cursor is on %q (%v), want the card the box stood in for", id, on)
	}
}

func boxRows(t *testing.T, m diffpane.Model, head string) int {
	t.Helper()

	lines := rows(t, m)
	return at(t, lines, "ctrl+s save") - at(t, lines, head) + 1
}

func TestTheBoxGrowsWithWhatIsTypedIntoIt(t *testing.T) {
	m := composing(t, 70, 24)
	before := boxRows(t, m, "◇ new")

	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	m = press(t, m, enter, enter, enter, enter, enter, enter)

	if got := boxRows(t, m, "◇ new"); got != before+3 {
		t.Errorf("the box is %d rows after seven lines, want %d", got, before+3)
	}
}

func TestTheBoxOpensTallEnoughForWhatItHolds(t *testing.T) {
	body := "one\ntwo\nthree\nfour\nfive\nsix"
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, body)

	m := editing(t, 70, 24, card)

	if got := boxRows(t, m, "◇ editing"); got != 8 {
		t.Errorf("the box is %d rows, want 8, the six lines and its borders", got)
	}
	if got := joined(t, m); !strings.Contains(got, "six") {
		t.Errorf("the last line is not on screen:\n%s", got)
	}
}

func TestTheBoxStopsAtThePane(t *testing.T) {
	body := strings.Repeat("a line\n", 40)
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, body)

	m := editing(t, 70, 12, card)

	if got := boxRows(t, m, "◇ editing"); got != 10 {
		t.Errorf("the box is %d rows, want 10", got)
	}
}

func TestTheBoxOpensOnAWrappedBodyWholeAndUnscrolled(t *testing.T) {
	body := "the first line of it, which is long enough to fold at this width\n" +
		"and a second\n" +
		"and a third line, also long enough to fold at the width this is drawn at\n" +
		"the last line says LASTLINE"
	card := testchangeset.Comment("cccccccccccc", twoHunks, 13, 13, body)

	m := editing(t, 60, 30, card)

	got := joined(t, m)
	if !strings.Contains(got, "the first line of it") {
		t.Errorf("the box opened scrolled past its first line:\n%s", got)
	}
	if !strings.Contains(got, "LASTLINE") {
		t.Errorf("the box is too short for its last line:\n%s", got)
	}

	lines := rows(t, m)
	for i := at(t, lines, "◇ editing") + 1; i < at(t, lines, "ctrl+s save"); i++ {
		if strings.TrimSpace(strings.ReplaceAll(lines[i], "│", "")) == "" {
			t.Errorf("row %d inside the box is blank:\n%s", i, got)
		}
	}
}
