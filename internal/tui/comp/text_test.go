package comp_test

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/zen-review/zen-review/internal/tui/comp"
)

// TestWrapFoldsToWidth, which is the whole contract a card and a listing both
// rely on. A row wider than what holds it is clipped with nothing to say it was.
func TestWrapFoldsToWidth(t *testing.T) {
	const body = "The base-side anchor is what moves here, so clamping the end loses it."

	for _, width := range []int{80, 40, 20, 8, 1} {
		for i, line := range comp.Wrap(body, width) {
			// A word wider than the space overhangs rather than breaking, so only a
			// line of more than one word has to fit.
			if len(strings.Fields(line)) > 1 && len(line) > width {
				t.Errorf("at width %d, line %d is %d wide: %q", width, i, len(line), line)
			}
		}
	}
}

// TestWrapKeepsTheBreaksSomebodyTyped. A newline is a break: it was typed into
// the box on purpose, and folding it away draws words nobody wrote in that order.
func TestWrapKeepsTheBreaksSomebodyTyped(t *testing.T) {
	body := "one two three\nfour five six"

	want := []string{"one two three", "four five six"}
	if got := comp.Wrap(body, 80); !slices.Equal(got, want) {
		t.Errorf("Wrap gave %q, want the two lines kept apart", got)
	}
}

// TestWrapKeepsWhatBeginsSomething. Folding a bullet into the paragraph above
// turns a list into a sentence.
func TestWrapKeepsWhatBeginsSomething(t *testing.T) {
	body := "why this matters:\n- the first\n- the second"

	want := []string{"why this matters:", "- the first", "- the second"}
	if got := comp.Wrap(body, 80); !slices.Equal(got, want) {
		t.Errorf("Wrap gave %q, want the list kept apart", got)
	}
}

// TestWrapPutsAnIndentBackOnEveryLineItFoldsInto. Taken off once, a long
// indented line comes back reading as the prose around it.
func TestWrapPutsAnIndentBackOnEveryLineItFoldsInto(t *testing.T) {
	body := "    a run of indented words that has to fold more than once"

	got := comp.Wrap(body, 24)
	if len(got) < 2 {
		t.Fatalf("Wrap gave %q, want more than one line", got)
	}
	for i, line := range got {
		if !strings.HasPrefix(line, "    ") {
			t.Errorf("line %d lost the indent: %q", i, line)
		}
	}
}

// TestWrapKeepsABlankLine, so two paragraphs do not run together into one.
func TestWrapKeepsABlankLine(t *testing.T) {
	got := comp.Wrap("first\n\nsecond", 80)
	if !slices.Equal(got, []string{"first", "", "second"}) {
		t.Errorf("Wrap gave %q, want the blank between them", got)
	}
}

// TestWrapMeasuresCellsAndNotRunes. A rune can take two columns, and counting
// runes calls a line that renders past the pane a line that fits.
func TestWrapMeasuresCellsAndNotRunes(t *testing.T) {
	for i, line := range comp.Wrap("界 界 界", 4) {
		if got := lipgloss.Width(line); got > 4 {
			t.Errorf("line %d is %d cells wide: %q", i, got, line)
		}
	}
}
