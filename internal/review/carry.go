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

	// Only the open ones. Everything else has stopped moving, and a refresh that
	// moved a resolved comment would be reopening a question somebody closed.
	open, err := s.db.OpenComments(ctx, latest.ID)
	if err != nil {
		return store.Carry{}, err
	}

	if len(rows) == 0 && len(prior) == 0 && len(open) == 0 {
		// Every refresh before the first mark and the first comment, and the
		// reason neither diff runs on the common path.
		return store.Carry{}, nil
	}

	was, err := s.repo.Tree(ctx, latest.CommitSha)
	if err != nil {
		return store.Carry{}, err
	}

	headRows, baseRows := onSide(rows, store.SideHead), onSide(rows, store.SideBase)
	headNotes, baseNotes := commentsOn(open, store.SideHead), commentsOn(open, store.SideBase)

	// One diff per side, shared by the ranges and the comments. They move through
	// the same change and differ only in how forgiving they are about it.
	headMoved, err := s.moved(ctx, len(headRows)+len(prior)+len(headNotes) > 0, was, tree)
	if err != nil {
		return store.Carry{}, err
	}

	// Base-side anchors are a deletion-only hunk somebody marked or wrote against,
	// or the whole-file mark on a deleted file with no lines, so this is nearly
	// always nothing and the base diff is the expensive one when it does fire.
	// prior is head-keyed and is carried on the head side alone.
	baseMoved, err := s.moved(ctx, len(baseRows)+len(baseNotes) > 0, latest.BaseSha, s.base.SHA)
	if err != nil {
		return store.Carry{}, err
	}

	head, cut := translate(headRows, store.SideHead, headMoved, prior)
	head = readable(head, hunky(files))

	base, baseCut := translate(baseRows, store.SideBase, baseMoved, nil)
	onHead(cut, baseCut, files)

	carried := append(head, base...)
	moved := append(carryAnchors(headNotes, headMoved), carryAnchors(baseNotes, baseMoved)...)
	return store.Carry{Ranges: carried, Cut: settled(cut, files, carried), Comments: moved}, nil
}

// carryAnchors moves each comment's anchor through one side's change.
//
// A comment that keeps its anchor moves onto the generation being written, and
// takes the file's new name with it, which is what follows a rename. One that
// loses it orphans where it stands.
//
// Translation.Anchor is deliberately more forgiving than Ranges: a comment on
// ten lines is about a region, and an agent rewriting a line in the middle of
// that region is usually the comment being acted on rather than the comment
// being lost. What it does not forgive is a file comment on a file whose bytes
// moved, because that anchor names the file and the file is no longer the one
// named.
func carryAnchors(comments []store.Comment, moved map[string]diff.File) []store.CommentMove {
	out := make([]store.CommentMove, 0, len(comments))
	for _, c := range comments {
		f, changed := moved[c.Path]
		if !changed {
			// Absent from a diff of the two whole trees means byte-identical
			// between them, so the anchor comes through where it was.
			out = append(out, store.CommentMove{ID: c.ID, Path: c.Path, LineRange: c.LineRange})
			continue
		}

		r, held := Translate(f).Anchor(Range{Start: c.Start, End: c.End})
		if !held {
			out = append(out, store.CommentMove{ID: c.ID, Lost: true})
			continue
		}
		out = append(out, store.CommentMove{
			ID:        c.ID,
			Path:      f.Path,
			LineRange: store.LineRange{Start: r.Start, End: r.End},
		})
	}
	return out
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

// onHead folds the base side's cuts into the head side's, under the name the
// changeset lists each file by.
//
// A base-side range belongs to a deletion-only hunk, and it fails to translate
// when upstream rewrote the very lines whose removal somebody read. That is
// content moving under a reader, the same as on the head side. What a base move
// does that is not a cut is widen the scope, merging a reviewed hunk with a
// newly in-scope one, and that leaves every stored range translating cleanly, so
// it never reaches here.
//
// A base path the new changeset does not list has no row to be recorded on, and
// is dropped.
func onHead(cut, base map[string]bool, files []diff.File) {
	if len(base) == 0 {
		return
	}

	for _, f := range files {
		if base[baseName(f)] {
			cut[f.Path] = true
		}
	}
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
// It is head-side only, and not because the base has no whole-file marks: a
// deleted file has no head blob, so its one sits there. It is because the base
// side cannot reach here. A deleted file gains hunks only by its base blob
// becoming diffable, which is the base moving, and the base translation takes
// the mark on the way past.
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

// moved indexes one side's change by the path each file had before it, which is
// the path an anchor is stored under.
//
// It is nil where nothing has to move, and nil where the two ends are the same
// object. That is the rebase leaving the content byte-identical: every file is
// absent from an empty index, which is the same answer a diff would give, so the
// doc's promise that a review survives one costs a string comparison.
func (s *Session) moved(ctx context.Context, wanted bool, from, to string) (map[string]diff.File, error) {
	if !wanted || from == to {
		return nil, nil
	}

	patch, err := s.repo.RemapDiff(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return byOldPath(diff.Parse(patch)), nil
}

// translate carries one side's ranges through that side's change, and reports
// which files it took lines off.
//
// prior is the cuts already recorded against these files, keyed by the path they
// had before. They come back keyed by the path they have after, so a rename
// carries the record with the ranges.
func translate(
	rows []store.ReviewedRange,
	side store.Side,
	moved map[string]diff.File,
	prior map[string]bool,
) ([]store.ReviewedRange, map[string]bool) {
	var out []store.ReviewedRange
	cut := make(map[string]bool)
	for _, g := range groups(rows) {
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
	return out, cut
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

func commentsOn(comments []store.Comment, side store.Side) []store.Comment {
	var out []store.Comment
	for _, c := range comments {
		if c.Side == side {
			out = append(out, c)
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
