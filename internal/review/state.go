package review

import (
	"context"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/store"
)

// State is how much of a hunk or a file has been read.
type State string

const (
	Unreviewed State = "unreviewed"
	Partial    State = "partial"
	Reviewed   State = "reviewed"
)

// Anchor is one side of a hunk and the lines it holds there.
type Anchor struct {
	Side  store.Side
	Range Range
}

// Hunk is one hunk of the changeset with what has been read marked on it.
type Hunk struct {
	Diff diff.Hunk

	// Anchors are the sides this hunk touches and the lines it holds on each,
	// head first, and there is always at least one.
	//
	// A hunk that both adds and removes has two. Reading it means reading both,
	// and marking it means marking both: a hunk anchored on its additions alone
	// would swallow a deletion arriving later, because the lines it removes are
	// not lines it has.
	Anchors []Anchor

	State State
}

// Name is the side and the line a hunk is named by: the first line it
// introduces, or the first line it removes when it introduces none.
//
// It is content rather than a position, where an index would name whatever is
// third in the file after an agent inserts a hunk above it.
func (h Hunk) Name() (store.Side, int) {
	return h.Anchors[0].Side, h.Anchors[0].Range.Start
}

// File is one file of the changeset with its hunks derived.
type File struct {
	Diff  diff.File
	State State
	Hunks []Hunk

	// Reviewed and Items are this file's share of the burn-down. An item is one
	// hunk, or the whole file when it has none.
	Reviewed int
	Items    int
}

// Changeset is a generation's diff with the review on it.
type Changeset struct {
	Files []File

	// Reviewed and Items are the burn-down: n of m, and the number the n key
	// walks down to zero.
	//
	// A file with no hunks counts as one item rather than none. A binary file is
	// one thing to read, and a counter leaving it out reads complete while it
	// sits unopened.
	Reviewed int
	Items    int
}

// Derive reads a changeset's state out of the ranges recorded against it.
//
// It holds no git and no database, and it makes no claim about how the ranges
// got there. A hunk is reviewed when every line of every anchor it has is
// covered, unreviewed when none is, and partial in between. A file is reviewed
// when every hunk is, or when a file with no hunks carries a whole-file mark.
//
// What it deliberately does not say is that something changed after review. A
// range that failed to translate and a range somebody withdrew leave the same
// coverage behind, so a read of that coverage cannot tell them apart. Only the
// refresh knows, because only the refresh ran the translation, and until it
// records what it cut this reports how much has been read and nothing about why.
func Derive(files []diff.File, rows []store.ReviewedRange) Changeset {
	cur := coverageOf(rows)

	c := Changeset{Files: make([]File, 0, len(files))}
	for _, f := range files {
		file := deriveFile(f, cur)
		c.Files = append(c.Files, file)
		c.Reviewed += file.Reviewed
		c.Items += file.Items
	}
	return c
}

// Hunk finds a hunk by the path, side and line the changeset names it under,
// which is how a subcommand naming one on the command line resolves it.
func (c Changeset) Hunk(path string, side store.Side, line int) (Hunk, bool) {
	for _, f := range c.Files {
		if f.Diff.Path != path {
			continue
		}
		for _, h := range f.Hunks {
			if s, l := h.Name(); s == side && l == line {
				return h, true
			}
		}
	}
	return Hunk{}, false
}

// Changeset is the generation's diff with the review on it.
//
// g has to be a generation that exists. Status reports that as Exists, and the
// zero value reaches git as an empty revision, the same way Session.Files does.
func (s *Session) Changeset(ctx context.Context, g Generation) (Changeset, error) {
	files, err := s.Files(ctx, g)
	if err != nil {
		return Changeset{}, err
	}

	rows, err := s.db.ReviewedRanges(ctx, g.ID)
	if err != nil {
		return Changeset{}, err
	}
	return Derive(files, rows), nil
}

// deriveFile is one file's hunks and the state that falls out of them.
func deriveFile(f diff.File, cur map[key]coverage) File {
	out := File{Diff: f}

	// Base-side ranges are stored under the name the file has on the base, which
	// a rename makes a different one from its own.
	sides := map[store.Side]coverage{
		store.SideHead: cur[key{path: f.Path, side: store.SideHead}],
		store.SideBase: cur[key{path: baseName(f), side: store.SideBase}],
	}

	read := false
	for _, d := range f.Hunks {
		anchors := anchorsOf(d)
		if len(anchors) == 0 {
			// Git emits no hunk with neither an addition nor a deletion. One
			// arriving here has nothing to mark and nothing to read, so it is not
			// a hunk of the review.
			continue
		}

		covered, lines := 0, 0
		for _, a := range anchors {
			covered += sides[a.Side].covered(a.Range)
			lines += a.Range.End - a.Range.Start + 1
		}

		state := reading(covered, lines)
		if state == Reviewed {
			out.Reviewed++
		}
		if state != Unreviewed {
			read = true
		}
		out.Hunks = append(out.Hunks, Hunk{Diff: d, Anchors: anchors, State: state})
	}

	// A whole-file mark answers only for a file with no lines to read. carry.go
	// drops one the moment its file has hunks, so honouring it on a file that
	// already has them would report a review the next refresh deletes.
	if len(out.Hunks) == 0 {
		out.Items = 1
		if sides[store.SideHead].whole {
			out.Reviewed, out.State = 1, Reviewed
			return out
		}
		out.State = Unreviewed
		return out
	}

	out.Items = len(out.Hunks)
	switch {
	case out.Reviewed == out.Items:
		out.State = Reviewed
	case read:
		out.State = Partial
	default:
		out.State = Unreviewed
	}
	return out
}

// anchorsOf is the sides a hunk touches and the lines it holds on each.
//
// A span runs from the first line the hunk has on that side to the last, taking
// in the context between them. Anchoring the changed lines exactly would be more
// precise and less correct: an agent editing a context line between two of them
// leaves both where they were, so the hunk would read reviewed with a changed
// line sitting inside it.
func anchorsOf(h diff.Hunk) []Anchor {
	var added, removed Range
	for _, l := range h.Lines {
		switch l.Kind {
		case diff.Added:
			added = extend(added, l.New)
		case diff.Removed:
			removed = extend(removed, l.Old)
		case diff.Context:
		}
	}

	var out []Anchor
	if added.Start != 0 {
		out = append(out, Anchor{Side: store.SideHead, Range: added})
	}
	if removed.Start != 0 {
		out = append(out, Anchor{Side: store.SideBase, Range: removed})
	}
	return out
}

// extend grows a span to take in one more line. A span starting at 0 has not
// started, which no line number can be mistaken for.
func extend(r Range, line int) Range {
	if r.Start == 0 {
		r.Start = line
	}
	r.End = line
	return r
}

// reading is how much of a hunk has been read.
func reading(covered, lines int) State {
	switch covered {
	case 0:
		return Unreviewed
	case lines:
		return Reviewed
	default:
		return Partial
	}
}

// key is one file on one side, which is what a range is stored under.
type key struct {
	path string
	side store.Side
}

// coverage is what has been read of one file on one side.
type coverage struct {
	// whole is a mark on the file rather than on lines in it, which is how a file
	// with no hunks is marked. Nothing reads it off a file that has hunks, so it
	// takes no part in the line arithmetic below.
	whole bool

	lines []Range
}

// covered is how many lines of r have been read.
func (c coverage) covered(r Range) int {
	n := 0
	for _, l := range c.lines {
		lo, hi := max(r.Start, l.Start), min(r.End, l.End)
		if lo <= hi {
			n += hi - lo + 1
		}
	}
	return n
}

// coverageOf groups stored ranges by file and side.
//
// The ranges are merged rather than taken as given. A stored set is normalised
// already, but Derive is handed what a caller has, and counting lines over a
// pair that overlaps would count the overlap twice.
func coverageOf(rows []store.ReviewedRange) map[key]coverage {
	out := make(map[key]coverage)
	for _, r := range rows {
		k := key{path: r.Path, side: r.Side}
		c := out[k]
		if r.Start == 0 {
			c.whole = true
		} else {
			c.lines = append(c.lines, Range{Start: r.Start, End: r.End})
		}
		out[k] = c
	}

	for k, c := range out {
		c.lines = merge(c.lines)
		out[k] = c
	}
	return out
}

// baseName is the name a file has on the base side, which a rename makes a
// different one from its own. It is what a base-side range is stored under.
func baseName(f diff.File) string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}
