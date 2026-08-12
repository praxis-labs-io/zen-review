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
	return fmt.Sprintf("generation %d is not the current one, %d is: refresh and mark against what is there now",
		e.Seq, e.Current)
}

// Mark records lines of a file as reviewed at a generation.
//
// The ranges are unioned with what is already there and normalised, so marking
// the same lines twice is not two rows and marking either side of a gap does not
// close over it. A range starting at 0 is the file as a whole, which is how a
// file with no hunks is marked.
//
// Lines are explicit here. Naming a hunk is a question about the parsed diff and
// is answered a layer up.
func (s *Session) Mark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, side, func(cur []store.LineRange) []store.LineRange {
		return lineRanges(merge(append(ranges(cur), rs...)))
	})
}

// Unmark takes lines back out of what was reviewed, cutting a stored range where
// the two overlap rather than dropping it whole.
func (s *Session) Unmark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, side, func(cur []store.LineRange) []store.LineRange {
		return lineRanges(subtract(ranges(cur), rs))
	})
}

// Reviewed is every range recorded against a generation, ordered by path, side
// and start line.
func (s *Session) Reviewed(ctx context.Context, g Generation) ([]store.ReviewedRange, error) {
	return s.db.ReviewedRanges(ctx, g.ID)
}

// updateReviewed refuses a stale generation and hands the arithmetic to the
// store, which runs it inside the write transaction.
func (s *Session) updateReviewed(
	ctx context.Context,
	g Generation,
	path string,
	side store.Side,
	change func([]store.LineRange) []store.LineRange,
) error {
	latest, found, err := s.db.LatestGeneration(ctx, s.row.ID)
	if err != nil {
		return err
	}
	if !found || latest.ID != g.ID {
		return &StaleGenerationError{Seq: g.Seq, Current: latest.Seq}
	}

	now := time.Now().UTC().Truncate(time.Second)
	return s.db.UpdateReviewedRanges(ctx, s.row.ID, g.ID, path, side, now, change)
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
