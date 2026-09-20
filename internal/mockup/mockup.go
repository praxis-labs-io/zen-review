// Package mockup renders the reader over a fixture changeset, with no repository and no database.
package mockup

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
)

// Mock is the fixture standing in for a session. It mutates, so a hunk marked in the reader reads
// reviewed in the frame that follows, and its state lives only as long as the process.
//
// The mutex is not for two writes at once, which the reader never issues, but for a body read
// crossing one.
type Mock struct {
	mu sync.Mutex

	files  []diff.File
	bodies map[string][]string

	base     review.Base
	rows     []store.ReviewedRange
	comments []store.Comment
	summary  string

	written int
}

// New builds the fixture session: one changeset at generation 2, part of it already read.
func New() *Mock {
	return &Mock{
		files:    files(),
		bodies:   bodies(),
		base:     base(),
		rows:     reviewed(),
		comments: comments(),
		summary:  summary,
	}
}

// Reload hands back what the fixture holds now, at the generation it has always been at, so a
// refresh reports no news rather than reshuffling a demo mid-screenshot.
func (m *Mock) Reload() (app.Reload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.at(), nil
}

func (m *Mock) Candidates() (review.BaseCandidates, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return candidates(), nil
}

// SetBase moves the ref the changeset claims to be measured from. The fixture diff is the same one.
func (m *Mock) SetBase(ref string) (app.Reload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.base = review.Base{Ref: ref, SHA: shaOf(ref, m.base.SHA)}
	return m.at(), nil
}

func (m *Mock) MarkHunk(_ review.Generation, path string, h review.Hunk) (app.Reload, error) {
	return m.marking(path, h.Anchors, adding)
}

func (m *Mock) UnmarkHunk(_ review.Generation, path string, h review.Hunk) (app.Reload, error) {
	return m.marking(path, h.Anchors, review.Subtract)
}

func (m *Mock) MarkFile(_ review.Generation, f review.File) (app.Reload, error) {
	return m.marking(f.Diff.Path, review.FileAnchors(f), adding)
}

func (m *Mock) UnmarkFile(_ review.Generation, f review.File) (app.Reload, error) {
	return m.marking(f.Diff.Path, review.FileAnchors(f), review.Subtract)
}

func (m *Mock) AddComment(_ review.Generation, n review.Note) (app.Reload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.written++
	now := time.Now().UTC()

	c := comment("w"+strconv.Itoa(m.written), n.Path, n.Range.Start, n.Range.End, n.Scope, n.Body)
	c.Side, c.CreatedAt, c.UpdatedAt = n.Side, now, now

	m.comments = append(m.comments, c)
	return m.at(), nil
}

// Reanchor is never reached: the fixture refuses no write, so nothing sends the reader back here.
func (m *Mock) Reanchor(n review.Note, _, _ review.Generation) (review.Note, bool, error) {
	return n, true, nil
}

func (m *Mock) ResolveComment(_ review.Generation, id string) (app.Reload, error) {
	return m.amend(id, func(c *store.Comment) { c.State = store.CommentResolved })
}

func (m *Mock) EditComment(_ review.Generation, id, body string) (app.Reload, error) {
	return m.amend(id, func(c *store.Comment) {
		c.Body = body
		c.UpdatedAt = time.Now().UTC()
	})
}

func (m *Mock) DeleteComment(_ review.Generation, id string) (app.Reload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, c := range m.comments {
		if c.ID == id {
			m.comments = append(m.comments[:i:i], m.comments[i+1:]...)
			return m.at(), nil
		}
	}
	return app.Reload{}, fmt.Errorf("no comment %s in the fixture", id)
}

func (m *Mock) SetSummary(text string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.summary = text
	return m.summary, nil
}

func (m *Mock) Body(_ review.Generation, path string) (review.Body, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, found := m.file(path)
	if !found {
		return review.Body{}, fmt.Errorf("no file %s in the fixture", path)
	}
	return review.Body{Side: sideOf(f), Lines: m.bodies[path]}, nil
}

// at is the fixture as one generation's worth of state. Every call derives a fresh changeset,
// because the panes point into the files it hands back.
func (m *Mock) at() app.Reload {
	return app.Reload{
		Base:       m.base,
		Generation: generation(),
		Changeset:  review.Derive(m.files, m.rows, cut()),
		Comments:   append([]store.Comment(nil), m.comments...),
		Replaced:   replaced(),
		Summary:    m.summary,
	}
}

// marking applies arithmetic to every side the anchors touch, the way a write to the database
// would: a base-side range is filed under the file's base-side name.
func (m *Mock) marking(
	path string,
	anchors []review.Anchor,
	arithmetic func(cur, rs []review.Range) []review.Range,
) (app.Reload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, found := m.file(path)
	if !found {
		return app.Reload{}, fmt.Errorf("no file %s in the fixture", path)
	}

	for _, side := range []store.Side{store.SideHead, store.SideBase} {
		var rs []review.Range
		for _, a := range anchors {
			if a.Side == side {
				rs = append(rs, a.Range)
			}
		}
		if len(rs) == 0 {
			continue
		}

		at := path
		if side == store.SideBase {
			at = f.BasePath()
		}
		m.rows = filed(m.rows, at, side, arithmetic(held(m.rows, at, side), rs))
	}
	return m.at(), nil
}

func (m *Mock) amend(id string, to func(*store.Comment)) (app.Reload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.comments {
		if m.comments[i].ID == id {
			to(&m.comments[i])
			return m.at(), nil
		}
	}
	return app.Reload{}, fmt.Errorf("no comment %s in the fixture", id)
}

func (m *Mock) file(path string) (diff.File, bool) {
	for _, f := range m.files {
		if f.Path == path {
			return f, true
		}
	}
	return diff.File{}, false
}

func adding(cur, rs []review.Range) []review.Range { return review.Merge(append(cur, rs...)) }

func held(rows []store.ReviewedRange, path string, side store.Side) []review.Range {
	var out []review.Range
	for _, r := range rows {
		if r.Path == path && r.Side == side {
			out = append(out, review.Range{Start: r.Start, End: r.End})
		}
	}
	return out
}

func filed(rows []store.ReviewedRange, path string, side store.Side, rs []review.Range) []store.ReviewedRange {
	out := make([]store.ReviewedRange, 0, len(rows)+len(rs))
	for _, r := range rows {
		if r.Path != path || r.Side != side {
			out = append(out, r)
		}
	}
	for _, r := range rs {
		out = append(out, store.ReviewedRange{
			Path:      path,
			Side:      side,
			LineRange: store.LineRange{Start: r.Start, End: r.End},
			CreatedAt: time.Now().UTC(),
		})
	}
	return out
}

var _ app.Source = (*Mock)(nil)
