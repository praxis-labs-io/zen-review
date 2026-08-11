package diff_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zen-review/zen-review/internal/diff"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	patch, err := os.ReadFile(filepath.Join("testdata", name+".diff"))
	if err != nil {
		t.Fatalf("reading the %s fixture: %v", name, err)
	}
	return patch
}

func TestAnEmptyPatchIsNoFiles(t *testing.T) {
	tests := []struct {
		name  string
		patch []byte
	}{
		{"nil", nil},
		{"empty", []byte("")},
		{"a newline", []byte("\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diff.Parse(tt.patch); len(got) != 0 {
				t.Errorf("got %d files, want none: %+v", len(got), got)
			}
		})
	}
}

// The changeset is one tracked diff with an untracked one appended, so the parser
// has to pick up where the first patch ended.
func TestFilesFromSeparateDiffsConcatenate(t *testing.T) {
	patch := append(fixture(t, "modify"), fixture(t, "untracked")...)

	got := diff.Parse(patch)

	if len(got) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(got), got)
	}
	if got[0].Path != "a.txt" || got[0].Status != diff.FileModified {
		t.Errorf("first file = %s %s, want a.txt modified", got[0].Path, got[0].Status)
	}
	if got[1].Path != "new.txt" || got[1].Status != diff.FileAdded {
		t.Errorf("second file = %s %s, want new.txt added", got[1].Path, got[1].Status)
	}
}

// A removed line whose own text begins with "--" arrives as "--- x", which is the
// shape of a path header. Reading it as one swallows the line and moves the file's
// path to whatever the content said.
func TestALineThatLooksLikeAPathHeaderIsStillALine(t *testing.T) {
	got := diff.Parse(fixture(t, "diff_text"))

	if len(got) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(got), got)
	}
	if got[0].Path != "notes.md" {
		t.Errorf("path = %q, want notes.md", got[0].Path)
	}
	if n := len(got[0].Hunks); n != 1 {
		t.Fatalf("got %d hunks, want 1", n)
	}

	lines := got[0].Hunks[0].Lines
	if n := len(lines); n != 6 {
		t.Fatalf("got %d lines, want 6: %+v", n, lines)
	}
	if lines[3].Kind != diff.Removed || lines[3].Text != "-- a sql comment" {
		t.Errorf("line 4 = %v %q, want a removed %q", lines[3].Kind, lines[3].Text, "-- a sql comment")
	}
}

// Line numbers are what reviewed state anchors to, so the two sides have to count
// independently through a hunk that adds and removes.
func TestLineNumbersCountEachSideSeparately(t *testing.T) {
	got := diff.Parse(fixture(t, "multiple_hunks"))

	if len(got) != 1 {
		t.Fatalf("got %d files, want 1", len(got))
	}
	for _, h := range got[0].Hunks {
		old, next := h.OldStart, h.NewStart
		for i, l := range h.Lines {
			switch l.Kind {
			case diff.Context:
				if l.Old != old || l.New != next {
					t.Errorf("%s line %d: old/new = %d/%d, want %d/%d", h.Header, i, l.Old, l.New, old, next)
				}
				old++
				next++
			case diff.Removed:
				if l.Old != old || l.New != 0 {
					t.Errorf("%s line %d: old/new = %d/%d, want %d/0", h.Header, i, l.Old, l.New, old)
				}
				old++
			case diff.Added:
				if l.New != next || l.Old != 0 {
					t.Errorf("%s line %d: old/new = %d/%d, want 0/%d", h.Header, i, l.Old, l.New, next)
				}
				next++
			}
		}
		if want := h.OldStart + h.OldLines; old != want {
			t.Errorf("%s: the old side ended at %d, want %d", h.Header, old, want)
		}
		if want := h.NewStart + h.NewLines; next != want {
			t.Errorf("%s: the new side ended at %d, want %d", h.Header, next, want)
		}
	}
}

// A file with no hunks has to say why, or it reads as unchanged.
func TestEveryFileWithoutHunksSaysWhy(t *testing.T) {
	tests := []struct {
		fixture string
		want    string
	}{
		{"mode_only", "mode change only"},
		{"rename", "renamed, contents unchanged"},
		{"copy", "copied, contents unchanged"},
		{"binary", "binary"},
		{"binary_quoted_path", "binary"},
		{"binary_backslash_path", "binary"},
		{"empty_file", "the new file is empty"},
		{"empty_file_removed", "the removed file was empty"},
		{"conflicted_cc", "conflicted, not yet merged"},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			got := diff.Parse(fixture(t, tt.fixture))
			if len(got) != 1 {
				t.Fatalf("got %d files, want 1", len(got))
			}
			if got[0].Omitted != tt.want {
				t.Errorf("omitted = %q, want %q", got[0].Omitted, tt.want)
			}
			if len(got[0].Hunks) != 0 {
				t.Errorf("got %d hunks, want none", len(got[0].Hunks))
			}
			if got[0].Path == "" {
				t.Error("the file has no path")
			}
		})
	}
}

// A path is the identity everything else hangs off, and the header forms that
// carry no --- and +++ lines are the ones where it is easiest to lose.
func TestPathsSurviveEveryHeaderForm(t *testing.T) {
	tests := []struct {
		fixture string
		path    string
		oldPath string
	}{
		{fixture: "modify", path: "a.txt"},
		{fixture: "add", path: "new.txt"},
		{fixture: "delete", path: "gone.txt"},
		{fixture: "rename", path: "new.txt", oldPath: "old.txt"},
		{fixture: "rename_with_edits", path: "new.txt", oldPath: "old.txt"},
		{fixture: "copy", path: "b.txt", oldPath: "a.txt"},
		{fixture: "mode_only", path: "run.sh"},
		{fixture: "binary_with_space", path: "my file.bin"},
		{fixture: "quoted_path", path: `say"hi".txt`},
		{fixture: "binary_quoted_path", path: `say"hi".bin`},
		{fixture: "binary_backslash_path", path: `back\`},
		{fixture: "submodule", path: "sub"},
		{fixture: "conflicted_cc", path: "a.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			got := diff.Parse(fixture(t, tt.fixture))
			if len(got) != 1 {
				t.Fatalf("got %d files, want 1", len(got))
			}
			if got[0].Path != tt.path {
				t.Errorf("path = %q, want %q", got[0].Path, tt.path)
			}
			if got[0].OldPath != tt.oldPath {
				t.Errorf("old path = %q, want %q", got[0].OldPath, tt.oldPath)
			}
		})
	}
}

// The side a file does not exist on has no blob. A caller writing a generation
// should not have to recognise forty zeros.
func TestTheMissingSideHasNoBlob(t *testing.T) {
	added := diff.Parse(fixture(t, "add"))[0]
	if added.OldBlob != "" {
		t.Errorf("added file old blob = %q, want empty", added.OldBlob)
	}
	if len(added.NewBlob) != 40 {
		t.Errorf("added file new blob = %q, want a full sha", added.NewBlob)
	}

	deleted := diff.Parse(fixture(t, "delete"))[0]
	if deleted.NewBlob != "" {
		t.Errorf("deleted file new blob = %q, want empty", deleted.NewBlob)
	}
	if len(deleted.OldBlob) != 40 {
		t.Errorf("deleted file old blob = %q, want a full sha", deleted.OldBlob)
	}
}
