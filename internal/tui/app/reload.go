package app

import (
	"errors"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

// ErrSaved marks a write that committed and could not be read back. A Source wraps it
// so the reader is not offered a retry that would write twice.
var ErrSaved = errors.New("the write was saved and the screen is behind it")

// Reload is the session as it stands at one generation. Every field is read at that generation.
type Reload struct {
	Base       review.Base
	Generation review.Generation
	Changeset  review.Changeset

	Comments []store.Comment

	Replaced map[string][]string

	Summary string
}

// Source is what the reader calls into review through. Calls run off the update loop,
// never two at once, and it must not be nil.
type Source interface {
	Reload() (Reload, error)
	Candidates() (review.BaseCandidates, error)
	SetBase(ref string) (Reload, error)

	MarkHunk(g review.Generation, path string, h review.Hunk) (Reload, error)
	UnmarkHunk(g review.Generation, path string, h review.Hunk) (Reload, error)
	MarkFile(g review.Generation, f review.File) (Reload, error)
	UnmarkFile(g review.Generation, f review.File) (Reload, error)

	AddComment(g review.Generation, n review.Note) (Reload, error)

	ResolveComment(g review.Generation, id string) (Reload, error)

	EditComment(g review.Generation, id, body string) (Reload, error)
	DeleteComment(g review.Generation, id string) (Reload, error)

	SetSummary(text string) (string, error)

	Body(g review.Generation, path string) (review.Body, error)
}

type basesLoadedMsg struct{ candidates review.BaseCandidates }
type basesFailedMsg struct{ err error }
type baseSetMsg struct {
	ref string
	r   Reload
}
type baseSetFailedMsg struct{ err error }

type bodyLoadedMsg struct {
	path string
	gen  int64
	body review.Body
}

type bodyFailedMsg struct {
	path string
	gen  int64
	err  error
}

type reloadedMsg struct{ r Reload }

type reloadFailedMsg struct{ err error }

func (m Model) reload() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		r, err := src.Reload()
		if err != nil {
			return reloadFailedMsg{err: err}
		}
		return reloadedMsg{r: r}
	}
}

func (m Model) loadBody(path string) tea.Cmd {
	src, g := m.src, m.gen
	return func() tea.Msg {
		b, err := src.Body(g, path)
		if err != nil {
			return bodyFailedMsg{path: path, gen: g.ID, err: err}
		}
		return bodyLoadedMsg{path: path, gen: g.ID, body: b}
	}
}

func (m Model) loadBases() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		candidates, err := src.Candidates()
		if err != nil {
			return basesFailedMsg{err: err}
		}
		return basesLoadedMsg{candidates: candidates}
	}
}

func (m Model) setBase(ref string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		r, err := src.SetBase(ref)
		if err != nil {
			return baseSetFailedMsg{err: err}
		}
		return baseSetMsg{ref: ref, r: r}
	}
}

// notice clears on the next key and never on a clock, because nothing on screen moves without a press.
type notice struct {
	text string
	bad  bool
}

type drift int

const (
	held drift = iota

	shifted

	dropped
)

type mark struct {
	at stop

	// from is the base-side name, which stays put across repeated renames.
	from string

	nth    int
	inFile int
}

func (m Model) mark() mark {
	k := mark{at: m.cursor, from: m.cursor.path}
	for _, f := range m.changeset.Files {
		if f.Diff.Path == m.cursor.path && f.Diff.OldPath != "" {
			k.from = f.Diff.OldPath
			break
		}
	}

	for _, s := range m.stops() {
		if s.path == k.at.path && s.side == k.at.side && s.line == k.at.line {
			break
		}
		k.nth++
		if s.path == k.at.path {
			k.inFile++
		}
	}
	return k
}

func (m *Model) apply(r Reload) (stop, drift, bool) {
	k := m.mark()
	at, was := m.diff.Cursor(), m.diff.Scroll().Offset
	moved := r.Generation.ID != m.gen.ID

	m.base, m.gen, m.changeset, m.comments, m.replaced = r.Base, r.Generation, r.Changeset, r.Comments, r.Replaced
	m.summary = r.Summary
	m.tree.SetChangeset(m.changeset)
	m.diff.SetFile(nil, nil, nil, 0)

	m.cursor = stop{}
	s, d, ok := m.landing(k)
	if ok {
		m.land(s)

		if !moved {
			m.diff.Restore(at, was)
			m.syncCursor()
		}
	}
	return k.at, d, moved
}

// landing falls back to an ordinal rather than the nearest line, which an insert at the top of a file would throw off.
func (m Model) landing(k mark) (stop, drift, bool) {
	stops := m.stops()
	if len(stops) == 0 {
		return stop{}, dropped, false
	}

	path := m.nowAt(k)

	var in []stop
	for _, s := range stops {
		if s.path == path {
			in = append(in, s)
		}
	}
	if len(in) == 0 {
		return stops[min(k.nth, len(stops)-1)], dropped, true
	}

	d := held
	if path != k.at.path {
		d = shifted
	}

	for _, s := range in {
		if s.side == k.at.side && s.line == k.at.line {
			return s, d, true
		}
	}
	return in[min(k.inFile, len(in)-1)], shifted, true
}

// nowAt matches a rename before a plain path, so a file renamed away and replaced by a new one follows its content.
func (m Model) nowAt(k mark) string {
	for _, f := range m.changeset.Files {
		if f.Diff.OldPath != "" && f.Diff.OldPath == k.from {
			return f.Diff.Path
		}
	}
	for _, f := range m.changeset.Files {
		if f.Diff.Path == k.at.path {
			return f.Diff.Path
		}
	}
	return ""
}

func said(was stop, d drift, seq int, moved bool) notice {
	if !moved {
		return notice{text: "up to date"}
	}

	gen := "generation " + strconv.Itoa(seq)
	if was.path == "" {
		return notice{text: gen}
	}

	switch d {
	case shifted:
		return notice{text: gen + ", " + comp.Safe(was.path) + " moved"}
	case dropped:
		return notice{text: gen + ", " + comp.Safe(was.path) + " is gone"}
	}
	return notice{text: gen}
}
