package diffpane

import (
	"strings"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
)

type body struct {
	side   store.Side
	lines  []string
	tokens [][]syntax.Token
}

// TogglePreview turns whole-file preview on or off, and false when the file has no text to fill in.
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

func (m *Model) settle() {
	m.remode()
	m.reveal()
	m.scrollToCursor()
}

// StopPreview turns preview off and caches nothing, for a body read that failed.
func (m *Model) StopPreview() {
	if m.preview == "" {
		return
	}
	m.preview = ""
	m.settle()
}

// OffHunk reports whether the cursor is on a filled-in file line, which is narrower than Hunk returning false.
func (m Model) OffHunk() bool {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return false
	}
	r := m.rows[m.cursor]
	return r.kind == codeRow && r.hunk < 0
}

func (m Model) previewing() bool {
	return m.preview != "" && m.body().lines != nil
}

func (m Model) body() body {
	if m.file == nil {
		return body{}
	}
	return m.bodies[m.file.Diff.Path]
}

func (m Model) hasText() bool {
	if m.file == nil || m.file.Diff.Binary {
		return false
	}
	b, read := m.bodies[m.file.Diff.Path]
	return !read || b.lines != nil
}

// NeedsBody returns the path whose body preview is waiting on. The caller reads it and hands it to SetBody.
func (m Model) NeedsBody() (string, bool) {
	if m.preview == "" || !m.hasText() {
		return "", false
	}
	if _, read := m.bodies[m.file.Diff.Path]; read {
		return "", false
	}
	return m.file.Diff.Path, true
}

// SetBody caches path's body and reports whether it has lines. An empty body for the file on screen turns preview off.
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

// tokenise sizes tokens to the file, not to Chroma's output, which drops a trailing blank line.
func (m *Model) tokenise(path string, b review.Body) body {
	if len(b.Lines) == 0 {
		return body{side: b.Side}
	}

	safe := make([]string, len(b.Lines))
	for i, line := range b.Lines {
		safe[i] = comp.Code(line)
	}

	tokens := make([][]syntax.Token, len(safe))
	copy(tokens, m.syntax.Lines(path, strings.Join(safe, "\n")))

	return body{side: b.Side, lines: safe, tokens: tokens}
}

type fill struct {
	from, to int
	delta    int
}

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

func (f fill) tokens(b body) [][]syntax.Token {
	from, to := min(max(0, f.from-1), len(b.tokens)), min(f.to, len(b.tokens))
	if from >= to {
		return nil
	}
	return b.tokens[from:to]
}

// fills keeps empty runs so the slice indexes by hunk.
func (m Model) fills() []fill {
	b := m.body()
	out := make([]fill, 0, len(m.file.Hunks)+1)

	at, delta := 1, 0
	for _, h := range m.file.Hunks {
		first, past := bounds(h.Diff.NewStart, h.Diff.NewLines)
		baseFirst, basePast := bounds(h.Diff.OldStart, h.Diff.OldLines)

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

// bounds handles a zero count, which in a unified diff names the line before the hunk.
func bounds(start, lines int) (int, int) {
	if lines == 0 {
		return start + 1, start + 1
	}
	return start, start + lines
}
