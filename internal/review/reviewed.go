package review

import (
	"context"
	"fmt"
	"time"

	"github.com/zen-review/zen-review/internal/store"
)

// StaleGenerationError means a mark was aimed at a generation that is no longer
// the session's latest.
//
// It is a refusal rather than a warning because such a mark is not merely old,
// it is inert: the carry runs from the latest generation, so nothing would ever
// pick the row up and the lines would read as unreviewed forever.
type StaleGenerationError struct {
	Seq     int
	Current int
}

func (e *StaleGenerationError) Error() string {
	if e.Current == 0 {
		return fmt.Sprintf("generation %d is gone and this session has none: refresh before marking anything", e.Seq)
	}
	return fmt.Sprintf("generation %d is not the current one, %d is: refresh and mark against what is there now",
		e.Seq, e.Current)
}

// Mark records lines of a file as reviewed at a generation.
//
// path is the file's head-side name, the one the changeset lists it under. A
// base-side mark is stored under the name the file has on the base, which a
// rename makes a different one, because the diff a refresh translates base-side
// ranges through knows only that name.
//
// The ranges are unioned with what is already there and normalised, so marking
// the same lines twice is not two rows and marking either side of a gap does not
// close over it. A range starting at 0 is the file as a whole, which is how a
// file with no hunks is marked.
//
// Lines are explicit here. Naming a hunk is a question about the parsed diff and
// is answered a layer up.
func (s *Session) Mark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, side, "", func(cur []store.LineRange) []store.LineRange {
		return lineRanges(merge(append(ranges(cur), rs...)))
	})
}

// Unmark takes lines back out of what was reviewed, cutting a stored range where
// the two overlap rather than dropping it whole.
//
// It also settles any cut the refresh recorded against the file. That record is
// the refresh reporting what it took off a reader's coverage, and a reader
// taking lines back by hand has made the coverage their own. Leaving it would
// have the file read as changed after review for a cut the reader may already
// have answered, with no refresh due to write it away.
func (s *Session) Unmark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, side, path, func(cur []store.LineRange) []store.LineRange {
		return lineRanges(subtract(ranges(cur), rs))
	})
}

// MarkHunk records every anchor a hunk has, which is what reading one means. A
// hunk that both adds and removes takes a mark on each side: the lines it
// removes are not lines it has, and a mark on the additions alone would swallow
// a deletion arriving later.
func (s *Session) MarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, h.Anchors, s.Mark)
}

// UnmarkHunk takes back what MarkHunk recorded.
func (s *Session) UnmarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, h.Anchors, s.Unmark)
}

// MarkFile records every anchor of every hunk, or the file as a whole when it
// has none.
func (s *Session) MarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, fileAnchors(f), s.Mark)
}

// UnmarkFile takes back what MarkFile recorded.
func (s *Session) UnmarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, fileAnchors(f), s.Unmark)
}

// Reviewed is every range recorded against a generation, ordered by path, side
// and start line.
//
// A base-side row carries the file's base-side path, so a caller grouping these
// by file joins a renamed one back through the changeset's old path.
func (s *Session) Reviewed(ctx context.Context, g Generation) ([]store.ReviewedRange, error) {
	return s.db.ReviewedRanges(ctx, g.ID)
}

// anchored applies one write per side the anchors touch, head first.
//
// Two sides are two transactions, because the store keys a write on one file and
// one side. A failure between them leaves the hunk partial, which is what it is:
// one side was read and recorded and the other was not.
func (s *Session) anchored(
	ctx context.Context,
	g Generation,
	path string,
	anchors []Anchor,
	apply func(context.Context, Generation, string, store.Side, []Range) error,
) error {
	for _, side := range []store.Side{store.SideHead, store.SideBase} {
		var rs []Range
		for _, a := range anchors {
			if a.Side == side {
				rs = append(rs, a.Range)
			}
		}
		if len(rs) == 0 {
			continue
		}
		if err := apply(ctx, g, path, side, rs); err != nil {
			return err
		}
	}
	return nil
}

// fileAnchors is every anchor of every hunk, or the whole file when it has none:
// a binary file, a mode change, a rename that moved nothing. The zero Range is
// the whole-file mark, which merge and subtract already keep apart from the line
// ranges.
func fileAnchors(f File) []Anchor {
	if len(f.Hunks) == 0 {
		return []Anchor{{Side: store.SideHead}}
	}

	var out []Anchor
	for _, h := range f.Hunks {
		out = append(out, h.Anchors...)
	}
	return out
}

// updateReviewed refuses a stale generation and hands the arithmetic to the
// store, which runs it inside the write transaction.
func (s *Session) updateReviewed(
	ctx context.Context,
	g Generation,
	path string,
	side store.Side,
	answers string,
	change func([]store.LineRange) []store.LineRange,
) error {
	latest, found, err := s.db.LatestGeneration(ctx, s.row.ID)
	if err != nil {
		return err
	}
	if !found || latest.ID != g.ID {
		return &StaleGenerationError{Seq: g.Seq, Current: latest.Seq}
	}

	if side == store.SideBase {
		if path, err = s.basePath(ctx, g, path); err != nil {
			return err
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	return s.db.UpdateReviewedRanges(ctx, s.row.ID, g.ID, path, side, now, answers, change)
}

// basePath is the name a file had on the base side of a generation.
//
// It is the head name for everything except a rename, and a rename is the whole
// reason this exists: the base blob of a file the branch moved sits at the old
// name, and so does its entry in the diff a refresh translates base-side ranges
// through. A path the generation does not hold is returned as it came, because
// refusing it here would turn a mark on a file that left the changeset into an
// error rather than a row nothing reads.
func (s *Session) basePath(ctx context.Context, g Generation, path string) (string, error) {
	f, found, err := s.db.GenFile(ctx, g.ID, path)
	if err != nil || !found || f.OldPath == "" {
		return path, err
	}
	return f.OldPath, nil
}

// ranges and lineRanges cross the store boundary. Range carries the engine's
// arithmetic and LineRange is the row, and neither can be the other's type.
func ranges(ls []store.LineRange) []Range {
	out := make([]Range, 0, len(ls))
	for _, l := range ls {
		out = append(out, Range{Start: l.Start, End: l.End})
	}
	return out
}

func lineRanges(rs []Range) []store.LineRange {
	out := make([]store.LineRange, 0, len(rs))
	for _, r := range rs {
		out = append(out, store.LineRange{Start: r.Start, End: r.End})
	}
	return out
}
