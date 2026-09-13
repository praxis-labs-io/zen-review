package diffpane

import (
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func (m Model) Selecting() bool { return m.anchor.comment != "" || m.anchor.seq >= 0 }

// Selected returns the selected lines, one anchor per side, not yet clipped to the hunks.
func (m Model) Selected() ([]review.Anchor, bool) {
	lo, hi, ok := m.span()
	if !ok {
		return nil, false
	}
	return anchorsOver(m.rows[lo:hi+1], m.scope())
}

// Line returns the anchors the cursor's row names, and false off code. A unified context row names both sides.
func (m Model) Line() ([]review.Anchor, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil, false
	}
	return anchorsOver(m.rows[m.cursor:m.cursor+1], m.scope())
}

func anchorsOver(rows []row, side store.Side) ([]review.Anchor, bool) {
	var head, base review.Range
	for i := range rows {
		if rows[i].kind != codeRow {
			continue
		}

		switch side {
		case store.SideBase:
			base = grow(base, rows[i].line.Old)
		case store.SideHead:
			head = grow(head, rows[i].right.New)
		default:
			head = grow(head, rows[i].line.New)
			base = grow(base, rows[i].line.Old)
		}
	}

	var out []review.Anchor
	if head.Start != 0 {
		out = append(out, review.Anchor{Side: store.SideHead, Range: head})
	}
	if base.Start != 0 {
		out = append(out, review.Anchor{Side: store.SideBase, Range: base})
	}

	return out, len(out) > 0
}

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

func (m Model) inSelection(i int) bool {
	lo, hi, ok := m.span()
	return ok && i >= lo && i <= hi && m.rows[i].kind == codeRow
}

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
