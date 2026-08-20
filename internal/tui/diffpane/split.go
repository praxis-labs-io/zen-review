package diffpane

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
)

// splitCodeMin is the narrowest column of source side-by-side is offered at.
// Under it the two halves clip away more than they show.
const splitCodeMin = 28

// splitRule divides the two columns. Two untinted context halves have no tint
// between them saying where one ends.
const splitRule = "│"

// ToggleSplit turns side-by-side on or off, and returns how many columns short
// the pane is when it refuses. Turning it off always takes.
//
// A pane too narrow keeps the answer and draws unified, so widening brings it
// back without a second press.
func (m *Model) ToggleSplit() int {
	if !m.split {
		if short := m.splitShort(); short > 0 {
			return short
		}
	}

	m.split = !m.split
	m.remode()
	m.reveal()
	return 0
}

// splitShort is how many more columns the pane needs for two halves, and 0 when
// it has them.
func (m Model) splitShort() int {
	want := 2*(paint.HalfColumn(m.gutter)+splitCodeMin) + lipgloss.Width(splitRule)
	return max(0, want-m.width)
}

// splitting is whether the pane draws side-by-side, which takes both the
// reader's answer and the width to honour it.
func (m Model) splitting() bool { return m.split && m.splitShort() == 0 }

// scope is the column the cursor is in, which a comment and a selection are read
// on. Empty is a unified row, which names both.
func (m Model) scope() store.Side {
	if !m.splitting() {
		return ""
	}
	return m.side
}

// NextColumn moves the cursor into the head column, and PrevColumn back into the
// base. Both report whether they took the key: only a split pane has anywhere to go.
func (m *Model) NextColumn() bool { return m.toSide(store.SideHead) }

// PrevColumn is NextColumn the other way, which is what h does inside the pane.
func (m *Model) PrevColumn() bool { return m.toSide(store.SideBase) }

// toSide lights the other column and brings the cursor onto a row that has a
// line in it. A pane already there has not taken the key and lets focus move on.
func (m *Model) toSide(to store.Side) bool {
	if !m.splitting() || m.side == to {
		return false
	}

	m.side = to
	if m.cursor >= 0 {
		m.moveTo(m.cursor)
	}
	m.repaintAll()
	return true
}

// has is whether a half carries a line. A blank one has no number on either
// field, which no real line does.
func has(l paint.Line) bool { return l.Old != 0 || l.New != 0 }

// focused is the half the cursor is in, and the whole row when unified.
func (m Model) focused(r row) paint.Line {
	if m.scope() == store.SideBase {
		return r.line
	}
	return r.right
}

// reachable is whether the cursor can sit on a row: not the blank between two
// hunks, and not a column this row has no line in.
func (m Model) reachable(i int) bool {
	if i < 0 || i >= len(m.rows) || m.blank(i) {
		return false
	}
	if m.scope() == "" || m.rows[i].kind != codeRow {
		return true
	}
	return has(m.focused(m.rows[i]))
}

// seek is the first row from i in a direction the cursor can sit on. A run
// reaching the end of the file turns back, rather than stranding it on a blank.
func (m Model) seek(from, by int) int {
	for i := from; i >= 0 && i < len(m.rows); i += by {
		if m.reachable(i) {
			return i
		}
	}
	for i := from; i >= 0 && i < len(m.rows); i -= by {
		if m.reachable(i) {
			return i
		}
	}
	return from
}

// columns is the width of each side-by-side column, the rule between them taken
// off the pane first. An odd cell goes to the head, that being the side read.
func (m Model) columns() (int, int) {
	w := max(0, m.width-lipgloss.Width(splitRule))
	return w / 2, w - w/2
}

// codeColumn is where a row's source starts, which a heading indents to and a
// card hangs under.
func (m Model) codeColumn() int {
	if m.splitting() {
		return paint.HalfColumn(m.gutter)
	}
	return paint.CodeColumn(m.gutter)
}

// pair is the diff lines one row draws, by index into a hunk's own. -1 is a
// column with no line, which only side-by-side has.
type pair struct{ left, right int }

// pairs is the rows a hunk draws as. Unified is one line per row; split puts a
// run of removals against the additions after it and pads the shorter side.
func pairs(lines []diff.Line, split bool) []pair {
	if !split {
		out := make([]pair, len(lines))
		for i := range lines {
			out[i] = pair{left: i, right: -1}
		}
		return out
	}

	out := make([]pair, 0, len(lines))
	var rem, add []int

	flush := func() {
		for i := range max(len(rem), len(add)) {
			p := pair{left: -1, right: -1}
			if i < len(rem) {
				p.left = rem[i]
			}
			if i < len(add) {
				p.right = add[i]
			}
			out = append(out, p)
		}
		rem, add = nil, nil
	}

	for i, l := range lines {
		switch l.Kind {
		case diff.Removed:
			// Additions already banked close the run, so a removal after them opens
			// a new one rather than pairing against a block it did not replace.
			if len(add) > 0 {
				flush()
			}
			rem = append(rem, i)
		case diff.Added:
			add = append(add, i)
		default:
			flush()
			out = append(out, pair{left: i, right: i})
		}
	}
	flush()

	return out
}

// code is one row of source. A unified row carries both numbers on one line; a
// split one carries the base number left and the head number right.
func (m Model) code(lines []diff.Line, p pair, tokens [][]syntax.Token, hunk int, split bool) row {
	r := row{kind: codeRow, hunk: hunk}

	if p.left >= 0 {
		l := lines[p.left]
		r.line = paint.Line{Kind: kindOf(l.Kind), Old: l.Old, New: l.New, Tokens: tokens[p.left]}
		if split {
			r.line.New = 0
		}
	}
	if p.right >= 0 {
		l := lines[p.right]
		r.right = paint.Line{Kind: kindOf(l.Kind), New: l.New, Tokens: tokens[p.right]}
	}
	return r
}

// sides is the lines a row draws, the base column first, so a comment scoped to
// either is placed under the row holding it. A context line is named once.
func sides(p pair) []int {
	switch {
	case p.left < 0:
		return []int{p.right}
	case p.right < 0 || p.right == p.left:
		return []int{p.left}
	}
	return []int{p.left, p.right}
}

// eol is whether either column of a row lost its trailing newline.
func eol(lines []diff.Line, p pair) bool {
	for _, i := range sides(p) {
		if lines[i].NoEOL {
			return true
		}
	}
	return false
}

// halves is one side-by-side row: the two columns and the rule between them.
//
// Only the column the cursor is in takes the fill. Lighting both would leave a
// reader on a rewritten line with nothing saying which side the next key takes.
// The rule never lights either, or the lit block runs a cell past its column.
func (m Model) halves(r row, fill color.Color) string {
	left, right := m.columns()

	l, rt := r.line, r.right
	if m.side == store.SideBase {
		l.Fill = fill
	} else {
		rt.Fill = fill
	}

	rule := lipgloss.NewStyle().Foreground(m.theme.Muted)
	return m.painter.Half(l, m.gutter, left) + rule.Render(splitRule) +
		m.painter.Half(rt, m.gutter, right)
}

// remode rebuilds the rows for a change of mode and puts the cursor back on the
// line it was on. seq counts rows, and the two modes do not have the same ones.
func (m *Model) remode() {
	had := m.cursor >= 0
	was := m.placeOf(m.cursor)
	side, line := m.at(m.cursor)

	m.layout()

	at := m.rowOf(side, line)
	if was.comment != "" {
		at = m.rowAt(was)
	}
	if at < 0 && had && len(m.rows) > 0 {
		at = 0
	}

	// moveTo rather than point: the mode it lands in may have no line for that
	// row in the column the cursor is in.
	if at < 0 {
		m.point(-1)
		return
	}
	m.moveTo(at)
}

// at is the side and line the row at i names, and 0 for a row naming none. The
// column the cursor is in, so a toggle puts it back on the line it was reading.
func (m Model) at(i int) (store.Side, int) {
	if i < 0 || i >= len(m.rows) || m.rows[i].kind != codeRow {
		return "", 0
	}

	r := m.rows[i]
	if m.scope() == store.SideBase {
		return store.SideBase, r.line.Old
	}
	if n := max(r.line.New, r.right.New); n != 0 {
		return store.SideHead, n
	}
	return store.SideBase, max(r.line.Old, r.right.Old)
}

// rowOf is the row naming a line on a side, and -1 for one this layout has not
// got. It is how a cursor crosses a change of mode.
func (m Model) rowOf(side store.Side, line int) int {
	if line == 0 {
		return -1
	}

	for i := range m.rows {
		if m.rows[i].kind != codeRow {
			continue
		}
		r := m.rows[i]
		if side == store.SideHead && (r.line.New == line || r.right.New == line) {
			return i
		}
		if side == store.SideBase && (r.line.Old == line || r.right.Old == line) {
			return i
		}
	}
	return -1
}
