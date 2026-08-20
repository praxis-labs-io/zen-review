package diffpane

import (
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// Selecting is whether v has a range open, which is what the status bar reads to
// name the keys that end one.
func (m Model) Selecting() bool { return m.anchor.comment != "" || m.anchor.seq >= 0 }

// Selected is the lines under the selection, one span per side, and false when
// there is none. review clips these to the lines the file's hunks hold.
func (m Model) Selected() ([]review.Anchor, bool) {
	lo, hi, ok := m.span()
	if !ok {
		return nil, false
	}
	return anchorsOver(m.rows[lo : hi+1])
}

// Line is the anchors the cursor's own row names, and false on a row that is
// not code. A context row names a line on both sides.
func (m Model) Line() ([]review.Anchor, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil, false
	}
	return anchorsOver(m.rows[m.cursor : m.cursor+1])
}

// anchorsOver is the lines a run of rows names, one span per side and the head
// first. Both columns of a side-by-side row feed the same two spans.
func anchorsOver(rows []row) ([]review.Anchor, bool) {
	var head, base review.Range
	for i := range rows {
		if rows[i].kind != codeRow {
			continue
		}
		head = grow(head, rows[i].line.New)
		base = grow(base, rows[i].line.Old)
		head = grow(head, rows[i].right.New)
		base = grow(base, rows[i].right.Old)
	}

	var out []review.Anchor
	if head.Start != 0 {
		out = append(out, review.Anchor{Side: store.SideHead, Range: head})
	}
	if base.Start != 0 {
		out = append(out, review.Anchor{Side: store.SideBase, Range: base})
	}

	// A run over a heading or a card alone names nothing, which a caller has to
	// tell from a run of lines rather than reach for an empty list of them.
	return out, len(out) > 0
}

// grow takes one more line into a span, ignoring the 0 a row without that side
// carries. A span starting at 0 has not started, which no line number can be.
func grow(r review.Range, line int) review.Range {
	if line == 0 {
		return r
	}
	if r.Start == 0 {
		r.Start = line
	}
	r.End = line
	return r
}

// span is the rows the selection covers, lowest first, and false when there is
// none or the row v was pressed on has gone.
func (m Model) span() (int, int, bool) {
	if !m.Selecting() || m.cursor < 0 {
		return 0, 0, false
	}
	at := m.rowAt(m.anchor)
	if at < 0 {
		return 0, 0, false
	}
	return min(at, m.cursor), max(at, m.cursor), true
}

// inSelection is whether a row draws filled. Only code fills: a heading, the
// blank between two hunks and a comment card are not lines anything marks.
func (m Model) inSelection(i int) bool {
	lo, hi, ok := m.span()
	return ok && i >= lo && i <= hi && m.rows[i].kind == codeRow
}

// selectRange anchors a selection at the cursor, or takes back the one already
// open. A pane with no cursor has nothing to anchor to.
func (m *Model) selectRange() {
	if m.Selecting() {
		m.clearSelection()
		return
	}
	if m.cursor < 0 {
		return
	}
	m.anchor = m.placeOf(m.cursor)
}

// clearSelection takes the fill off the rows it lit. Nothing else moves, so the
// rows have to be told the selection went.
func (m *Model) clearSelection() {
	lo, hi, ok := m.span()
	m.anchor = place{seq: -1}

	if !ok {
		return
	}
	for i := lo; i <= hi; i++ {
		m.repaint(i)
	}
}
