package review

import (
	"cmp"
	"math"
	"slices"

	"github.com/praxis-labs-io/zen-review/internal/diff"
)

// Range is a closed interval of lines on one side of a file, and the unit both
// reviewed state and comment anchors are stored as.
//
// A Start of 0 means the file as a whole rather than any line in it. That is how
// a file with no hunks is marked: a binary file, a mode change, a rename that
// moved nothing. End is 0 with it, and neither is a line number.
type Range struct {
	Start int
	End   int
}

// whole says this names the file rather than lines in it.
func (r Range) whole() bool { return r.Start == 0 }

// span is a run of old-side lines that came through a change unmoved, and the
// offset that puts them on the new side.
type span struct {
	lo, hi int
	delta  int
}

// Translation carries anchors on one side of a file's diff over to the other.
//
// It is the whole of the remap's arithmetic and it holds nothing else: no git,
// no database, no notion of which generation either side came from. The caller
// pairs the paths and decides what a lost anchor means.
type Translation struct {
	// held is the file arriving with the content it already had, so every anchor
	// on it comes through untouched.
	held bool

	// absent is the file being on one side only. An added file has no old side to
	// anchor from and a deleted one has no new side to reach, and either way there
	// is no pair to carry an anchor between. It is what separates a file whose
	// bytes moved from a file that is not there, which line anchors do not care
	// about and a whole-file anchor is entirely about.
	absent bool

	// spans are the surviving runs in old-side order. Empty, with held false,
	// means nothing survived.
	spans []span
}

// Translate reads out of a parsed file diff where its old-side lines went.
//
// Three files translate nothing. One that was added has no old side to anchor
// to and one that was deleted has no new side to reach. One whose bytes changed
// with no hunks to follow, a binary file or a path still unmerged, has lines
// that went somewhere unsayable, and guessing they stayed put is how a review
// reports code nobody read as read.
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

// Ranges moves reviewed ranges onto the new side, keeping only the lines that
// did not change, and returns them merged and in order.
//
// A range is cut wherever the change ran through it and the pieces come through
// on their own, because the lines either side of an edit are still lines that
// were read. What is never true is a changed line inside a surviving range: that
// is the property the whole feature rests on, and it is why this cuts rather
// than shifts whole.
func (t Translation) Ranges(rs []Range) []Range {
	if t.held {
		return merge(rs)
	}

	out := make([]Range, 0, len(rs))
	for _, r := range rs {
		// The file's bytes moved, so a mark on the file as a whole no longer
		// describes the file.
		if r.whole() {
			continue
		}
		out = append(out, t.cut(r)...)
	}
	return merge(out)
}

// Anchor moves a comment anchor, clamped to the lines that survived, and reports
// false when none did.
//
// It is deliberately more forgiving than Ranges. A comment on ten lines is about
// a region, and the agent rewriting a line in the middle of that region is
// usually the comment being acted on rather than the comment being lost. Orphan
// it there and every comment that worked orphans before it can be confirmed.
//
// An anchor on the file as a whole does not take the rule it takes in Ranges,
// and the difference is what the two are claims about. A whole-file mark says
// somebody read these bytes, so changing them voids it. A whole-file comment
// says something about the file, and editing the file is not an answer to it:
// "too many comments in here" stays true, and stays outstanding, while somebody
// works on it. It comes through while the file does and is lost when the file
// is, which is the only thing that can make it meaningless.
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

// cut is the surviving pieces of one range, on the new side, in order.
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

// held says the file came through with the content it already had. A rename or
// a copy moved it, or a mode change touched everything about it except its
// bytes.
//
// A file with hunks changed, and a binary file changed without saying how. An
// added or a deleted file is neither, and is refused above this.
func held(f diff.File) bool {
	if len(f.Hunks) > 0 || f.Binary {
		return false
	}
	if f.Status == diff.FileRenamed || f.Status == diff.FileCopied {
		return true
	}
	return f.OldMode != "" && f.NewMode != "" && f.OldMode != f.NewMode
}

// spansOf is every run of old-side lines a patch leaves where it found them.
//
// The runs between hunks are the bulk of a file and shift by the lines the
// hunks above them added or removed. The runs inside a hunk are its context, and
// those carry their own number on both sides, so they need no running total.
//
// The last run is unbounded because a patch never says how long the file is.
func spansOf(hunks []diff.Hunk) []span {
	var spans []span
	cursor, delta := 1, 0

	for _, h := range hunks {
		// A hunk that only inserts names the line it follows rather than one
		// inside itself, so its first old line is the one after.
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

// contextOf is the runs of unchanged lines inside one hunk.
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

// subtract is what is left of cur once every range in rs is taken out of it,
// and is the whole of what unmarking does.
func subtract(cur, rs []Range) []Range {
	out := merge(cur)
	for _, r := range rs {
		var next []Range
		for _, c := range out {
			next = append(next, c.without(r)...)
		}
		out = next
	}
	return out
}

// without is what is left of c once r is taken out of it.
//
// A whole-file mark needs no case of its own, and adding one is the next likely
// mistake here. It is the interval 0:0 and a line range starts at 1, so the two
// are always disjoint below and neither clips the other, which leaves a
// whole-file mark removable only by a whole-file unmark.
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

// merge normalises a set of ranges: sorted, with overlaps and touching pairs
// joined.
//
// Touching means exactly one past the end and never a tolerance. A gap of a
// single line is a line nobody reviewed, and closing over it would report it as
// read. A whole-file mark is kept apart from the line ranges for the same
// reason: it ends at line 0, so the first line of the file looks like it touches
// it.
func merge(rs []Range) []Range {
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
