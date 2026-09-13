package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/store"
)

// StaleGenerationError refuses a write against generation Seq when Current is the latest, or 0
// when the session has none.
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

// Mark adds rs to what is reviewed of path on side at g. path is the head-side name, and a Range
// starting at 0 is the whole file. It returns *StaleGenerationError when g is not the latest.
func (s *Session) Mark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, "", []sided{{side: side, change: adding(rs)}})
}

// Unmark cuts rs out of what is reviewed and clears the file's changed-after-review record.
func (s *Session) Unmark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, path, []sided{{side: side, change: removing(rs)}})
}

// MarkHunk marks every anchor h has, both sides for a hunk that adds and removes.
func (s *Session) MarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, "", h.Anchors, adding)
}

func (s *Session) UnmarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, path, h.Anchors, removing)
}

// MarkFile marks every hunk of f, or the file as a whole when it has none.
func (s *Session) MarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, "", fileAnchors(f), adding)
}

func (s *Session) UnmarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, f.Diff.Path, fileAnchors(f), removing)
}

// Reviewed is every range at g by path, side and start line. A base-side row carries the file's
// base-side path.
func (s *Session) Reviewed(ctx context.Context, g Generation) ([]store.ReviewedRange, error) {
	return s.db.ReviewedRanges(ctx, g.ID)
}

// anchored writes every side in one transaction, since half a hunk landing is not a smaller true fact.
func (s *Session) anchored(
	ctx context.Context,
	g Generation,
	path, answers string,
	anchors []Anchor,
	arithmetic func([]Range) func([]store.LineRange) []store.LineRange,
) error {
	var changes []sided
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
		changes = append(changes, sided{side: side, change: arithmetic(rs)})
	}
	return s.updateReviewed(ctx, g, path, answers, changes)
}

func adding(rs []Range) func([]store.LineRange) []store.LineRange {
	return func(cur []store.LineRange) []store.LineRange {
		return lineRanges(merge(append(ranges(cur), rs...)))
	}
}

func removing(rs []Range) func([]store.LineRange) []store.LineRange {
	return func(cur []store.LineRange) []store.LineRange {
		return lineRanges(subtract(ranges(cur), rs))
	}
}

func fileAnchors(f File) []Anchor {
	if len(f.Hunks) == 0 {
		return []Anchor{{Side: wholeSide(f.Diff.Status)}}
	}

	var out []Anchor
	for _, h := range f.Hunks {
		out = append(out, h.Anchors...)
	}
	return out
}

type sided struct {
	side   store.Side
	change func([]store.LineRange) []store.LineRange
}

func (s *Session) updateReviewed(
	ctx context.Context,
	g Generation,
	path, answers string,
	changes []sided,
) error {
	out := make([]store.SideChange, 0, len(changes))
	for _, c := range changes {
		at := path
		if c.side == store.SideBase {
			var err error
			if at, err = s.basePath(ctx, g, path); err != nil {
				return err
			}
		}
		out = append(out, store.SideChange{Path: at, Side: c.side, Change: c.change})
	}

	now := time.Now().UTC().Truncate(time.Second)
	return s.stale(ctx, g, s.db.UpdateReviewedRanges(ctx, s.row.ID, g.ID, now, answers, out))
}

func (s *Session) stale(ctx context.Context, g Generation, err error) error {
	if !errors.Is(err, store.ErrStaleGeneration) {
		return err
	}

	latest, _, lookup := s.db.LatestGeneration(ctx, s.row.ID)
	if lookup != nil {
		return lookup
	}
	return &StaleGenerationError{Seq: g.Seq, Current: latest.Seq}
}

// basePath returns a path g does not hold as given, so a mark on a file that left the changeset is not an error.
func (s *Session) basePath(ctx context.Context, g Generation, path string) (string, error) {
	f, found, err := s.db.GenFile(ctx, g.ID, path)
	if err != nil || !found || f.OldPath == "" {
		return path, err
	}
	return f.OldPath, nil
}

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
