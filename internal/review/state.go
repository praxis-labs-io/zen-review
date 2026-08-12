package review

import (
	"context"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/store"
)

// State is what a hunk or a file reads as.
type State string

const (
	Unreviewed State = "unreviewed"

	// Changed is changed after review: lines somebody read are no longer the
	// lines that are there.
	Changed State = "changed"

	Reviewed State = "reviewed"
)

// Hunk is one hunk of the changeset with what has been read marked on it.
type Hunk struct {
	Diff diff.Hunk

	// Side is head except on a deletion-only hunk, which has no head-side lines
	// to anchor to.
	Side store.Side

	// Range is the lines a mark on this hunk covers, and the lines its state is
	// read from. Its Start also names the hunk: it is the first line the hunk
	// introduces, which is content, where an index would name whatever is third
	// in the file after an agent inserts a hunk above it.
	Range Range

	State State
}

// File is one file of the changeset with its hunks derived.
type File struct {
	Diff  diff.File
	State State
	Hunks []Hunk

	// Reviewed is how many of Hunks read reviewed, which is the progress a
	// half-read file has to show.
	Reviewed int
}

// Changeset is a generation's diff with the review on it.
type Changeset struct {
	Files []File

	// Reviewed and Hunks are the burn-down: n of m, and the number the n key
	// walks down to zero.
	Reviewed int
	Hunks    int
}

// Derive reads a changeset's state out of the ranges recorded against it and
// the ranges recorded against the generation before it.
//
// It holds no git and no database, and it does not know which generations these
// are. The caller pairs them.
//
// A hunk is reviewed when every line of its anchor is covered, unreviewed when
// none is, and changed after review when some are. That last state has one
// cause: a mark covers a hunk whole, so a hunk covered in part is one a
// translation cut.
//
// A file is reviewed when every hunk is, or when a file with no hunks carries a
// whole-file mark. It is changed after review when a hunk is, or when it covers
// fewer lines than it did at the previous generation, which is what catches a
// hunk rewritten end to end: that loses its range whole and would otherwise
// read like a hunk nobody ever opened.
//
// Three things it does not do, none of which earn a column:
//
//   - The changed signal is one generation deep. Refresh again with no edits and
//     there is no drop left to find, so a file decays back to unreviewed.
//   - A mark on the same file after the refresh raises the count back and hides
//     the signal. That reads as the file being addressed, which is usually what
//     it is.
//   - The reviewed count is not stable across a base change. A base move can
//     merge a reviewed hunk with a newly in-scope one, and the union reads
//     unreviewed.
func Derive(files []diff.File, now, before []store.ReviewedRange) Changeset {
	cur, prev := coverageOf(now), coverageOf(before)

	c := Changeset{Files: make([]File, 0, len(files))}
	for _, f := range files {
		file := deriveFile(f, cur, prev)
		c.Files = append(c.Files, file)
		c.Reviewed += file.Reviewed
		c.Hunks += len(file.Hunks)
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
			if h.Side == side && h.Range.Start == line {
				return h, true
			}
		}
	}
	return Hunk{}, false
}

// Changeset is the generation's diff with the review on it.
//
// It reads the previous generation's ranges as well as this one's, because that
// is the only thing that tells a file which lost what was read about it from one
// nobody ever opened. The first generation of a session has none, and everything
// in it reads unreviewed until somebody marks it.
func (s *Session) Changeset(ctx context.Context, g Generation) (Changeset, error) {
	files, err := s.Files(ctx, g)
	if err != nil {
		return Changeset{}, err
	}

	now, err := s.db.ReviewedRanges(ctx, g.ID)
	if err != nil {
		return Changeset{}, err
	}

	prev, found, err := s.db.PreviousGeneration(ctx, s.row.ID, g.Seq)
	if err != nil {
		return Changeset{}, err
	}
	if !found {
		return Derive(files, now, nil), nil
	}

	before, err := s.db.ReviewedRanges(ctx, prev.ID)
	if err != nil {
		return Changeset{}, err
	}
	return Derive(files, now, before), nil
}

// deriveFile is one file's hunks and the state that falls out of them.
func deriveFile(f diff.File, cur, prev map[key]coverage) File {
	out := File{Diff: f, Hunks: make([]Hunk, 0, len(f.Hunks))}

	head := key{path: f.Path, side: store.SideHead}
	base := key{path: baseName(f), side: store.SideBase}

	// A whole-file mark is a claim about the file rather than about lines in it,
	// so it answers for both sides. carry.go already stops one outliving a file
	// that gained hunks, which is what keeps it from reading as a review of code.
	whole := cur[head].whole

	changed := false
	for _, h := range f.Hunks {
		side, r := anchor(h)

		state := Reviewed
		if !whole {
			k := head
			if side == store.SideBase {
				k = base
			}
			state = stateOf(cur[k], r)
		}

		switch state {
		case Reviewed:
			out.Reviewed++
		case Changed:
			changed = true
		case Unreviewed:
		}
		out.Hunks = append(out.Hunks, Hunk{Diff: h, Side: side, Range: r, State: state})
	}

	// Reviewed sits above Changed on purpose: a drop somebody then read is done,
	// and saying otherwise would leave a file nobody can finish.
	switch {
	case whole || (len(f.Hunks) > 0 && out.Reviewed == len(f.Hunks)):
		out.State = Reviewed
	case changed || dropped(f, cur, prev):
		out.State = Changed
	default:
		out.State = Unreviewed
	}
	return out
}

// anchor is the side a hunk is marked on and the lines a mark covers.
//
// The span runs from the first line the hunk introduces to the last, taking in
// the context between them. Anchoring the introduced lines exactly would be more
// precise and less correct: an agent editing a context line between two of them
// leaves both where they were, so the hunk would read reviewed with a changed
// line sitting inside it.
//
// A deletion-only hunk has no head-side lines and takes the base side, spanning
// what it removed.
func anchor(h diff.Hunk) (store.Side, Range) {
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

	if added.Start != 0 {
		return store.SideHead, added
	}
	return store.SideBase, removed
}

// extend grows a span to take in one more line. A span starting at 0 has not
// started, which is the same thing Range.whole means and cannot collide with it:
// no line is numbered 0.
func extend(r Range, line int) Range {
	if r.Start == 0 {
		r.Start = line
	}
	r.End = line
	return r
}

// stateOf reads one hunk's anchor against what has been covered.
func stateOf(c coverage, r Range) State {
	switch c.covered(r) {
	case 0:
		return Unreviewed
	case r.End - r.Start + 1:
		return Reviewed
	default:
		return Changed
	}
}

// dropped says a file covers fewer lines than it did at the previous generation.
//
// Head-side coverage is looked up under the file's own name and then under
// OldPath. An agent renaming a file between generations leaves the old name
// there, because both generations are measured from the same base, and without
// the fallback a rename plus a rewrite would read as untouched. The base side
// needs no fallback, because its key is that old name already.
func dropped(f diff.File, cur, prev map[key]coverage) bool {
	head := key{path: f.Path, side: store.SideHead}
	was, found := prev[head]
	if !found && f.OldPath != "" {
		was = prev[key{path: f.OldPath, side: store.SideHead}]
	}
	if shrank(was, cur[head]) {
		return true
	}

	base := key{path: baseName(f), side: store.SideBase}
	return shrank(prev[base], cur[base])
}

// shrank says coverage was lost between two generations.
//
// A translation maps a surviving run one line to one line, so a file's covered
// lines can only hold or fall, and a fall means something failed to translate.
func shrank(was, now coverage) bool {
	if was.whole && !now.whole {
		return true
	}
	return count(now.lines) < count(was.lines)
}

// key is one file on one side, which is what a range is stored under.
type key struct {
	path string
	side store.Side
}

// coverage is what has been read of one file on one side.
type coverage struct {
	// whole is a mark on the file rather than on lines in it, which is how a file
	// with no hunks is marked.
	whole bool

	lines []Range
}

// covered is how many lines of r have been read.
func (c coverage) covered(r Range) int {
	if c.whole {
		return r.End - r.Start + 1
	}

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

func count(rs []Range) int {
	n := 0
	for _, r := range rs {
		n += r.End - r.Start + 1
	}
	return n
}
