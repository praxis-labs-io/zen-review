package diffpane_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
)

const (
	tallCard    = "tttttttttttt"
	tallReplies = 30
)

var (
	bottom = tea.KeyPressMsg{Code: 'G', Text: "G"}
	top    = tea.KeyPressMsg{Code: 'g', Text: "g"}
)

func reply(n int) string { return fmt.Sprintf("reply line %02d", n) }

func talking(line int, state store.CommentState) store.Comment {
	lines := make([]string, tallReplies)
	for i := range lines {
		lines[i] = reply(i + 1)
	}
	c := testchangeset.Comment(tallCard, twoHunks, line, line, "Say the whole of it.")
	return testchangeset.Responded(testchangeset.In(c, state), strings.Join(lines, "\n"))
}

func tallPane(t *testing.T, c store.Comment) diffpane.Model {
	t.Helper()

	m := commented(t, twoHunks, 100, 12, c)
	m.Select(store.SideHead, 13)
	return m
}

func walk(t *testing.T, m diffpane.Model, k tea.KeyPressMsg, presses int) (diffpane.Model, map[string]bool) {
	t.Helper()

	seen := make(map[string]bool)
	look := func() {
		got := joined(t, m)
		for n := 1; n <= tallReplies; n++ {
			if strings.Contains(got, reply(n)) {
				seen[reply(n)] = true
			}
		}
	}

	look()
	for range presses {
		m = press(t, m, k)
		look()
	}
	return m, seen
}

func missed(seen map[string]bool) []string {
	var out []string
	for n := 1; n <= tallReplies; n++ {
		if !seen[reply(n)] {
			out = append(out, reply(n))
		}
	}
	return out
}

func TestEveryKeyReadsTheWholeOfATallCard(t *testing.T) {
	cases := []struct {
		name    string
		comment store.Comment
		from    []tea.KeyPressMsg
		key     tea.KeyPressMsg
	}{
		{"j over an orphan at the bottom", talking(900, store.CommentOrphaned), nil, down},
		{"k over an orphan at the bottom", talking(900, store.CommentOrphaned), []tea.KeyPressMsg{bottom}, up},
		{"ctrl+d over an orphan at the bottom", talking(900, store.CommentOrphaned), nil, halfDown},
		{"ctrl+u over an orphan at the bottom", talking(900, store.CommentOrphaned), []tea.KeyPressMsg{bottom}, halfUp},
		{"j over an addressed card under a pinned heading", talking(13, store.CommentAddressed), nil, down},
		{"k over an addressed card under a pinned heading", talking(13, store.CommentAddressed), []tea.KeyPressMsg{bottom}, up},
		{"ctrl+d over an addressed card under a pinned heading", talking(13, store.CommentAddressed), nil, halfDown},
		{"ctrl+u over an addressed card under a pinned heading", talking(13, store.CommentAddressed), []tea.KeyPressMsg{bottom}, halfUp},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := press(t, tallPane(t, tc.comment), tc.from...)

			_, seen := walk(t, m, tc.key, 120)
			if gone := missed(seen); len(gone) > 0 {
				t.Errorf("%d of %d lines never came on screen, first %q", len(gone), tallReplies, gone[0])
			}
		})
	}
}

func TestJLeavesATallCardOnceItsEndIsShown(t *testing.T) {
	m := tallPane(t, talking(13, store.CommentAddressed))

	for range 80 {
		if id, on := m.Comment(); on && id == tallCard {
			break
		}
		m = press(t, m, down)
	}
	if _, on := m.Comment(); !on {
		t.Fatalf("j never landed on the card:\n%s", joined(t, m))
	}

	m, _ = walk(t, m, down, 80)
	if _, on := m.Comment(); on {
		t.Errorf("80 presses of j left the cursor on the card:\n%s", joined(t, m))
	}
	if got := filled(t, m); got == "" {
		t.Errorf("the cursor left the card for a row that is not on screen:\n%s", joined(t, m))
	}
}

func TestTheCursorStaysOnATallCardWhileItScrolls(t *testing.T) {
	m := press(t, tallPane(t, talking(900, store.CommentOrphaned)), bottom)

	for i := range 20 {
		m = press(t, m, up)
		if id, on := m.Comment(); on && id == tallCard {
			continue
		}
		if !strings.Contains(joined(t, m), reply(1)) {
			t.Fatalf("after %d presses of k the cursor left the card before its top was shown:\n%s",
				i+1, joined(t, m))
		}
		return
	}
}

func TestGShowsTheEndOfATallCard(t *testing.T) {
	m := press(t, tallPane(t, talking(900, store.CommentOrphaned)), bottom)

	if got := joined(t, m); !strings.Contains(got, reply(tallReplies)) {
		t.Errorf("G left the card's last line off screen:\n%s", got)
	}
	if id, on := m.Comment(); !on || id != tallCard {
		t.Errorf("G left the cursor on %q, want the card", id)
	}
}

func TestGgFromATallCardGoesToTheTop(t *testing.T) {
	m := press(t, tallPane(t, talking(900, store.CommentOrphaned)), bottom, top, top)

	if got := m.Cursor(); got != 0 {
		t.Errorf("gg left the cursor on row %d, want the first", got)
	}
}

func TestSelectingATallCardOpensItAtItsTop(t *testing.T) {
	for _, c := range []store.Comment{talking(13, store.CommentAddressed), talking(900, store.CommentOrphaned)} {
		m := tallPane(t, c)
		m.SelectComment(tallCard)

		if got := joined(t, m); !strings.Contains(got, "Say the whole of it.") {
			t.Errorf("selecting the %s card left its body off screen:\n%s", c.State, got)
		}
	}
}

func TestAPageThatLandsInACardStopsOnIt(t *testing.T) {
	m := tallPane(t, talking(13, store.CommentAddressed))

	for range 10 {
		m = press(t, m, halfDown)
		if id, on := m.Comment(); on && id == tallCard {
			return
		}
	}
	t.Errorf("ctrl+d paged past the card without stopping on it:\n%s", joined(t, m))
}

func hunkCard(start, end int) store.Comment {
	return testchangeset.OnHunk(testchangeset.Comment("hhhhhhhhhhhh", twoHunks, start, end, "This hunk does two things."))
}

func TestAHunkCommentDrawsUnderItsHeading(t *testing.T) {
	cases := []struct {
		name       string
		start, end int
		heading    string
	}{
		{"on the whole hunk", 120, 126, "@@ -120,5 +120,7"},
		{"on lines that no longer match the hunk", 12, 40, "@@ -10,5 +10,5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := commented(t, twoHunks, 76, 40, hunkCard(tc.start, tc.end))

			if got := under(t, m, tc.heading); !strings.Contains(got, "◇ open · hunk") {
				t.Errorf("the row under %s is %q, want the hunk's card:\n%s", tc.heading, got, joined(t, m))
			}
		})
	}
}

func TestAHunkCommentWhoseFirstLineLeftEveryHunkGoesToTheFoot(t *testing.T) {
	m := commented(t, twoHunks, 76, 40, hunkCard(900, 905))
	got := rows(t, m)

	last := ""
	for _, row := range got {
		if strings.TrimSpace(row) != "" {
			last = row
		}
	}
	if !strings.Contains(joined(t, m), "lines 900-905") {
		t.Fatalf("the card does not name its lines:\n%s", joined(t, m))
	}
	if strings.Contains(under(t, m, "@@ -120,5 +120,7"), "◇") || strings.Contains(under(t, m, "@@ -10,5 +10,5"), "◇") {
		t.Errorf("a card on no hunk drew under a heading:\n%s", joined(t, m))
	}
	if !strings.Contains(last, "╰") {
		t.Errorf("the pane does not end on the card, it ends on %q:\n%s", last, joined(t, m))
	}
}

func TestEnterFromAHunkCardGoesToItsHeading(t *testing.T) {
	m := commented(t, twoHunks, 76, 40, hunkCard(120, 126))
	m.SelectComment("hhhhhhhhhhhh")
	m = press(t, m, enter)

	if got := filled(t, m); !strings.Contains(got, "@@ -120,5 +120,7") {
		t.Errorf("enter left the cursor on %q, want the hunk's heading", got)
	}
}
