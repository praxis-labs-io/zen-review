package cli

import (
	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// header is what every command says about the session before it says anything
// about the changeset. A refresh builds the generation and a status reads the
// one already recorded, and past this point there is one thing to print.
type header struct {
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

	// Skipped names the paths git could not read into the snapshot just taken. It
	// is a property of the work tree rather than of the generation, which is why
	// it sits here and not under one: a session with nothing built yet still has
	// to be able to say a file is missing from what is about to be reviewed.
	Skipped []string
}

// view is the changeset as a summary: what moved, and how much of it. status and
// refresh answer with this.
type view struct {
	header

	Files      []diff.File
	Candidates *review.BaseCandidates
}

// changesetView is the same session with the review derived on it, which is what
// files, review and unreview answer with.
//
// It holds the engine's Changeset rather than a second []diff.File. Two lists of
// the same files in one struct is how the two of them come to disagree.
type changesetView struct {
	header

	Changeset review.Changeset
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
func (v header) reason() staleness {
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

// statusHeader is the read path.
func statusHeader(s *review.Session, st review.Status) header {
	return header{
		SessionID:  st.SessionID,
		Ref:        s.Ref(),
		Kind:       st.Kind,
		Branch:     st.Branch,
		Base:       st.Base,
		Generation: st.Generation,
		Exists:     st.Exists,
		Stale:      st.Stale,
		Skipped:    st.Skipped,
	}
}

// generationView is the build path. A generation that was just built, or just
// confirmed to still hold, describes the snapshot it was measured against, so
// nothing about it is stale.
func generationView(s *review.Session, g review.Generation, files []diff.File) view {
	return view{
		header: header{
			SessionID:  s.ID(),
			Ref:        s.Ref(),
			Kind:       s.Kind(),
			Branch:     s.Branch(),
			Base:       s.Base(),
			Generation: g,
			Exists:     true,
			Skipped:    g.Skipped,
		},
		Files: files,
	}
}
