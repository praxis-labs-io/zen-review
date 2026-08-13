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
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// The marker a hunk header wears for the state it is in. Both are drawn, not
// only the folded one: a header that changes width between the two states
// shifts its own text sideways as the key is pressed.
const (
	openMark   = "▾ "
	foldedMark = "▸ "
)

// KeyMap is what the pane answers to.
type KeyMap struct {
	comp.Movement
	Fold key.Binding
}

// NewKeyMap is the bindings and the help text they carry.
func NewKeyMap() KeyMap {
	return KeyMap{
		Movement: comp.NewMovement(),
		Fold:     key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "fold hunk")),
	}
}

// Bindings is the pane's own keys, without the movement it shares.
func (k KeyMap) Bindings() []key.Binding {
	return []key.Binding{k.Fold}
}

// fold names a hunk the reader folded away.
//
// It is the hunk's own name, the side and line it introduces, rather than its
// place in the file: an agent inserting a hunk above a folded one would
// otherwise leave the fold sitting on different code wearing the same number.
type fold struct {
	path string
	side store.Side
	line int
}

// row is one painted line and the hunk it came out of. A row belonging to no
// hunk carries noHunk, which is what a file the painter has nothing to draw
// leaves behind.
type row struct {
	text string
	hunk int
}

const noHunk = -1

// Model is the diff pane. It renders the file it was given and holds no review
// state of its own.
type Model struct {
	Keys KeyMap

	theme   theme.Theme
	painter paint.Painter
	syntax  syntax.Syntax

	file *review.File
	rows []row

	// headers is where each hunk's header landed, so folding can put it back on
	// the top row without walking the rows to find it.
	headers []int

	folded map[fold]bool

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
		folded:  make(map[fold]bool),
	}
}

// SetFile puts a file in the pane and takes the reader back to the top of it.
//
// A nil file empties the pane, which is what a changeset with nothing in it
// looks like. Folds survive, so a file the reader walks away from and comes
// back to is the shape they left it.
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
	case key.Matches(press, m.Keys.Fold):
		m.toggleFold()
	case key.Matches(press, m.Keys.Down):
		m.scroll(1)
	case key.Matches(press, m.Keys.Up):
		m.scroll(-1)
	case key.Matches(press, m.Keys.HalfDown):
		m.scroll(m.half())
	case key.Matches(press, m.Keys.HalfUp):
		m.scroll(-m.half())
	case key.Matches(press, m.Keys.Top):
		m.offset = 0
	case key.Matches(press, m.Keys.Bottom):
		// The end of the file, not the end of the scroll. The room past it is
		// there so a hunk can reach the top row, and G is not going to a hunk.
		m.offset = max(len(m.rows)-m.height, 0)
	}
	return m, nil
}

func (m Model) View() string {
	out := make([]string, 0, m.height)
	for i := m.offset; i < len(m.rows) && len(out) < m.height; i++ {
		out = append(out, m.rows[i].text)
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
	m.rows, m.headers = nil, nil
	if m.file == nil || m.width <= 0 {
		return
	}

	tokens := m.tokens(*m.file)
	gutter := paint.Gutter(widest(*m.file))

	seen := 0
	for i, h := range m.file.Hunks {
		if i > 0 {
			// The blank belongs to the hunk it introduces, so a fold reaches the
			// hunk below whichever of the two the window opens on.
			m.add(i, "")
		}

		folded := m.folded[m.foldOf(h)]
		m.headers = append(m.headers, len(m.rows))
		m.add(i, m.painter.HunkHeader(mark(folded)+comp.Safe(h.Diff.Header), gutter, m.width))

		for _, l := range h.Diff.Lines {
			line := tokens[seen]
			seen++
			if folded {
				continue
			}
			m.add(i, m.painter.Line(paint.Line{
				Kind:   kindOf(l.Kind),
				Old:    l.Old,
				New:    l.New,
				Tokens: line,
			}, gutter, m.width))
		}
	}

	if len(m.file.Hunks) == 0 {
		m.add(noHunk, m.note(emptyReason(*m.file)))
	}
	m.clampOffset()
}

// add takes a painted line, finishing it to the pane's width.
//
// The painter leaves a row that fits short, because a row it tinted runs its
// own background out and a context row does not. The pane is the one that knows
// how wide it is, so it is the one that finishes the line.
func (m *Model) add(hunk int, text string) {
	if gap := m.width - lipgloss.Width(text); gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	m.rows = append(m.rows, row{text: text, hunk: hunk})
}

// tokens colours the file one side at a time, and hands back one entry per diff
// line in the order the hunks hold them.
//
// A lexer carries state across lines, so highlighting line by line comes apart
// on the first multi-line string, and running the two sides together would feed
// it a file holding both halves of every change. A context line goes into both
// sides so neither reads as source with its unchanged lines missing, and takes
// its colour from the head.
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

	oldTok := m.syntax.Lines(f.Diff.Path, strings.Join(oldSrc, "\n"))
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

// toggleFold turns the hunk the window opens in inside out and puts its header
// back on the top row.
//
// The pane has no cursor of its own yet, so the hunk the reader is looking at
// is whatever the top of the window sits in. The ring gives it one, and this
// moves onto it then.
func (m *Model) toggleFold() {
	if m.file == nil || m.offset >= len(m.rows) {
		return
	}

	i := m.rows[m.offset].hunk
	if i == noHunk {
		return
	}

	k := m.foldOf(m.file.Hunks[i])
	if m.folded[k] {
		delete(m.folded, k)
	} else {
		m.folded[k] = true
	}

	m.relayout()
	m.offset = m.headers[i]
	m.clampOffset()
}

// foldOf is the key a hunk of the file in the pane is folded under.
func (m Model) foldOf(h review.Hunk) fold {
	side, line := h.Name()
	return fold{path: m.Path(), side: side, line: line}
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
	m.offset = max(0, min(m.offset, m.maxOffset()))
}

// maxOffset is how far the pane scrolls.
//
// It runs past the end of the file far enough for the last hunk's header to
// reach the top row. Every hunk has to be able to get there: folding puts the
// hunk it acted on at the top, and space reads the hunk back off that row, so a
// last hunk that cannot top out would fold on one press and unfold a different
// hunk on the next.
//
// A file that fits on the pane does not scroll at all. There is nowhere to go
// and every header is already on screen.
func (m Model) maxOffset() int {
	if len(m.rows) <= m.height {
		return 0
	}
	last := 0
	if len(m.headers) > 0 {
		last = m.headers[len(m.headers)-1]
	}
	return max(len(m.rows)-m.height, last)
}

// half is how far ctrl+u and ctrl+d go, and never zero on a pane too short to
// halve.
func (m Model) half() int {
	return max(m.height/2, 1)
}

func mark(folded bool) string {
	if folded {
		return foldedMark
	}
	return openMark
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
