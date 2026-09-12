package review

import (
	"context"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// carry does the git work before the swap and leaves reading rows to the store's transaction,
// so a write landing mid-refresh is seen.
func (s *Session) carry(ctx context.Context, latest store.Generation, found bool, tree string, files []diff.File) (store.Advance, error) {
	if !found {
		return store.Advance{Carry: func(store.Prior) store.Carry { return store.Carry{} }}, nil
	}

	was, err := s.repo.Tree(ctx, latest.CommitSha)
	if err != nil {
		return store.Advance{}, err
	}

	headMoved, err := s.moved(ctx, was, tree)
	if err != nil {
		return store.Advance{}, err
	}

	baseMoved, err := s.moved(ctx, latest.BaseSha, s.base.SHA)
	if err != nil {
		return store.Advance{}, err
	}

	return store.Advance{
		From: latest.ID,
		Carry: func(p store.Prior) store.Carry {
			return translated(p, headMoved, baseMoved, files)
		},
	}, nil
}

// translated is pure because it runs inside the store's transaction, which holds the only connection.
func translated(p store.Prior, headMoved, baseMoved map[string]diff.File, files []diff.File) store.Carry {
	prior := cutsOf(p.Files)
	if len(p.Ranges) == 0 && len(prior) == 0 && len(p.Comments) == 0 {
		return store.Carry{}
	}

	headRows, baseRows := onSide(p.Ranges, store.SideHead), onSide(p.Ranges, store.SideBase)
	headNotes, baseNotes := commentsOn(p.Comments, store.SideHead), commentsOn(p.Comments, store.SideBase)

	head, cut := translate(headRows, store.SideHead, headMoved, prior)
	head = readable(head, hunky(files))

	base, baseCut := translate(baseRows, store.SideBase, baseMoved, nil)
	onHead(cut, baseCut, files)

	carried := append(head, base...)
	moved := append(carryAnchors(headNotes, headMoved), carryAnchors(baseNotes, baseMoved)...)
	return store.Carry{Ranges: carried, Cut: settled(cut, files, carried), Comments: moved}
}

// carryAnchors uses Anchor rather than Ranges: a comment survives an edit inside its region, where a mark is cut.
func carryAnchors(comments []store.Comment, moved map[string]diff.File) []store.CommentMove {
	out := make([]store.CommentMove, 0, len(comments))
	for _, c := range comments {
		f, changed := moved[c.Path]
		if !changed {
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

func cutsOf(files []store.GenFile) map[string]bool {
	out := make(map[string]bool)
	for _, f := range files {
		if f.Cut {
			out[f.Path] = true
		}
	}
	return out
}

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

func hunky(files []diff.File) map[string]bool {
	out := make(map[string]bool, len(files))
	for _, f := range files {
		if len(f.Hunks) > 0 {
			out[f.Path] = true
		}
	}
	return out
}

// readable drops a whole-file mark whose file gained hunks, which a base change causes with no head diff to translate.
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

func (s *Session) moved(ctx context.Context, from, to string) (map[string]diff.File, error) {
	if from == to {
		return nil, nil
	}

	patch, err := s.repo.RemapDiff(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return byOldPath(diff.Parse(patch)), nil
}

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
			out = append(out, g.rows...)
			continue
		}

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

	for p := range prior {
		if f, changed := moved[p]; changed {
			cut[f.Path] = true
			continue
		}
		cut[p] = true
	}
	return out, cut
}

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

// byOldPath leaves copies out, or a review would move onto a duplicate nobody read.
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

type group struct {
	path string
	rows []store.ReviewedRange

	// read is the oldest stamp in the group, since translation splits and merges rows past any one row's own.
	read time.Time
}

// groups assumes rows arrive ordered by path, as the store returns them.
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
