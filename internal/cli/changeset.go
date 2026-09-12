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

func hunkRows(f review.File) [][]string {
	rows := make([][]string, 0, len(f.Hunks))
	for _, h := range f.Hunks {
		side, line := h.Name()
		rows = append(rows, []string{string(side), fmt.Sprint(line), string(h.State)})
	}
	return rows
}

func read(f diff.File) string {
	if f.Omitted != "" {
		return f.Omitted
	}
	return churn(f)
}

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

	Changed bool `json:"changed"`

	Reviewed int `json:"reviewed"`
	Items    int `json:"items"`

	Additions int `json:"additions"`
	Deletions int `json:"deletions"`

	Hunks []stateHunkJSON `json:"hunks"`
}

type stateHunkJSON struct {
	Side  store.Side   `json:"side"`
	Line  int          `json:"line"`
	State review.State `json:"state"`

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
