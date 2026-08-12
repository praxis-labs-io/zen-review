package review_test

import (
	"fmt"
	"testing"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// hunk builds one from the markers of a unified diff, read left to right off the
// two start lines: a space is context, + is added, - is removed.
func hunk(oldStart, newStart int, spec string) diff.Hunk {
	h := diff.Hunk{OldStart: oldStart, NewStart: newStart}
	old, next := oldStart, newStart

	for _, c := range spec {
		switch c {
		case ' ':
			h.Lines = append(h.Lines, diff.Line{Kind: diff.Context, Old: old, New: next})
			old, next = old+1, next+1
			h.OldLines, h.NewLines = h.OldLines+1, h.NewLines+1
		case '+':
			h.Lines = append(h.Lines, diff.Line{Kind: diff.Added, New: next})
			next++
			h.NewLines++
		case '-':
			h.Lines = append(h.Lines, diff.Line{Kind: diff.Removed, Old: old})
			old++
			h.OldLines++
		}
	}
	return h
}

func row(path string, side store.Side, start, end int) store.ReviewedRange {
	return store.ReviewedRange{Path: path, Side: side, LineRange: store.LineRange{Start: start, End: end}}
}

// describe is what the table below reads: a line per file, a line per hunk under
// it, and the burn-down last.
func describe(c review.Changeset) []string {
	out := make([]string, 0, len(c.Files)+1)
	for _, f := range c.Files {
		out = append(out, fmt.Sprintf("%s %s %d/%d", f.Diff.Path, f.State, f.Reviewed, len(f.Hunks)))
		for _, h := range f.Hunks {
			out = append(out, fmt.Sprintf("  %s %d:%d %s", h.Side, h.Range.Start, h.Range.End, h.State))
		}
	}
	return append(out, fmt.Sprintf("%d of %d hunks", c.Reviewed, c.Hunks))
}

// twoLines is the common shape: one modified file whose single hunk introduces
// lines 11 and 12.
func twoLines() []diff.File {
	return []diff.File{{
		Path:   "a.go",
		Status: diff.FileModified,
		Hunks:  []diff.Hunk{hunk(10, 10, " ++ ")},
	}}
}

func TestDeriveReadsStateOutOfTheRanges(t *testing.T) {
	tests := []struct {
		name   string
		files  []diff.File
		now    []store.ReviewedRange
		before []store.ReviewedRange
		want   []string
	}{
		{
			name:  "every anchored line covered",
			files: twoLines(),
			now:   []store.ReviewedRange{row("a.go", store.SideHead, 11, 12)},
			want:  []string{"a.go reviewed 1/1", "  head 11:12 reviewed", "1 of 1 hunks"},
		},
		{
			// The only way to get here is a translation cutting the range, because
			// a mark covers a hunk whole.
			name:  "one of the two covered",
			files: twoLines(),
			now:   []store.ReviewedRange{row("a.go", store.SideHead, 11, 11)},
			want:  []string{"a.go changed 0/1", "  head 11:12 changed", "0 of 1 hunks"},
		},
		{
			name:  "nothing covered",
			files: twoLines(),
			want:  []string{"a.go unreviewed 0/1", "  head 11:12 unreviewed", "0 of 1 hunks"},
		},
		{
			name: "an added file is named by its first line",
			files: []diff.File{{
				Path:   "new.go",
				Status: diff.FileAdded,
				Hunks:  []diff.Hunk{hunk(0, 1, "+++")},
			}},
			now:  []store.ReviewedRange{row("new.go", store.SideHead, 1, 3)},
			want: []string{"new.go reviewed 1/1", "  head 1:3 reviewed", "1 of 1 hunks"},
		},
		{
			name: "a deleted file anchors base side",
			files: []diff.File{{
				Path:   "gone.go",
				Status: diff.FileDeleted,
				Hunks:  []diff.Hunk{hunk(1, 0, "---")},
			}},
			now:  []store.ReviewedRange{row("gone.go", store.SideBase, 1, 3)},
			want: []string{"gone.go reviewed 1/1", "  base 1:3 reviewed", "1 of 1 hunks"},
		},
		{
			// The anchor takes in the context line between the two additions. Cover
			// only the additions and a line inside the hunk is unread, which is what
			// this has to say.
			name: "the anchor spans context between two additions",
			files: []diff.File{{
				Path:   "a.go",
				Status: diff.FileModified,
				Hunks:  []diff.Hunk{hunk(20, 20, "+ +")},
			}},
			now: []store.ReviewedRange{
				row("a.go", store.SideHead, 20, 20),
				row("a.go", store.SideHead, 22, 22),
			},
			want: []string{"a.go changed 0/1", "  head 20:22 changed", "0 of 1 hunks"},
		},
		{
			name: "a deletion-only hunk beside a head-side one",
			files: []diff.File{{
				Path:   "mixed.go",
				Status: diff.FileModified,
				Hunks:  []diff.Hunk{hunk(5, 5, "---"), hunk(30, 27, " + ")},
			}},
			now: []store.ReviewedRange{row("mixed.go", store.SideBase, 5, 7)},
			want: []string{
				"mixed.go unreviewed 1/2",
				"  base 5:7 reviewed",
				"  head 28:28 unreviewed",
				"1 of 2 hunks",
			},
		},
		{
			// The base blob of a file the branch moved sits at the old name, and so
			// does the range anchored to it.
			name: "a base-side hunk of a renamed file is keyed by the base name",
			files: []diff.File{{
				Path:    "b.go",
				OldPath: "a.go",
				Status:  diff.FileRenamed,
				Hunks:   []diff.Hunk{hunk(5, 5, "---")},
			}},
			now:  []store.ReviewedRange{row("a.go", store.SideBase, 5, 7)},
			want: []string{"b.go reviewed 1/1", "  base 5:7 reviewed", "1 of 1 hunks"},
		},
		{
			name: "a file with no hunks and a whole-file mark",
			files: []diff.File{{
				Path:    "logo.png",
				Status:  diff.FileModified,
				Binary:  true,
				Omitted: "binary",
			}},
			now:  []store.ReviewedRange{row("logo.png", store.SideHead, 0, 0)},
			want: []string{"logo.png reviewed 0/0", "0 of 0 hunks"},
		},
		{
			name: "a file with no hunks and no mark",
			files: []diff.File{{
				Path:    "logo.png",
				Status:  diff.FileModified,
				Binary:  true,
				Omitted: "binary",
			}},
			want: []string{"logo.png unreviewed 0/0", "0 of 0 hunks"},
		},
		{
			name:  "a whole-file mark answers for the hunks too",
			files: twoLines(),
			now:   []store.ReviewedRange{row("a.go", store.SideHead, 0, 0)},
			want:  []string{"a.go reviewed 1/1", "  head 11:12 reviewed", "1 of 1 hunks"},
		},
		{
			// The hunk was rewritten end to end, so its range went whole and there
			// is nothing partial left to read it off.
			name:   "coverage lost with nothing partial left",
			files:  twoLines(),
			before: []store.ReviewedRange{row("a.go", store.SideHead, 11, 12)},
			want:   []string{"a.go changed 0/1", "  head 11:12 unreviewed", "0 of 1 hunks"},
		},
		{
			name:   "coverage lost and then read again",
			files:  twoLines(),
			now:    []store.ReviewedRange{row("a.go", store.SideHead, 11, 12)},
			before: []store.ReviewedRange{row("a.go", store.SideHead, 11, 12), row("a.go", store.SideHead, 20, 25)},
			want:   []string{"a.go reviewed 1/1", "  head 11:12 reviewed", "1 of 1 hunks"},
		},
		{
			name: "a rename does not hide the drop",
			files: []diff.File{{
				Path:    "b.go",
				OldPath: "a.go",
				Status:  diff.FileRenamed,
				Hunks:   []diff.Hunk{hunk(10, 10, " ++ ")},
			}},
			before: []store.ReviewedRange{row("a.go", store.SideHead, 11, 12)},
			want:   []string{"b.go changed 0/1", "  head 11:12 unreviewed", "0 of 1 hunks"},
		},
		{
			// The file's own name is there, so the old one is not consulted and the
			// coverage it holds does not read as a drop.
			name: "a renamed file already carrying its new name",
			files: []diff.File{{
				Path:    "b.go",
				OldPath: "a.go",
				Status:  diff.FileRenamed,
				Hunks:   []diff.Hunk{hunk(10, 10, " ++ "), hunk(40, 40, " + ")},
			}},
			now: []store.ReviewedRange{row("b.go", store.SideHead, 11, 12)},
			before: []store.ReviewedRange{
				row("a.go", store.SideHead, 11, 12),
				row("a.go", store.SideHead, 20, 25),
				row("b.go", store.SideHead, 11, 12),
			},
			want: []string{
				"b.go unreviewed 1/2",
				"  head 11:12 reviewed",
				"  head 41:41 unreviewed",
				"1 of 2 hunks",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRanges(t, describe(review.Derive(tt.files, tt.now, tt.before)), tt.want)
		})
	}
}

func TestAHunkIsFoundByThePathSideAndLineItIsNamedUnder(t *testing.T) {
	c := review.Derive([]diff.File{{
		Path:   "mixed.go",
		Status: diff.FileModified,
		Hunks:  []diff.Hunk{hunk(5, 5, "---"), hunk(30, 27, " + ")},
	}}, nil, nil)

	tests := []struct {
		name  string
		path  string
		side  store.Side
		line  int
		found bool
	}{
		{name: "the head-side hunk", path: "mixed.go", side: store.SideHead, line: 28, found: true},
		{name: "the base-side hunk", path: "mixed.go", side: store.SideBase, line: 5, found: true},
		{name: "the right line on the wrong side", path: "mixed.go", side: store.SideHead, line: 5},
		{name: "a line inside a hunk rather than its first", path: "mixed.go", side: store.SideBase, line: 6},
		{name: "a path the changeset does not hold", path: "other.go", side: store.SideHead, line: 28},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, found := c.Hunk(tt.path, tt.side, tt.line)
			if found != tt.found {
				t.Fatalf("found = %v, want %v", found, tt.found)
			}
			if found && h.Range.Start != tt.line {
				t.Errorf("hunk starts at %d, want %d", h.Range.Start, tt.line)
			}
		})
	}
}
