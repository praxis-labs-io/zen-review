package review

import (
	"context"
	"slices"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

type State string

const (
	Unreviewed State = "unreviewed"
	Partial    State = "partial"
	Reviewed   State = "reviewed"
)

type Anchor struct {
	Side  store.Side
	Range Range
}

type Hunk struct {
	Diff diff.Hunk

	// Anchors are the sides the hunk touches, head first, and never empty.
	Anchors []Anchor

	State State
}

// Name is the side and line a hunk is named by: its first added line, or first removed when it adds none.
func (h Hunk) Name() (store.Side, int) {
	return h.Anchors[0].Side, h.Anchors[0].Range.Start
}

type File struct {
	Diff  diff.File
	State State
	Hunks []Hunk

	// Changed means a refresh took reviewed lines off this file, as opposed to a mark withdrawn.
	Changed bool

	Reviewed int
	Items    int
}

// Owns reports whether c was written against f, following a rename for a base-side comment.
func (f File) Owns(c store.Comment) bool {
	if c.Side == store.SideBase {
		return c.Path == f.Diff.BasePath()
	}
	return c.Path == f.Diff.Path
}

type Changeset struct {
	Files []File

	// Reviewed and Items are the burn-down, counting a file with no hunks as one item.
	Reviewed int
	Items    int

	Additions int
	Deletions int
}

// Derive reads the review state of files out of rows, in file-tree order. cut names the files
// a refresh took reviewed lines off, reported only on a file not reviewed again since.
func Derive(files []diff.File, rows []store.ReviewedRange, cut map[string]bool) Changeset {
	cur := coverageOf(rows)

	c := Changeset{Files: make([]File, 0, len(files))}
	for _, f := range files {
		file := deriveFile(f, cur)
		file.Changed = cut[f.Path] && file.State != Reviewed
		c.Files = append(c.Files, file)
		c.Reviewed += file.Reviewed
		c.Items += file.Items
		c.Additions += f.Additions
		c.Deletions += f.Deletions
	}

	slices.SortFunc(c.Files, func(a, b File) int { return byTree(a.Diff.Path, b.Diff.Path) })
	return c
}

// File finds a file by its head-side path.
func (c Changeset) File(path string) (File, bool) {
	for _, f := range c.Files {
		if f.Diff.Path == path {
			return f, true
		}
	}
	return File{}, false
}

// Hunk finds a hunk by the path, side and line it is named by.
func (c Changeset) Hunk(path string, side store.Side, line int) (Hunk, bool) {
	f, found := c.File(path)
	if !found {
		return Hunk{}, false
	}
	for _, h := range f.Hunks {
		if s, l := h.Name(); s == side && l == line {
			return h, true
		}
	}
	return Hunk{}, false
}

// Changeset is g's diff with the review on it. g has to exist; see Status.Exists.
func (s *Session) Changeset(ctx context.Context, g Generation) (Changeset, error) {
	files, err := s.Files(ctx, g)
	if err != nil {
		return Changeset{}, err
	}

	rows, err := s.db.ReviewedRanges(ctx, g.ID)
	if err != nil {
		return Changeset{}, err
	}

	gen, err := s.db.GenFiles(ctx, g.ID)
	if err != nil {
		return Changeset{}, err
	}
	return Derive(files, rows, cutsOf(gen)), nil
}

func deriveFile(f diff.File, cur map[key]coverage) File {
	out := File{Diff: f}

	sides := map[store.Side]coverage{
		store.SideHead: cur[key{path: f.Path, side: store.SideHead}],
		store.SideBase: cur[key{path: baseName(f), side: store.SideBase}],
	}

	read := false
	for _, d := range f.Hunks {
		anchors := anchorsOf(d)
		if len(anchors) == 0 {
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

	if len(out.Hunks) == 0 {
		out.Items = 1
		if sides[wholeSide(f.Status)].whole {
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

// anchorsOf spans the context between changed lines, or an edit to that context would leave the hunk reading reviewed.
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

func extend(r Range, line int) Range {
	if r.Start == 0 {
		r.Start = line
	}
	r.End = line
	return r
}

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

type key struct {
	path string
	side store.Side
}

type coverage struct {
	whole bool

	lines []Range
}

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

// wholeSide is the base for a deleted file, which has no head bytes for a whole-file mark to name.
func wholeSide(status diff.Status) store.Side {
	if status == diff.FileDeleted {
		return store.SideBase
	}
	return store.SideHead
}

func baseName(f diff.File) string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}
