package cli

import (
	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

type header struct {
	SessionID string
	Ref       string
	Kind      store.Kind
	Branch    string

	Base       review.Base
	Generation review.Generation

	Exists bool
	Stale  bool

	Skipped []string
}

type view struct {
	header

	Files      []diff.File
	Candidates *review.BaseCandidates
}

type changesetView struct {
	header

	Changeset review.Changeset
}

type staleness string

const (
	fresh     staleness = ""
	staleTree staleness = "tree"
	staleBase staleness = "base"
)

func (v header) reason() staleness {
	if !v.Stale || !v.Exists {
		return fresh
	}
	if v.Base.SHA != v.Generation.BaseSha {
		return staleBase
	}
	return staleTree
}

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
