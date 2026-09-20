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

// Mark adds rs to what is reviewed of path on side at g.
func (s *Session) Mark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, "", []sided{{side: side, change: adding(rs)}})
}

// Unmark cuts rs out of what is reviewed.
func (s *Session) Unmark(ctx context.Context, g Generation, path string, side store.Side, rs []Range) error {
	return s.updateReviewed(ctx, g, path, path, []sided{{side: side, change: removing(rs)}})
}

// MarkHunk marks every anchor h has.
func (s *Session) MarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, "", h.Anchors, adding)
}

func (s *Session) UnmarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, path, h.Anchors, removing)
}

// MarkFile marks every hunk of f.
func (s *Session) MarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, "", fileAnchors(f), adding)
}

func (s *Session) UnmarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, f.Diff.Path, fileAnchors(f), removing)
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

// fileAnchors spans every hunk, or the side f has bytes on when it has none, since a file with
// no hunks still has to be markable.
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

func now() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}
