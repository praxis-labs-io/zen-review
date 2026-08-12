package review_test

import (
	"fmt"
	"strings"
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

// hunkLines is one line per hunk, naming every side it anchors on.
func hunkLines(f review.File) []string {
	out := make([]string, 0, len(f.Hunks))
	for _, h := range f.Hunks {
		at := make([]string, 0, len(h.Anchors))
		for _, a := range h.Anchors {
			at = append(at, fmt.Sprintf("%s %d:%d", a.Side, a.Range.Start, a.Range.End))
		}
		out = append(out, "  "+strings.Join(at, " ")+" "+string(h.State))
	}
	return out
}

// describe is what the table below reads: a line per file, a line per hunk under
// it, and the burn-down last.
func describe(c review.Changeset) []string {
	out := make([]string, 0, len(c.Files)+1)
	for _, f := range c.Files {
		out = append(out, fmt.Sprintf("%s %s %d/%d", f.Diff.Path, f.State, f.Reviewed, f.Items))
		out = append(out, hunkLines(f)...)
	}
	return append(out, fmt.Sprintf("%d of %d", c.Reviewed, c.Items))
}

// twoLines is the common shape: one modified file whose single hunk introduces
// lines 11 and 12 and removes nothing.
func twoLines() []diff.File {
	return []diff.File{{
		Path:   "a.go",
		Status: diff.FileModified,
		Hunks:  []diff.Hunk{hunk(10, 10, " ++ ")},
	}}
}

// binary is the shape with nothing to read line by line, markable only as a
// whole.
func binary() []diff.File {
	return []diff.File{{Path: "logo.png", Status: diff.FileModified, Binary: true, Omitted: "binary"}}
}

func TestDeriveReadsStateOutOfTheRanges(t *testing.T) {
	tests := []struct {
		name  string
		files []diff.File
		rows  []store.ReviewedRange
		want  []string
	}{
		{
			name:  "every anchored line covered",
			files: twoLines(),
			rows:  []store.ReviewedRange{row("a.go", store.SideHead, 11, 12)},
			want:  []string{"a.go reviewed 1/1", "  head 11:12 reviewed", "1 of 1"},
		},
		{
			name:  "one of the two covered",
			files: twoLines(),
			rows:  []store.ReviewedRange{row("a.go", store.SideHead, 11, 11)},
			want:  []string{"a.go partial 0/1", "  head 11:12 partial", "0 of 1"},
		},
		{
			name:  "nothing covered",
			files: twoLines(),
			want:  []string{"a.go unreviewed 0/1", "  head 11:12 unreviewed", "0 of 1"},
		},
		{
			// The lines a hunk removes are not lines it has. Anchor it on its
			// additions alone and a deletion arriving later hides inside a hunk that
			// still reads reviewed.
			name: "a hunk that both adds and removes anchors on both sides",
			files: []diff.File{{
				Path:   "a.go",
				Status: diff.FileModified,
				Hunks:  []diff.Hunk{hunk(10, 10, " -+ ")},
			}},
			rows: []store.ReviewedRange{row("a.go", store.SideHead, 11, 11)},
			want: []string{"a.go partial 0/1", "  head 11:11 base 11:11 partial", "0 of 1"},
		},
		{
			name: "the same hunk with both sides covered",
			files: []diff.File{{
				Path:   "a.go",
				Status: diff.FileModified,
				Hunks:  []diff.Hunk{hunk(10, 10, " -+ ")},
			}},
			rows: []store.ReviewedRange{
				row("a.go", store.SideBase, 11, 11),
				row("a.go", store.SideHead, 11, 11),
			},
			want: []string{"a.go reviewed 1/1", "  head 11:11 base 11:11 reviewed", "1 of 1"},
		},
		{
			name: "an added file is named by its first line",
			files: []diff.File{{
				Path:   "new.go",
				Status: diff.FileAdded,
				Hunks:  []diff.Hunk{hunk(0, 1, "+++")},
			}},
			rows: []store.ReviewedRange{row("new.go", store.SideHead, 1, 3)},
			want: []string{"new.go reviewed 1/1", "  head 1:3 reviewed", "1 of 1"},
		},
		{
			name: "a deleted file anchors base side only",
			files: []diff.File{{
				Path:   "gone.go",
				Status: diff.FileDeleted,
				Hunks:  []diff.Hunk{hunk(1, 0, "---")},
			}},
			rows: []store.ReviewedRange{row("gone.go", store.SideBase, 1, 3)},
			want: []string{"gone.go reviewed 1/1", "  base 1:3 reviewed", "1 of 1"},
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
			rows: []store.ReviewedRange{
				row("a.go", store.SideHead, 20, 20),
				row("a.go", store.SideHead, 22, 22),
			},
			want: []string{"a.go partial 0/1", "  head 20:22 partial", "0 of 1"},
		},
		{
			name: "a deletion-only hunk beside a head-side one",
			files: []diff.File{{
				Path:   "mixed.go",
				Status: diff.FileModified,
				Hunks:  []diff.Hunk{hunk(5, 5, "---"), hunk(30, 27, " + ")},
			}},
			rows: []store.ReviewedRange{row("mixed.go", store.SideBase, 5, 7)},
			want: []string{
				"mixed.go partial 1/2",
				"  base 5:7 reviewed",
				"  head 28:28 unreviewed",
				"1 of 2",
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
			rows: []store.ReviewedRange{row("a.go", store.SideBase, 5, 7)},
			want: []string{"b.go reviewed 1/1", "  base 5:7 reviewed", "1 of 1"},
		},
		{
			name:  "a file with no hunks and a whole-file mark",
			files: binary(),
			rows:  []store.ReviewedRange{row("logo.png", store.SideHead, 0, 0)},
			want:  []string{"logo.png reviewed 1/1", "1 of 1"},
		},
		{
			// It counts as one item rather than none, or a changeset carrying it
			// reads complete while it sits unopened.
			name:  "a file with no hunks and no mark",
			files: binary(),
			want:  []string{"logo.png unreviewed 0/1", "0 of 1"},
		},
		{
			// carry.go drops a whole-file mark the moment its file has hunks, so
			// honouring one here would report a review the next refresh deletes.
			name:  "a whole-file mark on a file that has hunks answers for nothing",
			files: twoLines(),
			rows:  []store.ReviewedRange{row("a.go", store.SideHead, 0, 0)},
			want:  []string{"a.go unreviewed 0/1", "  head 11:12 unreviewed", "0 of 1"},
		},
		{
			name:  "a binary file beside a hunk nobody read",
			files: append(twoLines(), binary()...),
			rows:  []store.ReviewedRange{row("logo.png", store.SideHead, 0, 0)},
			want: []string{
				"a.go unreviewed 0/1",
				"  head 11:12 unreviewed",
				"logo.png reviewed 1/1",
				"1 of 2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRanges(t, describe(review.Derive(tt.files, tt.rows)), tt.want)
		})
	}
}

func TestAHunkIsFoundByThePathSideAndLineItIsNamedUnder(t *testing.T) {
	c := review.Derive([]diff.File{{
		Path:   "mixed.go",
		Status: diff.FileModified,
		Hunks:  []diff.Hunk{hunk(5, 5, "---"), hunk(30, 27, " -+ ")},
	}}, nil)

	tests := []struct {
		name  string
		path  string
		side  store.Side
		line  int
		found bool
	}{
		{name: "the deletion-only hunk, named base side", path: "mixed.go", side: store.SideBase, line: 5, found: true},
		{name: "the two-sided hunk, named by its head anchor", path: "mixed.go", side: store.SideHead, line: 28, found: true},
		{name: "the two-sided hunk by its base anchor", path: "mixed.go", side: store.SideBase, line: 31},
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
			if !found {
				return
			}
			if side, line := h.Name(); side != tt.side || line != tt.line {
				t.Errorf("hunk is named %s %d, want %s %d", side, line, tt.side, tt.line)
			}
		})
	}
}
