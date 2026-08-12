package cli

import (
	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// view is what every command answers with, whichever way it got there. A
// refresh builds the generation and a status reads the one already recorded,
// and past this point there is one thing to print.
type view struct {
	SessionID string
	Ref       string
	Kind      store.Kind
	Branch    string

	// Base is what the changeset would be measured from now. Generation carries
	// what it was measured from when it was built, and a rebase moves the first
	// without moving the second, so Files belongs to the second one.
	Base       review.Base
	Generation review.Generation

	// Exists is false on a session that has never refreshed, where Generation is
	// the zero value and Files is empty.
	Exists bool
	Stale  bool

	Files []diff.File
}

// staleness is why a view no longer describes what is on disk.
type staleness string

const (
	fresh     staleness = ""
	staleTree staleness = "tree"
	staleBase staleness = "base"
)

// reason splits the engine's single Stale bool into the two different things it
// means. holds compares the base and the tree together, so a true covers both a
// rebase and an edit, and the reader wants to be told which one happened. Both
// bases are already in hand, so telling them apart costs nothing.
func (v view) reason() staleness {
	// A session with no generation is stale because nothing has been reviewed
	// against yet, and no base and no tree moved to make it so. The absent
	// generation is the whole story and a reason would be a second, worse one.
	if !v.Stale || !v.Exists {
		return fresh
	}
	if v.Base.SHA != v.Generation.BaseSha {
		return staleBase
	}
	return staleTree
}

// statusView is the read path.
func statusView(s *review.Session, st review.Status) view {
	return view{
		SessionID:  st.SessionID,
		Ref:        s.Ref(),
		Kind:       st.Kind,
		Branch:     st.Branch,
		Base:       st.Base,
		Generation: st.Generation,
		Exists:     st.Exists,
		Stale:      st.Stale,
		Files:      st.Files,
	}
}

// generationView is the build path. A generation that was just built, or just
// confirmed to still hold, describes the snapshot it was measured against, so
// nothing about it is stale.
func generationView(s *review.Session, g review.Generation, files []diff.File) view {
	return view{
		SessionID:  s.ID(),
		Ref:        s.Ref(),
		Kind:       s.Kind(),
		Branch:     s.Branch(),
		Base:       s.Base(),
		Generation: g,
		Exists:     true,
		Files:      files,
	}
}
