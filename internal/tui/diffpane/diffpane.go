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
	"github.com/zen-kit/zen-kit/syntax"
	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// KeyMap is what the pane answers to.
//
// The half-page keys are the pane's rather than the shared movement's, because
// they page the diff from either pane. The reader walking the tree is reading
// the diff beside it, and a key that paged the tree under the cursor would take
// them somewhere they did not ask to go.
type KeyMap struct {
	comp.Movement

	HalfUp   key.Binding
	HalfDown key.Binding
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		Movement: comp.NewMovement(),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
	}
}

// Scrolling is the keys that page the diff, which answer from either pane.
func (k KeyMap) Scrolling() []key.Binding {
	return []key.Binding{k.HalfDown, k.HalfUp}
}

// Hints is what the status bar says this pane can do while it holds the keys.
func (k KeyMap) Hints() []key.Binding {
	return []key.Binding{comp.Pair(k.Down, k.Up, "j/k", "scroll")}
}

// Paging is the half-page keys as the status bar names them, which is once and
// by what they move. They answer from either pane, so the bar carries them
// whichever has focus, and over the tree "half page down" would not say of what.
//
// The overlay lists the two separately and says the direction, because there it
// sits under the pane it belongs to and has the room.
func (k KeyMap) Paging() key.Binding {
	return comp.Pair(k.HalfDown, k.HalfUp, "ctrl+d/u", "page the diff")
}

// Model is the diff pane. It renders the file it was given and holds no review
// state of its own.
type Model struct {
	Keys KeyMap

	theme   theme.Theme
	painter paint.Painter
	syntax  syntax.Syntax

	file *review.File
	rows []string

	offset int

	width  int
	height int
}

// New is an empty pane. A changeset with no files leaves it that way.
//
// The theme names the Chroma style. A name Chroma does not know still yields a
// working colorizer, so a palette naming one degrades to different colours
// rather than to no diff, and there is nothing here worth failing to open over.
func New(t theme.Theme) Model {
	s, _ := syntax.New(t.Syntax)

	return Model{
		Keys:    NewKeyMap(),
		theme:   t,
		painter: paint.Painter{Theme: t},
		syntax:  s,
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

// Scroll is where the window sits in the file, for the counter the frame draws.
func (m Model) Scroll() comp.Scroll {
	return comp.Scroll{Offset: m.offset, Height: m.height, Total: len(m.rows)}
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
		m.scroll(-len(m.rows))
	case key.Matches(press, m.Keys.Bottom):
		m.scroll(len(m.rows))
	}
	return m, nil
}

func (m Model) View() string {
	out := make([]string, 0, m.height)
	for i := m.offset; i < len(m.rows) && len(out) < m.height; i++ {
		out = append(out, m.rows[i])
	}

	blank := strings.Repeat(" ", max(0, m.width))
	for len(out) < m.height {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// relayout repaints the file at the current width.
//
// The rows are built here rather than in View, because tokenising is where the
// syntax cache is written and View has to stay a pure read of the model.
func (m *Model) relayout() {
	m.rows = nil
	if m.file == nil || m.width <= 0 {
		return
	}

	tokens := m.tokens(*m.file)
	gutter := paint.Gutter(widest(*m.file))

	seen := 0
	for i, h := range m.file.Hunks {
		if i > 0 {
			m.add("")
		}
		m.add(m.painter.HunkHeader(comp.Safe(h.Diff.Header), gutter, m.width))

		for _, l := range h.Diff.Lines {
			m.add(m.painter.Line(paint.Line{
				Kind:   kindOf(l.Kind),
				Old:    l.Old,
				New:    l.New,
				Tokens: tokens[seen],
			}, gutter, m.width))
			seen++

			// The annotation takes no line number of its own, so it hangs under
			// the line it was written about. Without it a file that lost its
			// trailing newline shows a removal and an addition of the same text.
			if l.NoEOL {
				m.add(m.note("\\ No newline at end of file"))
			}
		}
	}

	if len(m.file.Hunks) == 0 {
		m.add(m.note(emptyReason(*m.file)))
	}
	m.clampOffset()
}

// add takes a painted line, finishing it to the pane's width.
//
// The painter leaves a row that fits short, because a row it tinted runs its
// own background out and a context row does not. The pane is the one that knows
// how wide it is, so it is the one that finishes the line.
func (m *Model) add(text string) {
	if gap := m.width - lipgloss.Width(text); gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	m.rows = append(m.rows, text)
}

// tokens colours the file one side at a time, and hands back one entry per diff
// line in the order the hunks hold them.
//
// A lexer carries state across lines, so highlighting line by line comes apart
// on the first multi-line string, and running the two sides together would feed
// it a file holding both halves of every change. A context line goes into both
// sides so neither reads as source with its unchanged lines missing, and takes
// its colour from the head.
//
// Each side is lexed by the name it has on that side. A rename that changes the
// extension is a different language on the base, and lexing its removals as the
// head's colours them by a grammar they were never written in.
func (m *Model) tokens(f review.File) [][]syntax.Token {
	// at is where one diff line landed: which side's body, and which line of it.
	type at struct {
		base bool
		i    int
	}

	var oldSrc, newSrc []string
	var index []at

	for _, h := range f.Hunks {
		for _, l := range h.Diff.Lines {
			text := comp.Code(l.Text)
			switch l.Kind {
			case diff.Removed:
				index = append(index, at{base: true, i: len(oldSrc)})
				oldSrc = append(oldSrc, text)
			case diff.Added:
				index = append(index, at{i: len(newSrc)})
				newSrc = append(newSrc, text)
			default:
				index = append(index, at{i: len(newSrc)})
				oldSrc = append(oldSrc, text)
				newSrc = append(newSrc, text)
			}
		}
	}

	oldTok := m.syntax.Lines(basePath(f.Diff), strings.Join(oldSrc, "\n"))
	newTok := m.syntax.Lines(f.Diff.Path, strings.Join(newSrc, "\n"))

	out := make([][]syntax.Token, len(index))
	for i, a := range index {
		src := newTok
		if a.base {
			src = oldTok
		}
		if a.i < len(src) {
			out[i] = src[a.i]
		}
	}
	return out
}

// basePath is the name the file has on the base side, which a rename or a copy
// makes a different one from its own.
func basePath(f diff.File) string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}

// note is a line about the file rather than a line of it, for a file the
// painter has nothing to draw.
func (m Model) note(text string) string {
	style := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	return comp.Clip(style.Render(text), m.width, style)
}

func (m *Model) scroll(by int) {
	m.offset += by
	m.clampOffset()
}

func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, max(len(m.rows)-m.height, 0)))
}

// half is how far ctrl+u and ctrl+d go, and never zero on a pane too short to
// halve.
func (m Model) half() int {
	return max(m.height/2, 1)
}

// kindOf is a parsed line's kind as the painter names it. The two packages name
// the same three things and neither has to agree with the other on a value:
// diff.Kind is a string the JSON output prints, and paint.Kind is an iota.
func kindOf(k diff.Kind) paint.Kind {
	switch k {
	case diff.Added:
		return paint.Added
	case diff.Removed:
		return paint.Removed
	default:
		return paint.Context
	}
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
