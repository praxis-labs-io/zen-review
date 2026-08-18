package compose_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/tui/compose"
)

// TestTheBoxNeverOutgrowsTheFrame. Over clips what does not fit, and a clipped
// box loses the border off two of its sides and the way out with it.
func TestTheBoxNeverOutgrowsTheFrame(t *testing.T) {
	sizes := []struct{ width, height int }{
		{200, 40},
		{100, 24},
		{56, 6},
		{20, 5},

		// Narrower than the box spends on its own border and gutter, which is the
		// width every piece of the arithmetic goes negative at.
		{6, 4},
	}

	body := strings.Repeat("a body long enough to wrap more than once. ", 8)

	for _, size := range sizes {
		m := compose.New(theme.RosePineMoon)
		m.SetSize(size.width, size.height)
		m.Open("Session note", body)

		w, h := lipgloss.Size(m.View())
		if w > size.width || h > size.height {
			t.Errorf("%dx%d drew a %dx%d box", size.width, size.height, w, h)
		}
	}

	// A textarea has a size of its own, so a box opened before the first resize
	// would draw at that size on a frame with no room reported for it yet.
	unsized := compose.New(theme.RosePineMoon)
	unsized.Open("Session note", body)

	if got := unsized.View(); got != "" {
		t.Errorf("a box drew before the frame had a size:\n%s", got)
	}
}

// TestAClosedBoxDrawsNothing, so the root can render it without asking first.
func TestAClosedBoxDrawsNothing(t *testing.T) {
	m := compose.New(theme.RosePineMoon)
	m.SetSize(100, 24)

	if got := m.View(); got != "" {
		t.Errorf("a closed box drew %q", got)
	}

	m.Open("Session note", "something")
	m.Close()

	if got := m.View(); got != "" {
		t.Errorf("a box closed again drew %q", got)
	}
	if got := m.Value(); got != "" {
		t.Errorf("the discarded body is still there: %q", got)
	}
}

// TestTheCursorStaysInsideTheFrame. A box on a frame with no room for its own
// chrome would put the terminal's cursor off the screen, where it parks anywhere.
func TestTheCursorStaysInsideTheFrame(t *testing.T) {
	m := compose.New(theme.RosePineMoon)
	m.SetSize(2, 2)
	m.Open("Comment on a.go:1", "hi")

	if c := m.TypingAt(); c != nil {
		t.Errorf("the cursor is at %d,%d on a 2x2 frame", c.X, c.Y)
	}
}
