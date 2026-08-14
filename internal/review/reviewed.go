package review

import (
	"context"
	"errors"
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
	return s.updateReviewed(ctx, g, path, "", []sided{{side: side, change: adding(rs)}})
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
	return s.updateReviewed(ctx, g, path, path, []sided{{side: side, change: removing(rs)}})
}

// MarkHunk records every anchor a hunk has, which is what reading one means. A
// hunk that both adds and removes takes a mark on each side: the lines it
// removes are not lines it has, and a mark on the additions alone would swallow
// a deletion arriving later.
func (s *Session) MarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, "", h.Anchors, adding)
}

// UnmarkHunk takes back what MarkHunk recorded.
func (s *Session) UnmarkHunk(ctx context.Context, g Generation, path string, h Hunk) error {
	return s.anchored(ctx, g, path, path, h.Anchors, removing)
}

// MarkFile records every anchor of every hunk, or the file as a whole when it
// has none.
func (s *Session) MarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, "", fileAnchors(f), adding)
}

// UnmarkFile takes back what MarkFile recorded.
func (s *Session) UnmarkFile(ctx context.Context, g Generation, f File) error {
	return s.anchored(ctx, g, f.Diff.Path, f.Diff.Path, fileAnchors(f), removing)
}

// Reviewed is every range recorded against a generation, ordered by path, side
// and start line.
//
// A base-side row carries the file's base-side path, so a caller grouping these
// by file joins a renamed one back through the changeset's old path.
func (s *Session) Reviewed(ctx context.Context, g Generation) ([]store.ReviewedRange, error) {
	return s.db.ReviewedRanges(ctx, g.ID)
}

// anchored gathers the anchors by side, head first, and writes every side at
// once.
//
// One write and not one per side. Reading a hunk means reading both of the sides
// it touches, so a half of it that landed is not a smaller version of the same
// fact, and a caller told the write failed would be looking at that half.
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

// adding and removing are the two directions a write goes: union with what is
// already recorded, or cut out of it where the two overlap.
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

// fileAnchors is every anchor of every hunk, or the whole file when it has none:
// a binary file, a mode change, a rename that moved nothing. The zero Range is
// the whole-file mark, which merge and subtract already keep apart from the line
// ranges.
//
// The side comes from wholeSide, the same function the read uses, so a mark
// always lands where Derive looks for it.
func fileAnchors(f File) []Anchor {
	if len(f.Hunks) == 0 {
		return []Anchor{{Side: wholeSide(f.Diff)}}
	}

	var out []Anchor
	for _, h := range f.Hunks {
		out = append(out, h.Anchors...)
	}
	return out
}

// sided is one side's arithmetic, before the base-side path is settled.
type sided struct {
	side   store.Side
	change func([]store.LineRange) []store.LineRange
}

// updateReviewed names each side's file and hands the arithmetic to the store,
// which runs all of it inside one transaction and refuses a stale generation
// from inside it.
//
// path is the file's head-side name throughout. answers is that same name when
// the write settles a recorded cut, because gen_files keys on it whichever side
// the ranges land on.
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

// stale turns the store's refusal into the sentence a reader gets, naming the
// generation that is current now.
//
// The store refuses from inside the writing transaction, where it knows only
// that the number did not match. The number it did not match is read back here,
// for the message alone: the write is already refused, and what the reader does
// next is refresh to whatever is there by then anyway. A read that fails takes
// the answer, because a database that cannot be read is the larger of the two
// facts and the refusal comes back the moment it can be.
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
