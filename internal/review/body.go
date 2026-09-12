package review

import (
	"context"
	"fmt"

	"github.com/praxis-labs-io/zen-review/internal/store"
)

// Body is a file's whole text at one generation, and the side it is numbered on.
//
// Lines is empty for a file the generation does not hold, and for one whose blob
// the repository cannot read: an embedded repository records a commit rather than
// a blob, and a caller with nothing to draw is better served than one handed an
// error it cannot act on.
type Body struct {
	Side  store.Side
	Lines []string
}

// Body is the whole of one file at a generation, which is what a reader asks for
// when three lines of context are not enough to judge a hunk.
//
// The side is the one the file has bytes on, the same rule a whole-file mark
// takes: the head, or the base for a file the changeset deleted.
func (s *Session) Body(ctx context.Context, g Generation, path string) (Body, error) {
	f, found, err := s.db.GenFile(ctx, g.ID, path)
	if err != nil {
		return Body{}, err
	}
	if !found {
		return Body{}, nil
	}

	side := wholeSide(f.Status)
	sha := f.HeadBlob
	if side == store.SideBase {
		sha = f.BaseBlob
	}
	if sha == "" {
		return Body{Side: side}, nil
	}

	blobs, err := s.repo.Blobs(ctx, []string{sha})
	if err != nil {
		return Body{}, fmt.Errorf("reading %s at generation %d: %w", path, g.Seq, err)
	}

	b, held := blobs[sha]
	if !held {
		return Body{Side: side}, nil
	}
	return Body{Side: side, Lines: text(b)}, nil
}
