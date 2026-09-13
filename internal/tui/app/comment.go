package app

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

type commentedMsg struct {
	r   Reload
	box review.Note
}

type editedMsg struct {
	r  Reload
	id string
}

type reanchoredMsg struct {
	r   Reload
	box review.Note
	now review.Note
}

type anchorGoneMsg struct {
	r   Reload
	box review.Note
	was review.Note
}

type reanchorFailedMsg struct{ err error }

func (m Model) commenting() (review.Note, bool) {
	if as, on := m.diff.Selected(); on {
		a := head(as)
		return review.NoteOnLines(m.diff.Path(), a.Side, a.Range, ""), true
	}

	if m.focus == focusTree {
		return m.fileNote(m.tree.Path())
	}

	f := m.fileAt(m.diff.Path())
	if f == nil {
		return review.Note{}, false
	}

	if len(f.Hunks) == 0 {
		return review.NoteOnFile(*f, ""), true
	}

	if as, on := m.diff.Line(); on {
		a := head(as)
		return review.NoteOnLines(f.Diff.Path, a.Side, a.Range, ""), true
	}

	h, ok := m.hunkAt(*f)
	if !ok {
		return review.Note{}, false
	}
	return review.NoteOnHunk(f.Diff.Path, h, ""), true
}

func (m Model) fileNote(path string) (review.Note, bool) {
	f := m.fileAt(path)
	if f == nil {
		return review.Note{}, false
	}
	return review.NoteOnFile(*f, ""), true
}

func head(as []review.Anchor) review.Anchor {
	for _, a := range as {
		if a.Side == store.SideHead {
			return a
		}
	}
	return as[0]
}

func (m *Model) commentOn() (tea.Cmd, bool) {
	n, ok := m.commenting()
	if !ok {
		return nil, false
	}

	cmd := m.openComment(n, m.stranded)
	m.stranded = ""
	return cmd, true
}

func (m *Model) openComment(n review.Note, body string) tea.Cmd {
	m.pending = n
	c := boxed(n)
	if cmd, up := m.diff.Compose(c, body); up {
		return cmd
	}
	return m.compose.Open(commentTitle(c), body)
}

func (m Model) boxBody() string {
	if m.diff.Composing() {
		return m.diff.Draft()
	}
	return m.compose.Value()
}

func (m *Model) reopen(msg reanchoredMsg) tea.Cmd {
	if m.pending != msg.box {
		m.moveOn(msg.r)
		return nil
	}

	body := m.boxBody()
	m.shut()
	m.apply(msg.r)

	if msg.now.Path != m.diff.Path() {
		if s, ok := m.firstOf(msg.now.Path); ok {
			m.land(s)
		}
	}

	m.note = notice{text: "generation " + strconv.Itoa(m.gen.Seq) + " landed before the save: the box is on " +
		where(boxed(msg.now)) + " now"}
	return m.openComment(msg.now, body)
}

func (m *Model) strand(msg anchorGoneMsg) {
	if m.pending != msg.box {
		m.moveOn(msg.r)
		return
	}

	body := m.boxBody()
	m.shut()
	m.apply(msg.r)

	m.stranded = body
	m.note = notice{text: where(boxed(msg.was)) + " went at generation " + strconv.Itoa(m.gen.Seq) +
		": c on the lines it belongs to brings the words back", bad: true}
}

func (m *Model) moveOn(r Reload) {
	was, d, moved := m.apply(r)
	m.note = said(was, d, m.gen.Seq, moved)
}

func (m *Model) editOn() (tea.Cmd, bool) {
	id, on := m.diff.Comment()
	if !on {
		return nil, false
	}
	c, ok := m.commentAt(id)
	if !ok {
		return nil, false
	}

	m.editing = id
	if cmd, up := m.diff.Edit(c); up {
		return cmd, true
	}
	return m.compose.Open(editTitle(c), c.Body), true
}

func (m Model) commentAt(id string) (store.Comment, bool) {
	if id == "" {
		return store.Comment{}, false
	}
	for _, c := range m.comments {
		if c.ID == id {
			return c, true
		}
	}
	return store.Comment{}, false
}

func (m *Model) crossOver() tea.Cmd {
	if !m.diff.Composing() || m.diff.FitsBox() {
		return nil
	}

	body := m.diff.Draft()
	m.diff.CloseDraft()
	return m.compose.Open(m.boxTitle(), body)
}

func (m Model) boxTitle() string {
	if c, ok := m.commentAt(m.editing); ok {
		return editTitle(c)
	}
	return commentTitle(boxed(m.pending))
}

func boxed(n review.Note) store.Comment {
	return store.Comment{
		Path:      n.Path,
		Side:      n.Side,
		Scope:     n.Scope,
		LineRange: store.LineRange{Start: n.Range.Start, End: n.Range.End},
	}
}

func commentTitle(c store.Comment) string { return "Comment on " + where(c) }

func editTitle(c store.Comment) string { return "Edit comment on " + where(c) }

func where(c store.Comment) string {
	at := comp.Safe(c.Path)
	if c.Scope != store.ScopeFile {
		at += ":" + strconv.Itoa(c.Start)
		if c.End != c.Start {
			at += "-" + strconv.Itoa(c.End)
		}
	}
	if c.Side == store.SideBase {
		at += " (base)"
	}
	return at
}

func (m Model) drafting(msg tea.Msg) (Model, tea.Cmd) {
	if !m.busy {
		m.note = notice{}
	}

	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(press, m.keys.Interrupt):
			return m, tea.Quit

		case key.Matches(press, m.compose.Keys.Discard):
			m.shut()
			return m, nil

		case key.Matches(press, m.compose.Keys.Save):
			if m.busy {
				m.note = notice{text: "still writing"}
				return m, nil
			}
			cmd := m.save(m.diff.Draft())
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.diff, cmd = m.diff.Update(msg)
	return m, cmd
}

func (m *Model) save(body string) tea.Cmd {
	body = strings.TrimRight(body, " \t\r\n")

	if m.editing != "" && strings.TrimSpace(body) == "" {
		m.note = notice{text: "cannot save an empty comment"}
		return nil
	}

	if m.editing != "" {
		return m.saveEdit(body)
	}
	return m.saveComment(body)
}

func (m *Model) saveEdit(body string) tea.Cmd {
	src, g, id := m.src, m.gen, m.editing
	m.busy = true

	return func() tea.Msg {
		r, err := src.EditComment(g, id, body)
		if err != nil {
			return failed(err)
		}
		return editedMsg{r: r, id: id}
	}
}

func (m *Model) saveComment(body string) tea.Cmd {
	if strings.TrimSpace(body) == "" {
		m.shut()
		return nil
	}

	src, g, box := m.src, m.gen, m.pending
	n := box
	n.Body = body
	m.busy = true

	return func() tea.Msg {
		r, err := src.AddComment(g, n)
		if err == nil {
			return commentedMsg{r: r, box: box}
		}
		if msg := failed(err); !refused(msg) {
			return msg
		}
		return reanchor(src, g, box, n)
	}
}

func refused(msg tea.Msg) bool {
	_, stale := msg.(staleMsg)
	return stale
}

func reanchor(src Source, from review.Generation, box, n review.Note) tea.Msg {
	r, err := src.Reload()
	if err != nil {
		return reanchorFailedMsg{err: err}
	}

	now, held, err := src.Reanchor(n, from, r.Generation)
	if err != nil {
		return reanchorFailedMsg{err: err}
	}
	if !held {
		return anchorGoneMsg{r: r, box: box, was: n}
	}
	return reanchoredMsg{r: r, box: box, now: now}
}
