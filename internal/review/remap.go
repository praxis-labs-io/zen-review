package review

import (
	"cmp"
	"math"
	"slices"

	"github.com/praxis-labs-io/zen-review/internal/diff"
)

// Range is a closed interval of lines on one side of a file. A Start of 0 is the file as a
// whole, with End 0.
type Range struct {
	Start int
	End   int
}

func (r Range) whole() bool { return r.Start == 0 }

type span struct {
	lo, hi int
	delta  int
}

type Translation struct {
	held bool

	absent bool

	spans []span
}

// Translate reads where f's old-side lines went. An added or deleted file carries nothing, and
// so does one whose bytes changed with no hunks to follow.
func Translate(f diff.File) Translation {
	switch {
	case f.Status == diff.FileAdded || f.Status == diff.FileDeleted:
		return Translation{absent: true}
	case held(f):
		return Translation{held: true}
	case len(f.Hunks) == 0:
		return Translation{}
	default:
		return Translation{spans: spansOf(f.Hunks)}
	}
}

// Ranges moves rs onto the new side, cut wherever the change ran through them, merged and in
// order. A whole-file range survives only a file whose bytes held.
func (t Translation) Ranges(rs []Range) []Range {
	if t.held {
		return Merge(rs)
	}

	out := make([]Range, 0, len(rs))
	for _, r := range rs {
		if r.whole() {
			continue
		}
		out = append(out, t.cut(r)...)
	}
	return Merge(out)
}

// Anchor clamps r to the lines that survived, false when none did. A whole-file anchor holds
// while the file exists.
func (t Translation) Anchor(r Range) (Range, bool) {
	if t.held {
		return r, true
	}
	if r.whole() {
		return r, !t.absent
	}

	survived := t.cut(r)
	if len(survived) == 0 {
		return Range{}, false
	}
	return Range{Start: survived[0].Start, End: survived[len(survived)-1].End}, true
}

func (t Translation) cut(r Range) []Range {
	var out []Range
	for _, s := range t.spans {
		lo, hi := max(r.Start, s.lo), min(r.End, s.hi)
		if lo > hi {
			continue
		}
		out = append(out, Range{Start: lo + s.delta, End: hi + s.delta})
	}
	return out
}

func held(f diff.File) bool {
	if len(f.Hunks) > 0 || f.Binary {
		return false
	}
	if f.Status == diff.FileRenamed || f.Status == diff.FileCopied {
		return true
	}
	return f.OldMode != "" && f.NewMode != "" && f.OldMode != f.NewMode
}

// spansOf leaves the last run unbounded because a patch never says how long the file is.
func spansOf(hunks []diff.Hunk) []span {
	var spans []span
	cursor, delta := 1, 0

	for _, h := range hunks {
		from := h.OldStart
		if h.OldLines == 0 {
			from = h.OldStart + 1
		}

		if from > cursor {
			spans = append(spans, span{lo: cursor, hi: from - 1, delta: delta})
		}
		spans = append(spans, contextOf(h)...)

		cursor = from + h.OldLines
		delta += h.NewLines - h.OldLines
	}
	return append(spans, span{lo: cursor, hi: math.MaxInt, delta: delta})
}

func contextOf(h diff.Hunk) []span {
	var spans []span
	var run span
	open := false

	flush := func() {
		if open {
			spans = append(spans, run)
			open = false
		}
	}

	for _, l := range h.Lines {
		if l.Kind != diff.Context {
			flush()
			continue
		}

		delta := l.New - l.Old
		if open && l.Old == run.hi+1 && delta == run.delta {
			run.hi = l.Old
			continue
		}
		flush()
		run, open = span{lo: l.Old, hi: l.Old, delta: delta}, true
	}
	flush()

	return spans
}

// Subtract cuts rs out of cur, splitting a range the cut lands inside. Both sides are merged first.
func Subtract(cur, rs []Range) []Range {
	out := Merge(cur)
	for _, r := range rs {
		var next []Range
		for _, c := range out {
			next = append(next, c.without(r)...)
		}
		out = next
	}
	return out
}

// without needs no whole-file case: 0:0 and a range starting at line 1 never overlap.
func (c Range) without(r Range) []Range {
	if r.End < c.Start || r.Start > c.End {
		return []Range{c}
	}

	var out []Range
	if r.Start > c.Start {
		out = append(out, Range{Start: c.Start, End: r.Start - 1})
	}
	if r.End < c.End {
		out = append(out, Range{Start: r.End + 1, End: c.End})
	}
	return out
}

// Merge joins only overlapping or touching ranges: a one-line gap is a line nobody read. A whole-file
// range stays its own entry.
func Merge(rs []Range) []Range {
	if len(rs) == 0 {
		return nil
	}

	lines := make([]Range, 0, len(rs))
	whole := false
	for _, r := range rs {
		if r.whole() {
			whole = true
			continue
		}
		lines = append(lines, r)
	}
	slices.SortFunc(lines, func(a, b Range) int { return cmp.Compare(a.Start, b.Start) })

	out := make([]Range, 0, len(lines)+1)
	if whole {
		out = append(out, Range{})
	}
	for _, r := range lines {
		last := len(out) - 1
		if last >= 0 && !out[last].whole() && r.Start <= out[last].End+1 {
			out[last].End = max(out[last].End, r.End)
			continue
		}
		out = append(out, r)
	}
	return out
}
