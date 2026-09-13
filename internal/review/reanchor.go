package review

import (
	"context"
	"fmt"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// Reanchor carries n from one generation onto a later one, as a refresh carries a comment. False when its lines are gone.
func (s *Session) Reanchor(ctx context.Context, n Note, from, to Generation) (Note, bool, error) {
	if from.ID == to.ID {
		return n, true, nil
	}

	was, found, err := s.db.GenFile(ctx, from.ID, n.Path)
	if err != nil || !found {
		return Note{}, false, err
	}

	moved, err := s.sideMoved(ctx, n.Side, from, to)
	if err != nil {
		return Note{}, false, err
	}

	at := sidePath(n.Side, was)
	r := n.Range
	if f, changed := moved[at]; changed {
		var held bool
		if r, held = Translate(f).Anchor(r); !held {
			return Note{}, false, nil
		}
		at = f.Path
	}

	files, err := s.db.GenFiles(ctx, to.ID)
	if err != nil {
		return Note{}, false, fmt.Errorf("reading generation %d to move a comment onto it: %w", to.Seq, err)
	}
	for _, f := range files {
		if sidePath(n.Side, f) != at || sideBlob(n.Side, f) == "" {
			continue
		}
		n.Path, n.Range = f.Path, r
		return n, true, nil
	}
	return Note{}, false, nil
}

func (s *Session) sideMoved(ctx context.Context, side store.Side, from, to Generation) (map[string]diff.File, error) {
	if side == store.SideBase {
		return s.moved(ctx, from.BaseSha, to.BaseSha)
	}

	was, err := s.repo.Tree(ctx, from.CommitSha)
	if err != nil {
		return nil, err
	}
	now, err := s.repo.Tree(ctx, to.CommitSha)
	if err != nil {
		return nil, err
	}
	return s.moved(ctx, was, now)
}

func sidePath(side store.Side, f store.GenFile) string {
	if side == store.SideBase && f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}

func sideBlob(side store.Side, f store.GenFile) string {
	if side == store.SideBase {
		return f.BaseBlob
	}
	return f.HeadBlob
}
