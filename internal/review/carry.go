package review

import (
	"context"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/store"
)

// carry is the previous generation's reviewed state, translated onto the
// generation being written.
//
// It runs after the compare-and-swap has won. A lost race otherwise pays for two
// tree diffs and a whole translation on a path the CLI documents as an ordinary
// outcome. The holds early return builds no generation at all, so there is
// nothing there to stamp and nothing to carry.
func (s *Session) carry(ctx context.Context, latest store.Generation, found bool, tree string) (store.Carry, error) {
	if !found {
		return store.Carry{}, nil
	}

	rows, err := s.db.ReviewedRanges(ctx, latest.ID)
	if err != nil {
		return store.Carry{}, err
	}
	if len(rows) == 0 {
		// Every refresh before the first mark, and the reason neither diff runs
		// on the common path.
		return store.Carry{}, nil
	}

	was, err := s.repo.Tree(ctx, latest.CommitSha)
	if err != nil {
		return store.Carry{}, err
	}

	head, err := s.translate(ctx, rows, store.SideHead, was, tree)
	if err != nil {
		return store.Carry{}, err
	}

	// Base-side rows exist only for deletion-only hunks somebody marked, so this
	// is nearly always nothing, and the base diff is the expensive one when it
	// does fire.
	base, err := s.translate(ctx, rows, store.SideBase, latest.BaseSha, s.base.SHA)
	if err != nil {
		return store.Carry{}, err
	}
	return store.Carry{Ranges: append(head, base...)}, nil
}

// translate carries one side's ranges from one tree-ish to another.
//
// Equal ends need no diff and no translation, which is the rebase that leaves
// the content byte-identical: every mark comes through and the doc's promise
// that a review survives one costs a string comparison.
func (s *Session) translate(
	ctx context.Context,
	rows []store.ReviewedRange,
	side store.Side,
	from, to string,
) ([]store.ReviewedRange, error) {
	mine := onSide(rows, side)
	if len(mine) == 0 || from == to {
		return mine, nil
	}

	patch, err := s.repo.RemapDiff(ctx, from, to)
	if err != nil {
		return nil, err
	}
	moved := byOldPath(diff.Parse(patch))

	var out []store.ReviewedRange
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
		for _, r := range Translate(f).Ranges(rangesOf(g.rows)) {
			out = append(out, store.ReviewedRange{
				Path:      f.Path,
				Side:      side,
				LineRange: store.LineRange{Start: r.Start, End: r.End},
				CreatedAt: g.read,
			})
		}
	}
	return out, nil
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
