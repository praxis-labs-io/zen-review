package diff

import (
	"regexp"
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Parse reads git's unified diff output. It never fails: an unreadable file gets Omitted set.
func Parse(patch []byte) []File {
	p := &parser{}

	for _, line := range strings.Split(strings.TrimSuffix(string(patch), "\n"), "\n") {
		p.line(line)
	}
	p.flush()
	return p.files
}

type parser struct {
	files []File

	file *File

	oldSide, newSide string

	inBody bool

	combined bool

	oldNo, newNo int
}

func (p *parser) line(line string) {
	if rest, ok := strings.CutPrefix(line, "diff --git "); ok {
		p.begin()
		p.oldSide, p.newSide = splitPaths(rest)
		return
	}

	for _, prefix := range []string{"diff --cc ", "diff --combined "} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			p.begin()
			p.combined = true
			p.newSide = unquote(rest)
			p.file.Omitted = "conflicted, not yet merged"
			return
		}
	}

	switch {
	case p.file == nil, p.combined:
	case p.inBody:
		p.body(line)
	default:
		p.header(line)
	}
}

func (p *parser) begin() {
	p.flush()
	p.file = &File{Status: FileModified}
	p.oldSide, p.newSide = "", ""
	p.inBody, p.combined = false, false
}

func (p *parser) flush() {
	f := p.file
	if f == nil {
		return
	}
	p.file = nil

	if f.Path == "" {
		f.Path = p.newSide
		if f.Path == "" {
			f.Path = p.oldSide
		}
	}
	if len(f.Hunks) == 0 && f.Omitted == "" {
		f.Omitted = omission(f)
	}
	p.files = append(p.files, *f)
}

func omission(f *File) string {
	switch {
	case f.OldMode != "" && f.NewMode != "" && f.OldMode != f.NewMode:
		return "mode change only"
	case f.Status == FileRenamed:
		return "renamed, contents unchanged"
	case f.Status == FileCopied:
		return "copied, contents unchanged"
	case f.Status == FileAdded:
		return "the new file is empty"
	case f.Status == FileDeleted:
		return "the removed file was empty"
	default:
		return "no content change"
	}
}

func (p *parser) header(line string) {
	f := p.file

	switch {
	case has(line, "old mode ", &f.OldMode):
	case has(line, "new mode ", &f.NewMode):
	case has(line, "new file mode ", &f.NewMode):
		f.Status = FileAdded
	case has(line, "deleted file mode ", &f.OldMode):
		f.Status = FileDeleted

	case hasPath(line, "rename from ", &f.OldPath):
		f.Status = FileRenamed
	case hasPath(line, "rename to ", &f.Path):
		f.Status = FileRenamed
	case hasPath(line, "copy from ", &f.OldPath):
		f.Status = FileCopied
	case hasPath(line, "copy to ", &f.Path):
		f.Status = FileCopied

	case strings.HasPrefix(line, "index "):
		p.index(strings.TrimPrefix(line, "index "))
	case strings.HasPrefix(line, "--- "):
		p.oldSide = pathSide(strings.TrimPrefix(line, "--- "), "a/")
	case strings.HasPrefix(line, "+++ "):
		p.newSide = pathSide(strings.TrimPrefix(line, "+++ "), "b/")

	case strings.HasPrefix(line, "Binary files "):
		f.Binary = true
		f.Omitted = "binary"

	case hunkHeader.MatchString(line):
		p.inBody = true
		p.hunk(line)
	}
}

func (p *parser) index(rest string) {
	shas, mode, _ := strings.Cut(rest, " ")
	old, next, ok := strings.Cut(shas, "..")
	if !ok {
		return
	}

	f := p.file
	f.OldBlob, f.NewBlob = blob(old), blob(next)
	if mode != "" {
		if f.OldMode == "" {
			f.OldMode = mode
		}
		if f.NewMode == "" {
			f.NewMode = mode
		}
	}
}

func (p *parser) hunk(line string) {
	m := hunkHeader.FindStringSubmatch(line)
	h := Hunk{
		Header:   line,
		OldStart: atoi(m[1]),
		OldLines: lineCount(m[2]),
		NewStart: atoi(m[3]),
		NewLines: lineCount(m[4]),
	}

	p.oldNo, p.newNo = h.OldStart, h.NewStart
	p.file.Hunks = append(p.file.Hunks, h)
}

func (p *parser) body(line string) {
	if hunkHeader.MatchString(line) {
		p.hunk(line)
		return
	}

	f := p.file
	h := &f.Hunks[len(f.Hunks)-1]

	switch {
	case strings.HasPrefix(line, "+"):
		h.Lines = append(h.Lines, Line{Kind: Added, New: p.newNo, Text: line[1:]})
		p.newNo++
		f.Additions++

	case strings.HasPrefix(line, "-"):
		h.Lines = append(h.Lines, Line{Kind: Removed, Old: p.oldNo, Text: line[1:]})
		p.oldNo++
		f.Deletions++

	case strings.HasPrefix(line, `\`):
		if n := len(h.Lines); n > 0 {
			h.Lines[n-1].NoEOL = true
		}

	default:
		h.Lines = append(h.Lines, Line{Kind: Context, Old: p.oldNo, New: p.newNo, Text: strings.TrimPrefix(line, " ")})
		p.oldNo++
		p.newNo++
	}
}

// splitPaths is only reached with equal paths, so an unquoted space splits at the midpoint.
func splitPaths(rest string) (string, string) {
	if strings.HasPrefix(rest, `"`) {
		for i := 1; i < len(rest); i++ {
			switch {
			case rest[i] == '\\':
				i++
			case rest[i] != '"':
			case i+2 < len(rest) && rest[i+1] == ' ' && rest[i+2] == '"':
				return strings.TrimPrefix(unquote(rest[:i+1]), "a/"), strings.TrimPrefix(unquote(rest[i+2:]), "b/")
			default:
				return "", ""
			}
		}
		return "", ""
	}

	mid := (len(rest) - 1) / 2
	if mid <= 0 || rest[mid] != ' ' {
		return "", ""
	}
	old, next := rest[:mid], rest[mid+1:]
	if !strings.HasPrefix(old, "a/") || !strings.HasPrefix(next, "b/") || old[2:] != next[2:] {
		return "", ""
	}
	return old[2:], next[2:]
}

func pathSide(rest, prefix string) string {
	rest = unquote(rest)
	if rest == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(rest, prefix)
}

// unquote exists because git C-quotes some paths even with core.quotePath off.
func unquote(s string) string {
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	if out, err := strconv.Unquote(s); err == nil {
		return out
	}
	return s
}

func blob(sha string) string {
	if strings.Trim(sha, "0") == "" {
		return ""
	}
	return sha
}

func lineCount(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func has(line, prefix string, field *string) bool {
	rest, ok := strings.CutPrefix(line, prefix)
	if ok {
		*field = rest
	}
	return ok
}

func hasPath(line, prefix string, field *string) bool {
	rest, ok := strings.CutPrefix(line, prefix)
	if ok {
		*field = unquote(rest)
	}
	return ok
}
