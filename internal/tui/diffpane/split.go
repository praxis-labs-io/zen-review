package diffpane

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/paint"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
)

const splitCodeMin = 28

const splitRule = "│"

// ToggleSplit flips side-by-side and returns 0, or how many columns short the pane is when it refuses to turn on.
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

func (m Model) splitShort() int {
	want := 2*(paint.HalfColumn(m.gutter)+splitCodeMin) + lipgloss.Width(splitRule)
	return max(0, want-m.width)
}

func (m Model) splitting() bool { return m.split && m.splitShort() == 0 }

func (m Model) scope() store.Side {
	if !m.splitting() {
		return ""
	}
	return m.side
}

// Column moves the cursor into the named column, and false unless split and not already there.
func (m *Model) Column(to store.Side) bool {
	if !m.splitting() || m.side == to {
		return false
	}

	m.side = to
	if m.cursor >= 0 {
		hunk := m.hunkAt(m.cursor)
		at := m.seek(m.cursor, 1)
		if m.hunkAt(at) != hunk {
			if back := m.seek(m.cursor, -1); m.hunkAt(back) == hunk {
				at = back
			}
		}
		m.moveTo(at)
	}
	m.repaintAll()
	return true
}

func (m Model) reachable(i int) bool {
	if i < 0 || i >= len(m.rows) || m.blank(i) {
		return false
	}
	if m.scope() == "" || m.rows[i].kind != codeRow {
		return true
	}

	l := m.rows[i].right
	if m.scope() == store.SideBase {
		l = m.rows[i].line
	}
	return l.Old != 0 || l.New != 0
}

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

// columns gives an odd cell to the head, the side being read.
func (m Model) columns() (int, int) {
	w := max(0, m.width-lipgloss.Width(splitRule))
	return w / 2, w - w/2
}

func (m Model) codeColumn() int {
	if m.splitting() {
		return paint.HalfColumn(m.gutter)
	}
	return paint.CodeColumn(m.gutter)
}

type pair struct{ left, right int }

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

func sides(p pair) []int {
	switch {
	case p.left < 0:
		return []int{p.right}
	case p.right < 0 || p.right == p.left:
		return []int{p.left}
	}
	return []int{p.left, p.right}
}

func eol(lines []diff.Line, p pair) bool {
	for _, i := range sides(p) {
		if lines[i].NoEOL {
			return true
		}
	}
	return false
}

func (m Model) halves(r row, fill, bar color.Color, weight bool) string {
	left, right := m.columns()

	l, rt := r.line, r.right
	if m.side == store.SideBase {
		l.Fill, l.Bar, l.Weight = fill, bar, weight
	} else {
		rt.Fill, rt.Bar, rt.Weight = fill, bar, weight
	}

	rule := lipgloss.NewStyle().Foreground(m.theme.Muted)
	return m.painter.Half(l, m.gutter, left) + rule.Render(splitRule) +
		m.painter.Half(rt, m.gutter, right)
}

func (m *Model) remode() {
	had := m.cursor >= 0
	was := m.placeOf(m.cursor)
	hunk := m.hunkAt(m.cursor)
	side, line := m.at(m.cursor)
	anchorSide, anchorLine := m.anchorAt()

	m.layout()
	m.reanchor(anchorSide, anchorLine)

	at := m.rowOf(side, line)
	if was.comment != "" {
		at = m.rowAt(was)
	}

	if at < 0 && hunk >= 0 && hunk < len(m.headAt) {
		at = m.headAt[hunk]
	}
	if at < 0 && had && len(m.rows) > 0 {
		at = 0
	}

	if at < 0 {
		m.point(-1)
		return
	}
	m.moveTo(at)
}

func (m Model) anchorAt() (store.Side, int) {
	if !m.Selecting() || m.anchor.comment != "" {
		return "", 0
	}
	return m.at(m.rowAt(m.anchor))
}

// reanchor drops a selection it cannot place, because a moved one would write a comment against lines nobody picked.
func (m *Model) reanchor(side store.Side, line int) {
	if !m.Selecting() || m.anchor.comment != "" {
		return
	}

	at := -1
	if line != 0 {
		at = m.rowOf(side, line)
	}
	if at < 0 {
		m.anchor = place{seq: -1}
		return
	}
	m.anchor = m.placeOf(at)
}

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
	return store.SideBase, r.line.Old
}

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
		if side == store.SideBase && r.line.Old == line {
			return i
		}
	}
	return -1
}
