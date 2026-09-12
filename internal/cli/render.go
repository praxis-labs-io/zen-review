package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
)

const label = "%-10s  %s\n"

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

func (v header) empty(b *strings.Builder) {
	if v.reason() == fresh {
		fmt.Fprintf(b, "\nno changes since %s\n", v.Base.Name())
		return
	}
	fmt.Fprintf(b, "\ngeneration %d held no changes since %s\n", v.Generation.Seq, v.Base.Name())
}

func (v header) write(b *strings.Builder) {
	fmt.Fprintf(b, label, "base", baseCell(v.Base))
	if v.Exists {
		fmt.Fprintf(b, label, "generation", fmt.Sprint(v.Generation.Seq))
	}
	fmt.Fprintf(b, label, "session", v.Ref)

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

func baseCell(b review.Base) string {
	cell := fmt.Sprintf("%s (%s)", b.Name(), short(b.SHA))
	if b.Fallback == "" {
		return cell
	}
	return cell + "  ·  " + b.Fallback
}

func writeFiles(b *strings.Builder, files []diff.File) {
	rows := make([][]string, 0, len(files))
	for _, f := range files {
		cells := []string{letter(f.Status), name(f), extent(f)}
		if c := churn(f); c != "" {
			cells = append(cells, c)
		}
		rows = append(rows, cells)
	}
	writeColumns(b, "", rows)
}

// Padded by hand rather than through text/tabwriter, so it cannot fail and never pads a row's last cell.
func writeColumns(b *strings.Builder, indent string, rows [][]string) {
	widths := columnWidths(rows)
	for _, cells := range rows {
		writeRow(b, indent, widths, cells)
	}
}

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

type headerJSON struct {
	Session string `json:"session"`
	Ref     string `json:"ref"`
	Kind    string `json:"kind"`
	Branch  string `json:"branch,omitempty"`

	Base baseJSON `json:"base"`

	Generation *generationJSON `json:"generation"`

	Stale bool `json:"stale"`

	// No omitempty here or on Stale: false and absent are different answers.
	StaleReason staleness `json:"staleReason"`

	Skipped []string `json:"skipped"`
}

type payload struct {
	headerJSON

	Files      []fileJSON      `json:"files"`
	Totals     totalsJSON      `json:"totals"`
	Candidates *candidatesJSON `json:"candidates,omitempty"`
}

type candidatesJSON struct {
	Local  []candidateJSON `json:"local"`
	Remote []candidateJSON `json:"remote"`
}

type candidateJSON struct {
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
	Ahead int    `json:"ahead"`
}

type baseJSON struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`

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
	if v.Candidates != nil {
		p.Candidates = &candidatesJSON{
			Local:  candidateJSONs(v.Candidates.Local),
			Remote: candidateJSONs(v.Candidates.Remote),
		}
	}

	return p
}

func candidateJSONs(candidates []review.Candidate) []candidateJSON {
	out := make([]candidateJSON, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidateJSON{
			Ref: candidate.Branch, SHA: candidate.SHA, Ahead: candidate.Ahead,
		})
	}
	return out
}

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
