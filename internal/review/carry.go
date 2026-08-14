package review

import (
	"context"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/store"
)

// carry is the previous generation's reviewed state, translated onto the
// generation being written. files is that generation's changeset, already
// parsed by the caller.
//
// It runs before the compare-and-swap rather than after. Everything between the
// ref moving and the row landing is a window where a failure leaves the ref
// ahead of the database, and two whole-tree diffs is too much to put in it. The
// price is that a lost race throws the work away, which is a path the CLI
// already documents as an ordinary outcome.
//
// The holds early return builds no generation at all, so there is nothing there
// to stamp and nothing to carry.
func (s *Session) carry(ctx context.Context, latest store.Generation, found bool, tree string, files []diff.File) (store.Carry, error) {
	if !found {
		return store.Carry{}, nil
	}

	rows, err := s.db.ReviewedRanges(ctx, latest.ID)
	if err != nil {
		return store.Carry{}, err
	}

	// One indexed read, against two whole-tree diffs already in this window. It
	// runs before the early return because a file cut to nothing has no ranges
	// left, and that is the case the record exists for.
	prior, err := s.cuts(ctx, latest.ID)
	if err != nil {
		return store.Carry{}, err
	}
	if len(rows) == 0 && len(prior) == 0 {
		// Every refresh before the first mark, and the reason neither diff runs
		// on the common path.
		return store.Carry{}, nil
	}

	was, err := s.repo.Tree(ctx, latest.CommitSha)
	if err != nil {
		return store.Carry{}, err
	}

	head, cut, err := s.translate(ctx, rows, store.SideHead, was, tree, prior)
	if err != nil {
		return store.Carry{}, err
	}
	head = readable(head, hunky(files))

	// Base-side rows exist only for deletion-only hunks somebody marked, so this
	// is nearly always nothing, and the base diff is the expensive one when it
	// does fire. Its diff runs only when the base moved, and a base move that
	// merges a reviewed hunk with a newly in-scope one is a changed scope rather
	// than changed content, so the cuts it reports are dropped.
	base, _, err := s.translate(ctx, rows, store.SideBase, latest.BaseSha, s.base.SHA, nil)
	if err != nil {
		return store.Carry{}, err
	}

	carried := append(head, base...)
	return store.Carry{Ranges: carried, Cut: settled(cut, files, carried)}, nil
}

// cuts is what the previous generation was left holding, keyed by the path each
// file had there. translate moves the key onto the new path, so a file renamed
// between two generations keeps its record without a second diff.
func (s *Session) cuts(ctx context.Context, generationID int64) (map[string]bool, error) {
	files, err := s.db.GenFiles(ctx, generationID)
	if err != nil {
		return nil, err
	}

	out := make(map[string]bool)
	for _, f := range files {
		if f.Cut {
			out[f.Path] = true
		}
	}
	return out, nil
}

// settled drops the files that read reviewed again.
//
// The record says lines changed after they were read, and a file read end to
// end has none of those lines left to point at. Clearing it here rather than at
// the next mark is what keeps the write in one place: a mark made after this
// generation is written raises the coverage, and Derive suppresses the record
// against it until this runs again.
func settled(cut map[string]bool, files []diff.File, carried []store.ReviewedRange) map[string]bool {
	if len(cut) == 0 {
		return nil
	}
	for _, f := range Derive(files, carried, nil).Files {
		if f.State == Reviewed {
			delete(cut, f.Diff.Path)
		}
	}
	return cut
}

// hunky is every path whose changeset entry has lines to read.
func hunky(files []diff.File) map[string]bool {
	out := make(map[string]bool, len(files))
	for _, f := range files {
		if len(f.Hunks) > 0 {
			out[f.Path] = true
		}
	}
	return out
}

// readable drops the whole-file marks whose file now has hunks in it.
//
// A whole-file mark is a claim about a changeset entry that had nothing to read,
// so it cannot outlive the entry gaining lines. The bytes need not have moved
// for that to happen: a base change gives a file real content while the head
// tree sits still, which is the case no translation can catch, because there is
// no diff between the two head trees to translate through.
//
// It is head-side only. A file with no hunks has no deletion-only hunk, so it
// has nothing to anchor base-side in the first place.
func readable(rows []store.ReviewedRange, hunks map[string]bool) []store.ReviewedRange {
	out := make([]store.ReviewedRange, 0, len(rows))
	for _, r := range rows {
		if r.Start == 0 && hunks[r.Path] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// translate carries one side's ranges from one tree-ish to another, and reports
// which files it took lines off.
//
// prior is the cuts already recorded against these files, keyed by the path they
// had on the from side. They come back keyed by the path they have on the to
// side, so a rename carries the record with the ranges.
//
// Equal ends need no diff and no translation, which is the rebase that leaves
// the content byte-identical: every mark comes through and the doc's promise
// that a review survives one costs a string comparison.
func (s *Session) translate(
	ctx context.Context,
	rows []store.ReviewedRange,
	side store.Side,
	from, to string,
	prior map[string]bool,
) ([]store.ReviewedRange, map[string]bool, error) {
	mine := onSide(rows, side)
	if (len(mine) == 0 && len(prior) == 0) || from == to {
		// Identical ends move no file, so a prior record keeps the path it had.
		return mine, copied(prior), nil
	}

	patch, err := s.repo.RemapDiff(ctx, from, to)
	if err != nil {
		return nil, nil, err
	}
	moved := byOldPath(diff.Parse(patch))

	var out []store.ReviewedRange
	cut := make(map[string]bool)
	for _, g := range groups(mine) {
		f, changed := moved[g.path]
		if !changed {
			// Absent from a diff of the two whole trees means byte-identical
			// between them, so the ranges come through untouched. This is also
			// what keeps a file that left the changeset reviewed: it left the
			// changeset and not the work tree, and it comes back as it was.
			out = append(out, g.rows...)
			continue
		}

		// The new path, so a range follows a renamed file. Where nothing moved
		// it is the path it already had.
		before := rangesOf(g.rows)
		after := Translate(f).Ranges(before)
		for _, r := range after {
			out = append(out, store.ReviewedRange{
				Path:      f.Path,
				Side:      side,
				LineRange: store.LineRange{Start: r.Start, End: r.End},
				CreatedAt: g.read,
			})
		}
		if shrank(before, after) {
			cut[f.Path] = true
		}
	}

	// A prior record follows its file through the same lookup, and does it
	// whether or not the file still has ranges. One cut to nothing has none, and
	// dropping the record there would lose it on the rewrite it was written for.
	for p := range prior {
		if f, changed := moved[p]; changed {
			cut[f.Path] = true
			continue
		}
		cut[p] = true
	}
	return out, cut, nil
}

func copied(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// shrank reports whether a file came out of a translation holding less than it
// went in with.
//
// Both sides are normalised, the stored rows by the write that made them and the
// translated ones by Ranges, so the two line counts are comparable. A whole-file
// mark holds no lines and is its own test: it survives only where the file's
// bytes did.
func shrank(before, after []Range) bool {
	if marksWhole(before) && !marksWhole(after) {
		return true
	}
	return spanned(after) < spanned(before)
}

func spanned(rs []Range) int {
	n := 0
	for _, r := range rs {
		if r.whole() {
			continue
		}
		n += r.End - r.Start + 1
	}
	return n
}

func marksWhole(rs []Range) bool {
	for _, r := range rs {
		if r.whole() {
			return true
		}
	}
	return false
}

// byOldPath indexes a remap diff by the path each file had on its old side,
// which is the path a range is stored under.
//
// A rename is found under OldPath and never under its new name, or a delete of x
// and a rename of y to x in one diff would both claim x and the winner would
// depend on the order git listed them.
//
// An addition and a copy are left out, because neither has an old side to carry
// from. Following a copy would move the review off the file that was read and
// onto a duplicate nobody has looked at, and copy detection being off is only
// the second reason that cannot happen.
func byOldPath(files []diff.File) map[string]diff.File {
	out := make(map[string]diff.File, len(files))
	for _, f := range files {
		switch f.Status {
		case diff.FileAdded, diff.FileCopied:
		case diff.FileRenamed:
			out[f.OldPath] = f
		default:
			out[f.Path] = f
		}
	}
	return out
}

// group is one file's ranges and when the oldest of them was read.
type group struct {
	path string
	rows []store.ReviewedRange

	// read is the oldest timestamp in the group, and is what every range
	// translated out of it takes. Translation splits and merges ranges, so there
	// is no row on the far side to carry one row's own stamp onto, and the oldest
	// is the honest answer to how long these lines have been read.
	read time.Time
}

// groups splits rows by path. They arrive ordered by path, so this is one pass
// and the output keeps that order.
func groups(rows []store.ReviewedRange) []group {
	var out []group
	for _, r := range rows {
		last := len(out) - 1
		if last >= 0 && out[last].path == r.Path {
			out[last].rows = append(out[last].rows, r)
			if r.CreatedAt.Before(out[last].read) {
				out[last].read = r.CreatedAt
			}
			continue
		}
		out = append(out, group{path: r.Path, rows: []store.ReviewedRange{r}, read: r.CreatedAt})
	}
	return out
}

func onSide(rows []store.ReviewedRange, side store.Side) []store.ReviewedRange {
	var out []store.ReviewedRange
	for _, r := range rows {
		if r.Side == side {
			out = append(out, r)
		}
	}
	return out
}

func rangesOf(rows []store.ReviewedRange) []Range {
	out := make([]Range, 0, len(rows))
	for _, r := range rows {
		out = append(out, Range{Start: r.Start, End: r.End})
	}
	return out
}
