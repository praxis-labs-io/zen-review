package app

import (
	"errors"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

// ErrSaved is a write that committed and could not be read back. A Source wraps
// it, because this is the one failure a retry would write a second time.
var ErrSaved = errors.New("the write was saved and the screen is behind it")

// Reload is the changeset as it now stands, which is what a press of the reload
// key brings back.
//
// The base comes with it because a refresh re-resolves the merge base, and the
// fact on screen names the sha. Reading it off the session instead would race
// the goroutine that just wrote it.
type Reload struct {
	Base       review.Base
	Generation review.Generation
	Changeset  review.Changeset

	// Comments come with the changeset rather than from a second call. The two
	// have to be one generation, or a card hangs under a line that has moved.
	Comments []store.Comment

	// Summary is the session note, which C opens over. It rides along rather
	// than being read off the session, which a command would race.
	Summary string
}

// Source rebuilds the changeset from what is on disk.
//
// It is an interface rather than review.Session because a render test has no
// repository to open one against, and a rendered frame is what the tests drive.
// cli implements it over the live session.
//
// It takes no context. Bubble Tea puts none on a command, and the alternative
// is a context field on a model that outlives every call made through it.
//
// It is never called twice at once, and it must not be nil. The model runs it
// off the update loop and holds a flag saying one is in flight; an
// implementation over a session is what that flag protects.
type Source interface {
	Reload() (Reload, error)

	// The writes name the generation on screen, which is what everything
	// written anchors to, and re-derive at it rather than refreshing.
	MarkHunk(g review.Generation, path string, h review.Hunk) (Reload, error)
	UnmarkHunk(g review.Generation, path string, h review.Hunk) (Reload, error)
	MarkFile(g review.Generation, f review.File) (Reload, error)
	UnmarkFile(g review.Generation, f review.File) (Reload, error)

	// AddComment writes one, so the card comes back hanging under the code it
	// was written against.
	AddComment(g review.Generation, n review.Note) (Reload, error)

	// ResolveComment settles one comment, so the card the reader is on comes
	// back under the state the write left it in.
	ResolveComment(g review.Generation, id string) (Reload, error)

	// SetSummary writes the session note and hands back what review stored. It
	// names no generation: the note is the session's rather than a snapshot's.
	SetSummary(text string) (string, error)
}

// reloadedMsg is a reload that came back.
type reloadedMsg struct{ r Reload }

// reloadFailedMsg is one that did not. Nothing on screen moves for it.
type reloadFailedMsg struct{ err error }

// reload runs the source off the update loop, so a key is never waiting on git.
//
// The source is lifted out of the model first and the closure carries it alone.
// A command runs on a goroutine, and the model it was built from goes on being
// written by Update.
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

// notice is what the last reload did, shown at the right of the status bar.
//
// It is cleared by the next key and never by a clock. Nothing on screen moves
// without a press, and a line that vanished on its own would be one more thing
// moving under a reader mid-hunk.
type notice struct {
	text string
	tone tone
}

// tone is how a notice reads. Anything but plain takes the whole bar, being a
// sentence rather than a label.
type tone int

const (
	plain tone = iota
	warn
	bad
)

// drift is what a reload did to the hunk the cursor was on.
type drift int

const (
	// held is the same hunk still being there under the same name, which is
	// every reload that changed nothing and most of the ones that changed
	// something.
	held drift = iota

	// shifted is the file surviving and the hunk not, a rename included.
	shifted

	// dropped is the file leaving the changeset.
	dropped
)

// mark is where the cursor sat before a reload: what it named, and the two
// places it sat in.
//
// The ordinals are what is left to go on once the hunk it named has gone. They
// are read before the changeset is replaced, because that is the list they
// count in.
type mark struct {
	at stop

	// from is the file's name on the base side, which is the name that does not
	// move. Every generation is diffed from the same base, so a file renamed
	// twice is still reported against the name it started with, and matching on
	// the name it had last generation loses it the second time.
	from string

	// nth is the stop's place in the whole changeset, and inFile its place among
	// the stops of its own file.
	nth    int
	inFile int
}

// mark reads where the cursor is now.
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

// apply puts a reload on screen: the new changeset into both panes, and the
// cursor back on the hunk it was on.
//
// The diff pane is blanked first. It is the only holder of a *review.File
// pointing into the old changeset once the tree has rebuilt, and land re-points
// it only when the path changed, which after a reload it usually has not.
func (m *Model) apply(r Reload) (stop, drift, bool) {
	k := m.mark()
	at, was := m.diff.Cursor(), m.diff.Scroll().Offset
	moved := r.Generation.ID != m.gen.ID

	m.base, m.gen, m.changeset, m.comments = r.Base, r.Generation, r.Changeset, r.Comments
	m.summary = r.Summary
	m.tree.SetChangeset(m.changeset)
	m.diff.SetFile(nil, nil, 0)

	m.cursor = stop{}
	s, d, ok := m.landing(k)
	if ok {
		m.land(s)

		// A generation the refresh did not build is the same bytes on screen, so
		// the reader keeps the row they had walked to.
		if !moved {
			m.diff.Restore(at, was)
			m.syncCursor()
		}
	}
	return k.at, d, moved
}

// landing is where the cursor goes after the changeset moved under it, and what
// happened to it on the way.
//
// A hunk is named by identity and not by position, so the usual answer is that
// the same one is still there and the reader is left exactly where they were.
//
// Everything else falls back to an ordinal rather than to the nearest line
// number. An agent inserting two hundred lines at the top of a file moves every
// hunk in it down by two hundred, and nearest-by-line would put the reader back
// on the first hunk of a file they had read most of. Third of three stays third
// of three.
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

	// A rename is the same file under another name, so the hunk can survive it
	// whole. The reader is still told, because the row they were on is not the
	// row they are on.
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

// nowAt is where the marked file lives in the changeset: under the name it had,
// or under the one a rename gave it. Empty when the changeset no longer holds
// it.
//
// The rename is matched on the base-side name rather than the one on screen, so
// a file renamed a second time is still the same file.
//
// A rename beats a plain match, and the two passes are what makes it. An agent
// renaming a.go to c.go and writing a new a.go in the same generation leaves
// both on screen, and the one the reader was reading is the one their content
// went to.
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

// said is what the bar reports a reload found.
//
// A generation the refresh did not move means the work tree has not moved
// either. That is the whole of the staleness question, and the reason nothing
// here asks Session.Status: that one snapshots the tree to answer it, for the
// price of the refresh that was just run anyway.
func said(was stop, d drift, seq int, moved bool) notice {
	if !moved {
		return notice{text: "up to date"}
	}

	// A changeset that had nothing in it has no file to have lost, whatever the
	// drift says.
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
