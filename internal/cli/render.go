package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zen-review/zen-review/internal/diff"
)

// label is the width of the widest heading, so the three of them line up
// without a tabwriter for three rows.
const label = "%-10s  %s\n"

// render writes the view as prose.
//
// It returns a string rather than writing, because a strings.Builder cannot
// fail and that makes this a pure function of the view. Every formatting test
// is then a table over view literals with no repository behind it.
func (v view) render() string {
	var b strings.Builder
	v.write(&b)

	switch {
	case !v.Exists:
	case len(v.Files) == 0:
		v.empty(&b)
	default:
		b.WriteString("\n")
		writeFiles(&b, v.Files)
		fmt.Fprintf(&b, "\n%s, %s\n", plural(len(v.Files), "file"), plural(hunks(v.Files), "hunk"))
	}

	writeSkipped(&b, v.Skipped)
	return b.String()
}

// empty is the generation holding nothing to review.
//
// It is scoped to the generation once that stops describing what is on disk. A
// generation built on a clean branch and then edited against would otherwise
// print "no changes since origin/main" directly under the line saying the work
// tree moved, and of the two the reader believes the one that sounds like an
// answer.
func (v header) empty(b *strings.Builder) {
	if v.reason() == fresh {
		fmt.Fprintf(b, "\nno changes since %s\n", v.Base.Name())
		return
	}
	fmt.Fprintf(b, "\ngeneration %d held no changes since %s\n", v.Generation.Seq, v.Base.Name())
}

// write is the three headings and the sentence about the generation, which every
// command opens with.
func (v header) write(b *strings.Builder) {
	fmt.Fprintf(b, label, "base", fmt.Sprintf("%s (%s)", v.Base.Name(), short(v.Base.SHA)))
	if v.Exists {
		fmt.Fprintf(b, label, "generation", fmt.Sprint(v.Generation.Seq))
	}
	fmt.Fprintf(b, label, "session", v.Ref)

	// The base is not the one a reader would expect, so it says which and why
	// before it says anything about the changeset measured from it.
	if v.Base.Fallback != "" {
		fmt.Fprintf(b, "%s\n", v.Base.Fallback)
	}

	switch {
	case !v.Exists:
		b.WriteString("no generation yet, so run zen-review refresh\n")
	case v.reason() == staleBase:
		fmt.Fprintf(b, "the base moved to %s since generation %d was measured from %s\n",
			short(v.Base.SHA), v.Generation.Seq, short(v.Generation.BaseSha))
	case v.reason() == staleTree:
		fmt.Fprintf(b, "the work tree has moved since generation %d was built\n", v.Generation.Seq)
	}
}

// writeFiles lays the rows out in columns.
func writeFiles(b *strings.Builder, files []diff.File) {
	rows := make([][]string, 0, len(files))
	for _, f := range files {
		// The churn cell is left off rather than left empty, so a file with nothing
		// to count does not end its row with padding.
		cells := []string{letter(f.Status), name(f), extent(f)}
		if c := churn(f); c != "" {
			cells = append(cells, c)
		}
		rows = append(rows, cells)
	}
	writeColumns(b, "", rows)
}

// writeColumns pads a set of rows to a shared width and writes them under an
// indent.
//
// The padding is here rather than in text/tabwriter because that writes through
// an io.Writer that can fail, and this one provably cannot. Doing it by hand
// also makes the guarantee explicit rather than a side effect: the last cell of
// a row is never padded, so no line ends in whitespace.
//
// Widths count runes, which is what tabwriter counted, so a path outside ASCII
// lines up the same way it did.
//
// One call is one set of columns. The hunk rows under a file are their own call,
// because sharing widths with the file rows above would pad them out to a path.
func writeColumns(b *strings.Builder, indent string, rows [][]string) {
	widths := columnWidths(rows)
	for _, cells := range rows {
		writeRow(b, indent, widths, cells)
	}
}

// columnWidths is the widest cell in each column.
func columnWidths(rows [][]string) []int {
	var widths []int
	for _, cells := range rows {
		for i, cell := range cells {
			for len(widths) <= i {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	return widths
}

// writeRow writes one row padded to widths. The last cell is never padded, so no
// line ends in whitespace.
func writeRow(b *strings.Builder, indent string, widths []int, cells []string) {
	const gap = 2

	b.WriteString(indent)
	last := len(cells) - 1
	for i, cell := range cells {
		b.WriteString(cell)
		if i != last {
			b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+gap))
		}
	}
	b.WriteString("\n")
}

// writeSkipped names every path rather than counting them, and it goes last so
// it is the final word after the count it corrects.
//
// It says "just now" deliberately. On a status these paths come from the
// snapshot taken moments ago while the files above come from the stored
// generation, so this is not a property of the generation being reported.
//
// The stakes are higher than a missing row. The snapshot index is seeded from
// HEAD, so a tracked file git could not read keeps the blob that was already
// there: it does not vanish and it does not read as deleted, it reads as
// unchanged. An edit nobody can see is the failure this tool exists to prevent.
func writeSkipped(b *strings.Builder, skipped []string) {
	if len(skipped) == 0 {
		return
	}

	fmt.Fprintf(b, "\ngit could not read %s just now, so they are not in this review:\n",
		plural(len(skipped), "path"))
	for _, path := range skipped {
		fmt.Fprintf(b, "  %s\n", path)
	}
}

// headerJSON is what every payload opens with, embedded so the session keys are
// spelled once and every command promises the same ones.
type headerJSON struct {
	Session string `json:"session"`
	Ref     string `json:"ref"`
	Kind    string `json:"kind"`
	Branch  string `json:"branch,omitempty"`

	Base baseJSON `json:"base"`

	// Generation is null on a session that has never refreshed, which says more
	// than a zeroed object beside a flag. Its BaseSha sits beside Base.SHA
	// deliberately: when the two differ the files below were measured from this
	// one, and a consumer holding only the other cannot tell.
	Generation *generationJSON `json:"generation"`

	Stale bool `json:"stale"`

	// StaleReason is "", "tree" or "base". It is empty on a session with no
	// generation, where Stale is true and neither a base nor a tree moved to make
	// it so: a consumer switching on this reads the null generation for that case.
	//
	// No omitempty on it or on Stale, because false and absent are different
	// answers and a consumer should not have to guess which it got.
	StaleReason staleness `json:"staleReason"`

	// Skipped is top level rather than under Generation, because on a status it
	// describes the snapshot just taken and not the generation being reported.
	Skipped []string `json:"skipped"`
}

// payload is the wire shape, and a contract with whatever is parsing it rather
// than a mirror of the engine's structs.
//
// review.Status is declared for the engine: its Go names are wrong here, its
// Kind is an engine type, and its Files carry every hunk and every line where a
// status wants counts. Keeping the two apart also means a field landing on the
// engine through M5 does not silently join this output the day it is declared.
type payload struct {
	headerJSON

	Files  []fileJSON `json:"files"`
	Totals totalsJSON `json:"totals"`
}

type baseJSON struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`

	// Fallback is why this is not the base that was asked for, and is absent when
	// it is. Ref is empty beside it only for the empty tree, which has no ref.
	Fallback string `json:"fallback,omitempty"`
}

type generationJSON struct {
	Seq       int       `json:"seq"`
	Commit    string    `json:"commit"`
	BaseSha   string    `json:"baseSha"`
	HeadSha   string    `json:"headSha"`
	CreatedAt time.Time `json:"createdAt"`
}

type fileJSON struct {
	Path    string      `json:"path"`
	OldPath string      `json:"oldPath,omitempty"`
	Status  diff.Status `json:"status"`

	// Omitted says why a file has no hunks, and is empty when the counts are the
	// whole story.
	Omitted string `json:"omitted,omitempty"`

	Hunks     int `json:"hunks"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

type totalsJSON struct {
	Files     int `json:"files"`
	Hunks     int `json:"hunks"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// headerOf projects the session onto the wire.
//
// Skipped is made rather than declared. It is nil when nothing was skipped, and
// a nil slice marshals to null, which leaves a caller handling two spellings of
// empty.
func headerOf(v header) headerJSON {
	h := headerJSON{
		Session:     v.SessionID,
		Ref:         v.Ref,
		Kind:        string(v.Kind),
		Branch:      v.Branch,
		Base:        baseJSON{Ref: v.Base.Ref, SHA: v.Base.SHA, Fallback: v.Base.Fallback},
		Stale:       v.Stale,
		StaleReason: v.reason(),
		Skipped:     make([]string, 0, len(v.Skipped)),
	}
	h.Skipped = append(h.Skipped, v.Skipped...)

	if v.Exists {
		h.Generation = &generationJSON{
			Seq:       v.Generation.Seq,
			Commit:    v.Generation.CommitSha,
			BaseSha:   v.Generation.BaseSha,
			HeadSha:   v.Generation.HeadSha,
			CreatedAt: v.Generation.CreatedAt,
		}
	}
	return h
}

// payloadOf projects the view onto the wire. Files is made for the same reason
// Skipped is: it is nil on a session that never refreshed.
func payloadOf(v view) payload {
	p := payload{
		headerJSON: headerOf(v.header),
		Files:      make([]fileJSON, 0, len(v.Files)),
	}

	for _, f := range v.Files {
		p.Files = append(p.Files, fileJSON{
			Path:      f.Path,
			OldPath:   f.OldPath,
			Status:    f.Status,
			Omitted:   f.Omitted,
			Hunks:     len(f.Hunks),
			Additions: f.Additions,
			Deletions: f.Deletions,
		})
		p.Totals.Additions += f.Additions
		p.Totals.Deletions += f.Deletions
	}
	p.Totals.Files = len(v.Files)
	p.Totals.Hunks = hunks(v.Files)

	return p
}

// encode writes the view as JSON, indented to match the goldens elsewhere in
// the repo. The encoder writes its own trailing newline.
func (v view) encode(out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payloadOf(v)); err != nil {
		return fmt.Errorf("writing the status as JSON: %w", err)
	}
	return nil
}

func hunks(files []diff.File) int {
	n := 0
	for _, f := range files {
		n += len(f.Hunks)
	}
	return n
}

func letter(s diff.Status) string {
	switch s {
	case diff.FileAdded:
		return "A"
	case diff.FileDeleted:
		return "D"
	case diff.FileRenamed:
		return "R"
	case diff.FileCopied:
		return "C"
	default:
		return "M"
	}
}

// name shows a rename as both of its names, because the old one is how a reader
// knows which file this used to be.
func name(f diff.File) string {
	if f.OldPath == "" {
		return f.Path
	}
	return f.OldPath + " -> " + f.Path
}

func extent(f diff.File) string {
	if f.Omitted != "" {
		return f.Omitted
	}
	return plural(len(f.Hunks), "hunk")
}

func churn(f diff.File) string {
	if f.Omitted != "" {
		return ""
	}
	return fmt.Sprintf("+%d -%d", f.Additions, f.Deletions)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func short(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}
