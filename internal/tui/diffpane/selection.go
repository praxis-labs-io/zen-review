package diffpane

import (
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
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

	var head, base review.Range
	for i := lo; i <= hi; i++ {
		if m.rows[i].kind != codeRow {
			continue
		}
		head = grow(head, m.rows[i].line.New)
		base = grow(base, m.rows[i].line.Old)
	}

	var out []review.Anchor
	if head.Start != 0 {
		out = append(out, review.Anchor{Side: store.SideHead, Range: head})
	}
	if base.Start != 0 {
		out = append(out, review.Anchor{Side: store.SideBase, Range: base})
	}
	return out, true
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
