package comp_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/tui/comp"
)

func pane() comp.Pane { return comp.NewPane(theme.RosePineMoon) }

// strip is the frame as a golden would hold it: the box drawn, the colour gone.
func strip(rendered string) []string {
	return strings.Split(ansi.Strip(rendered), "\n")
}

// TestTheBoxIsExactlyTheSizeItWasGiven. A pane clips overflow silently, so a
// row that outruns it takes the border with it and nothing says so.
func TestTheBoxIsExactlyTheSizeItWasGiven(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		content       string
	}{
		{"empty", 30, 6, ""},
		{"short", 30, 6, "one\ntwo"},
		{"taller than the pane", 30, 4, strings.Repeat("row\n", 20)},
		{"wider than the pane", 30, 4, strings.Repeat("wide ", 20)},
		{"one row of content", 12, 3, "hello"},
		{"tall and narrow", 4, 20, "a\nb\nc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strip(pane().Size(tt.width, tt.height).
				Index(1).Title("a title").Footer("41/942").Render(tt.content))

			if len(lines) != tt.height {
				t.Fatalf("drew %d lines, want %d:\n%s", len(lines), tt.height, strings.Join(lines, "\n"))
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != tt.width {
					t.Errorf("line %d is %d columns, want %d: %q", i, got, tt.width, line)
				}
			}
		})
	}
}

// TestTheBorderCarriesTheIndexAndTheTitle, flush against the left corner.
func TestTheBorderCarriesTheIndexAndTheTitle(t *testing.T) {
	top := strip(pane().Size(30, 4).Index(2).Title("Files (7)").Render(""))[0]

	if want := "╭─[2]─Files (7)"; !strings.HasPrefix(top, want) {
		t.Errorf("the top border is %q, want it to start %q", top, want)
	}
	if !strings.HasSuffix(top, "╮") {
		t.Errorf("the top border does not close: %q", top)
	}
}

// TestTheHeadingAnswersToFocus. A lit border under a dim name reads as two
// panes half-focused rather than one focused pane, so the index, the title and
// the border move together.
func TestTheHeadingAnswersToFocus(t *testing.T) {
	t2 := theme.RosePineMoon

	lit := map[string]string{
		"corner": lipgloss.NewStyle().Foreground(t2.Accent).Render("╭"),
		"index":  lipgloss.NewStyle().Foreground(t2.Accent).Render("[1]"),
		"title":  lipgloss.NewStyle().Foreground(t2.Text).Bold(true).Render("Files (7)"),
	}
	dim := map[string]string{
		"corner": lipgloss.NewStyle().Foreground(t2.BorderSubtleOrBorder()).Render("╭"),
		"index":  lipgloss.NewStyle().Foreground(t2.Muted).Render("[1]"),
		"title":  lipgloss.NewStyle().Foreground(t2.Subtle).Render("Files (7)"),
	}

	for _, tt := range []struct {
		name    string
		focused bool
		want    map[string]string
		gone    map[string]string
	}{
		{"focused", true, lit, dim},
		{"blurred", false, dim, lit},
	} {
		t.Run(tt.name, func(t *testing.T) {
			top := strings.Split(pane().Size(30, 4).Index(1).Title("Files (7)").
				Focus(tt.focused).Render(""), "\n")[0]

			for part, want := range tt.want {
				if !strings.Contains(top, want) {
					t.Errorf("the %s does not answer to focus = %v", part, tt.focused)
				}
			}
			for part, unwanted := range tt.gone {
				if strings.Contains(top, unwanted) {
					t.Errorf("the %s is still drawn for focus = %v", part, !tt.focused)
				}
			}
		})
	}
}

// TestANarrowPaneClipsItsTitle rather than pushing the corner off the frame.
func TestANarrowPaneClipsItsTitle(t *testing.T) {
	long := "internal/tui/diffpane/painting_the_unified_view.go"
	top := strip(pane().Size(20, 4).Index(1).Title(long).Render(""))[0]

	if lipgloss.Width(top) != 20 {
		t.Fatalf("the top border is %d columns: %q", lipgloss.Width(top), top)
	}
	if strings.Contains(top, long) {
		t.Errorf("the title was not clipped: %q", top)
	}
	if !strings.HasSuffix(top, "╮") {
		t.Errorf("the clipped title took the corner with it: %q", top)
	}
}

// TestTheFooterSitsInTheBottomBorder, one rune in from the corner.
func TestTheFooterSitsInTheBottomBorder(t *testing.T) {
	lines := strip(pane().Size(30, 4).Footer("41/942").Render(""))
	bottom := lines[len(lines)-1]

	if want := "41/942─╯"; !strings.HasSuffix(bottom, want) {
		t.Errorf("the bottom border is %q, want it to end %q", bottom, want)
	}

	plain := strip(pane().Size(30, 4).Render(""))
	if got := plain[len(plain)-1]; strings.ContainsAny(got, "0123456789") {
		t.Errorf("a pane with no footer drew one: %q", got)
	}
}

// TestAPaneWithNoRoomDrawsNothing. Two borders is the whole of a pane at that
// size, and a caller appending an empty string spends no line on it.
func TestAPaneWithNoRoomDrawsNothing(t *testing.T) {
	for _, size := range []struct{ width, height int }{{1, 10}, {10, 1}, {0, 0}} {
		if got := pane().Size(size.width, size.height).Render("content"); got != "" {
			t.Errorf("%dx%d drew %q", size.width, size.height, got)
		}
	}
}

// TestTheScrollCounterStaysOffWhenTheContentFits. A counter on content that
// already fits is noise.
func TestTheScrollCounterStaysOffWhenTheContentFits(t *testing.T) {
	tests := []struct {
		name   string
		scroll comp.Scroll
		want   string
	}{
		{"fits exactly", comp.Scroll{Offset: 0, Height: 20, Total: 20}, ""},
		{"shorter than the pane", comp.Scroll{Offset: 0, Height: 20, Total: 3}, ""},
		{"at the top", comp.Scroll{Offset: 0, Height: 20, Total: 942}, "20/942"},
		{"partway down", comp.Scroll{Offset: 21, Height: 20, Total: 942}, "41/942"},
		{"at the bottom", comp.Scroll{Offset: 922, Height: 20, Total: 942}, "942/942"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scroll.Footer(); got != tt.want {
				t.Errorf("Footer() = %q, want %q", got, tt.want)
			}
		})
	}
}
