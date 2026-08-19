package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// render writes the changeset with the review on it: a row per file, and under
// each one a row per hunk named the way --hunk names it.
func (v changesetView) render() string {
	var b strings.Builder
	v.write(&b)

	switch {
	case !v.Exists:
	case len(v.Changeset.Files) == 0:
		v.empty(&b)
	default:
		b.WriteString("\n")
		writeChangeset(&b, v.Changeset)
		fmt.Fprintf(&b, "\n%s, %d of %d reviewed\n",
			plural(len(v.Changeset.Files), "file"), v.Changeset.Reviewed, v.Changeset.Items)
	}

	writeSkipped(&b, v.Skipped)
	return b.String()
}

// writeChangeset lays each file out with its hunks indented under it.
//
// The hunk rows are their own column set. Sharing widths with the file rows
// would pad a side and a line number out to the width of a path.
func writeChangeset(b *strings.Builder, c review.Changeset) {
	rows := make([][]string, 0, len(c.Files))
	for _, f := range c.Files {
		rows = append(rows, fileRow(f))
	}

	widths := columnWidths(rows)
	for i, f := range c.Files {
		writeRow(b, "", widths, rows[i])
		writeColumns(b, "     ", hunkRows(f))
	}
}

// fileRow is the summary line: what git did to the file, how much of it has been
// read, and the churn. A file the last refresh took reviewed lines off says so
// last, where the reader is already looking for the reason the count moved.
func fileRow(f review.File) []string {
	cells := []string{
		letter(f.Diff.Status),
		name(f.Diff),
		string(f.State),
		fmt.Sprintf("%d of %d", f.Reviewed, f.Items),
		read(f.Diff),
	}
	if f.Changed {
		cells = append(cells, "changed after review")
	}
	return cells
}

// hunkRows name each hunk the way --hunk and --side do, so a reader marks what
// they are looking at by typing back what they see.
func hunkRows(f review.File) [][]string {
	rows := make([][]string, 0, len(f.Hunks))
	for _, h := range f.Hunks {
		side, line := h.Name()
		rows = append(rows, []string{string(side), fmt.Sprint(line), string(h.State)})
	}
	return rows
}

// read is the churn, or the reason there is none to count.
func read(f diff.File) string {
	if f.Omitted != "" {
		return f.Omitted
	}
	return churn(f)
}

// statePayload is the wire shape of a changeset with its review, and a contract
// with whatever is parsing it. It shares the session keys with the status
// payload and nothing else: a status counts a file's hunks where this one names
// them.
type statePayload struct {
	headerJSON

	Files  []stateFileJSON `json:"files"`
	Totals stateTotalsJSON `json:"totals"`
}

type stateFileJSON struct {
	Path    string      `json:"path"`
	OldPath string      `json:"oldPath,omitempty"`
	Status  diff.Status `json:"status"`
	Omitted string      `json:"omitted,omitempty"`

	State review.State `json:"state"`

	// Changed is the last refresh reporting it took reviewed lines off this file,
	// and it cannot be read off the counts beside it. A range the translation cut
	// and a range somebody unmarked leave the same coverage behind.
	Changed bool `json:"changed"`

	// Reviewed of Items is this file's share of the burn-down. An item is one
	// hunk, or the whole file when it has none.
	Reviewed int `json:"reviewed"`
	Items    int `json:"items"`

	Additions int `json:"additions"`
	Deletions int `json:"deletions"`

	Hunks []stateHunkJSON `json:"hunks"`
}

// stateHunkJSON names a hunk the way the write commands take it. Side and Line
// are what --side and --hunk want, so a hunk round-trips out of this output.
type stateHunkJSON struct {
	Side  store.Side   `json:"side"`
	Line  int          `json:"line"`
	State review.State `json:"state"`

	// Anchors are the lines the hunk holds on each side it touches, which is what
	// --lines aims inside one.
	Anchors []anchorJSON `json:"anchors"`
}

type anchorJSON struct {
	Side  store.Side `json:"side"`
	Start int        `json:"start"`
	End   int        `json:"end"`
}

type stateTotalsJSON struct {
	Files     int `json:"files"`
	Reviewed  int `json:"reviewed"`
	Items     int `json:"items"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// statePayloadOf projects the changeset onto the wire. Every slice is made
// rather than declared, so nothing marshals to null and no caller handles two
// spellings of empty.
func statePayloadOf(v changesetView) statePayload {
	c := v.Changeset
	p := statePayload{
		headerJSON: headerOf(v.header),
		Files:      make([]stateFileJSON, 0, len(c.Files)),
		Totals: stateTotalsJSON{
			Files:     len(c.Files),
			Reviewed:  c.Reviewed,
			Items:     c.Items,
			Additions: c.Additions,
			Deletions: c.Deletions,
		},
	}

	for _, f := range c.Files {
		file := stateFileJSON{
			Path:      f.Diff.Path,
			OldPath:   f.Diff.OldPath,
			Status:    f.Diff.Status,
			Omitted:   f.Diff.Omitted,
			State:     f.State,
			Changed:   f.Changed,
			Reviewed:  f.Reviewed,
			Items:     f.Items,
			Additions: f.Diff.Additions,
			Deletions: f.Diff.Deletions,
			Hunks:     make([]stateHunkJSON, 0, len(f.Hunks)),
		}

		for _, h := range f.Hunks {
			side, line := h.Name()
			hunk := stateHunkJSON{
				Side:    side,
				Line:    line,
				State:   h.State,
				Anchors: make([]anchorJSON, 0, len(h.Anchors)),
			}
			for _, a := range h.Anchors {
				hunk.Anchors = append(hunk.Anchors, anchorJSON{
					Side: a.Side, Start: a.Range.Start, End: a.Range.End,
				})
			}
			file.Hunks = append(file.Hunks, hunk)
		}
		p.Files = append(p.Files, file)
	}
	return p
}

func (v changesetView) encode(out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(statePayloadOf(v)); err != nil {
		return fmt.Errorf("writing the changeset as JSON: %w", err)
	}
	return nil
}
