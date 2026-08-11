package diff

import (
	"regexp"
	"strconv"
	"strings"
)

// hunkHeader is the @@ line. The counts are optional: git writes a one-line range
// as "@@ -1 +1 @@" rather than spelling out the 1.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Parse reads git's unified diff output.
//
// It cannot fail. A file it cannot read arrives with Omitted set rather than as an
// error or an absence, because silently dropping a changed file is the failure
// this whole tool exists to prevent.
//
// Only a "diff --git" or "diff --cc" line starts a new file. Every other header
// form is read before the first @@ of a file and treated as body text after it: a
// removed line whose own text begins with "--" arrives as "--- x", and would
// otherwise be read as a path.
func Parse(patch []byte) []File {
	p := &parser{}

	// The trailing newline would otherwise split into an empty final line and land
	// in the last hunk as a blank one the file does not have.
	for _, line := range strings.Split(strings.TrimSuffix(string(patch), "\n"), "\n") {
		p.line(line)
	}
	p.flush()
	return p.files
}

type parser struct {
	files []File

	// file is the one being read, nil before the first "diff --git".
	file *File

	// oldSide and newSide are the paths seen so far for the current file, empty
	// for the side it does not exist on. A rename names its paths outright; these
	// are what everything else falls back to.
	oldSide, newSide string

	// inBody says the first @@ has been read, after which no line is a header.
	inBody bool

	// combined says this file arrived as a combined diff, whose two-column body
	// this package does not read.
	combined bool

	// oldNo and newNo are the line numbers the next body line takes.
	oldNo, newNo int
}

func (p *parser) line(line string) {
	if rest, ok := strings.CutPrefix(line, "diff --git "); ok {
		p.begin()
		p.oldSide, p.newSide = splitPaths(rest)
		return
	}

	// A combined diff is what git writes for a path that is still unmerged. There
	// is no single old side to anchor a review to, so the file is listed with a
	// reason rather than parsed or dropped.
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
		// Anything before the first file header, and the body of a combined diff.
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

// flush finishes the current file: the parts git states only by omission are
// settled here, once every header line has been seen.
func (p *parser) flush() {
	f := p.file
	if f == nil {
		return
	}
	p.file = nil

	if f.Path == "" {
		// A rename or a copy has already named both paths. Everything else takes
		// the new side, or the old one when the file was deleted.
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

// omission says why a file arrived with no hunks.
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

// index reads the blob shas, and the mode git writes here only when it is the same
// on both sides. A created or deleted file states its mode on its own line and
// leaves this one without.
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
		// "\ No newline at end of file" annotates the line above it.
		if n := len(h.Lines); n > 0 {
			h.Lines[n-1].NoEOL = true
		}

	default:
		// A blank context line can arrive as the empty string rather than as a lone
		// space, so the marker comes off as a prefix rather than by index.
		h.Lines = append(h.Lines, Line{Kind: Context, Old: p.oldNo, New: p.newNo, Text: strings.TrimPrefix(line, " ")})
		p.oldNo++
		p.newNo++
	}
}

// splitPaths reads the two paths off a "diff --git" line. It is only needed for a
// file with no --- and +++ lines, which is a binary file or a mode change, and
// there both sides are the same path.
//
// The line is ambiguous in general, because a path may hold a space and git does
// not quote for one. Equal paths make the space fall at the midpoint, and a rename
// or a copy states its paths on their own lines, so every case that reaches here
// splits exactly.
func splitPaths(rest string) (string, string) {
	if strings.HasPrefix(rest, `"`) {
		// The scan steps over an escape and whatever it escapes. Looking only at the
		// byte before a quote cannot tell an escaped quote from the closing quote of
		// a path that ends in a backslash, which git writes as "a/back\\".
		for i := 1; i < len(rest); i++ {
			switch {
			case rest[i] == '\\':
				i++
			case rest[i] != '"':
			case i+2 < len(rest) && rest[i+1] == ' ' && rest[i+2] == '"':
				return strings.TrimPrefix(unquote(rest[:i+1]), "a/"), strings.TrimPrefix(unquote(rest[i+2:]), "b/")
			default:
				// The first side closed without a second following it.
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

// pathSide reads a path off a --- or +++ line. /dev/null is the side a file does
// not exist on, and comes back empty.
func pathSide(rest, prefix string) string {
	rest = unquote(rest)
	if rest == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(rest, prefix)
}

// unquote reads a C-quoted path. core.quotePath is pinned off, but git still wraps
// a path holding a quote, a backslash or a control character, and the escapes it
// writes are the ones a Go string literal uses.
func unquote(s string) string {
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	if out, err := strconv.Unquote(s); err == nil {
		return out
	}
	return s
}

// blob normalises git's null sha to an empty string.
func blob(sha string) string {
	if strings.Trim(sha, "0") == "" {
		return ""
	}
	return sha
}

// lineCount is a hunk's line count on one side. An absent one means one line.
func lineCount(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}

// atoi is for digits a regular expression already matched.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// has assigns the remainder of a header line to field, reporting whether the
// prefix matched, so the switch above reads as the list of header forms it is.
func has(line, prefix string, field *string) bool {
	rest, ok := strings.CutPrefix(line, prefix)
	if ok {
		*field = rest
	}
	return ok
}

// hasPath is has for a field holding a path, which git may have quoted.
func hasPath(line, prefix string, field *string) bool {
	rest, ok := strings.CutPrefix(line, prefix)
	if ok {
		*field = unquote(rest)
	}
	return ok
}
