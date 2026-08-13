// Package diffpane is the right pane: the selected file's diff.
//
// It draws through zen-kit's painter, so this tool and zen-octo render a diff
// the same way and neither grows a second line painter.
package diffpane

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/paint"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// KeyMap is what the pane answers to. It is the shared movement and nothing
// else until folding lands.
type KeyMap struct {
	comp.Movement
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{Movement: comp.NewMovement()}
}

// Model is the diff pane. It renders the file it was given and holds no review
// state of its own.
type Model struct {
	Keys KeyMap

	theme   theme.Theme
	painter paint.Painter

	file  *review.File
	lines []string

	offset int

	width  int
	height int
}

// New is an empty pane. A changeset with no files leaves it that way.
func New(t theme.Theme) Model {
	return Model{
		Keys:    NewKeyMap(),
		theme:   t,
		painter: paint.Painter{Theme: t},
	}
}

// SetFile puts a file in the pane and takes the reader back to the top of it.
//
// A nil file empties the pane, which is what a changeset with nothing in it
// looks like.
func (m *Model) SetFile(f *review.File) {
	m.file, m.offset = f, 0
	m.relayout()
}

// SetSize gives the pane the room it draws into, which is the inside of the
// frame and not the frame.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.relayout()
}

// Path is the file in the pane, and is empty when there is none.
func (m Model) Path() string {
	if m.file == nil {
		return ""
	}
	return m.file.Diff.Path
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch {
	case key.Matches(press, m.Keys.Down):
		m.scroll(1)
	case key.Matches(press, m.Keys.Up):
		m.scroll(-1)
	case key.Matches(press, m.Keys.HalfDown):
		m.scroll(m.half())
	case key.Matches(press, m.Keys.HalfUp):
		m.scroll(-m.half())
	case key.Matches(press, m.Keys.Top):
		m.scroll(-len(m.lines))
	case key.Matches(press, m.Keys.Bottom):
		m.scroll(len(m.lines))
	}
	return m, nil
}

func (m Model) View() string {
	out := make([]string, 0, m.height)
	for i := m.offset; i < len(m.lines) && len(out) < m.height; i++ {
		out = append(out, m.lines[i])
	}

	blank := strings.Repeat(" ", max(0, m.width))
	for len(out) < m.height {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// relayout repaints the file at the current width.
//
// The rows are built here rather than in View, because painting is where the
// syntax cache is written and View has to stay a pure read of the model.
func (m *Model) relayout() {
	m.lines = nil
	if m.file == nil || m.width <= 0 {
		return
	}

	gutter := paint.Gutter(widest(*m.file))
	for i, h := range m.file.Hunks {
		if i > 0 {
			m.lines = append(m.lines, "")
		}
		m.lines = append(m.lines, m.painter.HunkHeader(comp.Safe(h.Diff.Header), gutter, m.width))
	}

	if len(m.file.Hunks) == 0 {
		m.lines = append(m.lines, m.note(emptyReason(*m.file)))
	}

	// The painter leaves a row that fits short, because a row it tinted runs its
	// own background out and a context row does not. The pane is the one that
	// knows how wide it is, so it is the one that finishes the line.
	for i, line := range m.lines {
		if gap := m.width - lipgloss.Width(line); gap > 0 {
			m.lines[i] = line + strings.Repeat(" ", gap)
		}
	}
	m.clampOffset()
}

// note is a line about the file rather than a line of it, for a file the
// painter has nothing to draw.
func (m Model) note(text string) string {
	style := lipgloss.NewStyle().Foreground(m.theme.Faint)
	return comp.Clip(style.Render(text), m.width, style)
}

func (m *Model) scroll(by int) {
	m.offset += by
	m.clampOffset()
}

func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, max(len(m.lines)-m.height, 0)))
}

// half is how far ctrl+u and ctrl+d go, and never zero on a pane too short to
// halve.
func (m Model) half() int {
	return max(m.height/2, 1)
}

// widest is the highest line number the file reaches, which is what sizes the
// gutter. Both columns take the same width so the marker between them does not
// move from one row to the next.
//
// Start plus Lines is the line after the hunk, not its last, and a hunk ending
// at 99 sized off 100 buys a third column the file never fills. An empty range
// has Start 0 and no last line to name.
func widest(f review.File) int {
	n := 0
	for _, h := range f.Hunks {
		n = max(n, last(h.Diff.OldStart, h.Diff.OldLines), last(h.Diff.NewStart, h.Diff.NewLines))
	}
	return n
}

func last(start, lines int) int {
	if lines == 0 {
		return start
	}
	return start + lines - 1
}

// emptyReason says why a file has no hunks. The parser already worked it out
// for a binary or an oversized one; a file with neither has no changed lines.
func emptyReason(f review.File) string {
	if f.Diff.Omitted != "" {
		return f.Diff.Omitted
	}
	return "no changed lines"
}
