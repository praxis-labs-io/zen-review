package diffpane

import (
	"strings"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
)

// body is a file's whole text in the pane: the lines as they draw, their
// colours, and the side they are numbered on.
//
// It is tokenised once when it arrives rather than on every layout. A lexer
// carries state across lines, so the whole of it goes through in one pass, and
// the cost of that on a long file is not one a resize should pay again.
type body struct {
	side   store.Side
	lines  []string
	tokens [][]syntax.Token
}

// TogglePreview turns the whole file on or off around the hunks, and reports
// whether the key took. Turning it off always does.
//
// It is refused on a file with no text to fill in: a binary one, and one whose
// blob the repository could not read. The mode is what the reader asked for and
// previewing is what they are getting, the same two facts split as the side-by-side
// pair, because the body arrives a keystroke after the press.
func (m *Model) TogglePreview() bool {
	if m.preview != "" {
		m.preview = ""
		m.settle()
		return true
	}
	if !m.hasText() {
		return false
	}

	m.preview = m.file.Diff.Path
	m.settle()
	return true
}

// settle rebuilds the rows for the mode and opens the window on the hunk the
// reader is in.
//
// Preview moves a row by as many lines as the file holds above it, and the
// shortest scroll onto the screen is the wrong one over that distance: it lands
// the hunk on the bottom row with every line of it off the window. A hunk
// already on screen whole is left alone, the way the ring leaves one.
func (m *Model) settle() {
	m.remode()
	m.reveal()
	m.scrollToCursor()
}

// StopPreview stands the mode down, for a read that failed. Nothing is cached,
// so the next press asks again rather than refusing on a stale answer.
func (m *Model) StopPreview() {
	if m.preview == "" {
		return
	}
	m.preview = ""
	m.settle()
}

// OffHunk is whether the cursor is on a line of the file belonging to no hunk,
// which is a row only the whole file drawn around them has.
//
// This is not Hunk answering false. A comment card outside every hunk answers
// false too, and the ring is what put the reader on one, so a mark key there
// falls back to the stop it came from. A line is different: nothing put the
// reader on it but their own movement, and the hunk the ring last named is not
// what they are looking at.
func (m Model) OffHunk() bool {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return false
	}
	r := m.rows[m.cursor]
	return r.kind == codeRow && r.hunk < 0
}

// previewing is whether the pane draws the whole file, which takes the reader's
// answer and a body to honour it with.
func (m Model) previewing() bool {
	return m.preview != "" && m.body().lines != nil
}

// body is the text of the file in the pane, and the zero value before it lands.
func (m Model) body() body {
	if m.file == nil {
		return body{}
	}
	return m.bodies[m.file.Diff.Path]
}

// hasText is whether the file in the pane has lines a preview could fill in.
// A body already read and empty is as settled an answer as a binary header.
func (m Model) hasText() bool {
	if m.file == nil || m.file.Diff.Binary {
		return false
	}
	b, read := m.bodies[m.file.Diff.Path]
	return !read || b.lines != nil
}

// NeedsBody is the file whose text the pane is waiting on, and false when it is
// not waiting. The root runs the read; the pane holds what comes back.
//
// It takes the same view of a file with no text as the key does. The mode lasts
// the run, so walking onto a binary file with it on would otherwise read a blob
// that is not lines and draw it as though it were.
func (m Model) NeedsBody() (string, bool) {
	if m.preview == "" || !m.hasText() {
		return "", false
	}
	if _, read := m.bodies[m.file.Diff.Path]; read {
		return "", false
	}
	return m.file.Diff.Path, true
}

// SetBody puts a file's text in the pane, reporting whether there was any to
// draw. One that came back empty stands the mode down rather than leaving the
// reader on a key that appeared to do nothing.
//
// It is kept either way, so a file with nothing to show is asked for once.
func (m *Model) SetBody(path string, b review.Body) bool {
	if m.bodies == nil {
		m.bodies = make(map[string]body)
	}
	m.bodies[path] = m.tokenise(path, b)

	if m.file == nil || m.file.Diff.Path != path {
		return len(b.Lines) > 0
	}
	if len(b.Lines) == 0 {
		m.preview = ""
		return false
	}

	m.settle()
	return true
}

// tokenise colours a whole file, by the name it has on the side it came back on.
func (m *Model) tokenise(path string, b review.Body) body {
	if len(b.Lines) == 0 {
		return body{side: b.Side}
	}

	safe := make([]string, len(b.Lines))
	for i, line := range b.Lines {
		safe[i] = comp.Code(line)
	}

	// Sized to the file rather than to what came back, the way a card's block is:
	// Chroma drops a trailing blank line and every row here indexes by number.
	tokens := make([][]syntax.Token, len(safe))
	copy(tokens, m.syntax.Lines(path, strings.Join(safe, "\n")))

	return body{side: b.Side, lines: safe, tokens: tokens}
}

// fill is a run of unchanged lines preview draws where the diff showed a gap:
// the first and last line of the file it covers, and the offset the other side's
// numbers carry there.
type fill struct {
	from, to int
	delta    int
}

// lines is the run as the context lines a hunk would hold it as, so the rows it
// draws come through the same builder every other row does.
//
// A base-side body is a deleted file, which has no head at all, so those lines
// carry no head number rather than one worked back from an offset.
func (f fill) lines(b body) []diff.Line {
	out := make([]diff.Line, 0, max(0, f.to-f.from+1))
	for n := f.from; n <= f.to && n <= len(b.lines); n++ {
		l := diff.Line{Kind: diff.Context, Text: b.lines[n-1]}
		if b.side == store.SideBase {
			l.Old = n
		} else {
			l.Old, l.New = n+f.delta, n
		}
		out = append(out, l)
	}
	return out
}

// tokens is the run's colours, taken off the whole-file pass by line number.
func (f fill) tokens(b body) [][]syntax.Token {
	from, to := min(max(0, f.from-1), len(b.tokens)), min(f.to, len(b.tokens))
	if from >= to {
		return nil
	}
	return b.tokens[from:to]
}

// fills is the unchanged runs preview draws: one above each hunk and one closing
// the file. Empty runs are kept, so the slice indexes by hunk.
func (m Model) fills() []fill {
	b := m.body()
	out := make([]fill, 0, len(m.file.Hunks)+1)

	at, delta := 1, 0
	for _, h := range m.file.Hunks {
		first, past := bounds(h.Diff.NewStart, h.Diff.NewLines)
		baseFirst, basePast := bounds(h.Diff.OldStart, h.Diff.OldLines)

		// A base-side body is a deleted file. Its runs are measured on the side it
		// has, and its lines carry no head number for an offset to reach.
		if b.side == store.SideBase {
			out = append(out, fill{from: at, to: baseFirst - 1})
			at = basePast
			continue
		}

		out = append(out, fill{from: at, to: first - 1, delta: delta})
		at, delta = past, basePast-past
	}
	return append(out, fill{from: at, to: len(b.lines), delta: delta})
}

// bounds is the lines a hunk covers on one side: the first, and the first past it.
//
// A zero count covers none and names the line before the hunk rather than the
// first of it, so both land on the line after and the runs either side of the
// hunk run straight through it.
func bounds(start, lines int) (int, int) {
	if lines == 0 {
		return start + 1, start + 1
	}
	return start, start + lines
}
