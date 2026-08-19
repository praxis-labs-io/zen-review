package review_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
)

// TestDeriveOrdersTheFilesTheWayATreeReads over the shapes the rule has to
// answer for. Each case is given to Derive shuffled, so a case that happens to
// arrive in order proves nothing.
func TestDeriveOrdersTheFilesTheWayATreeReads(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{
			name: "a directory sorts above a file beside it",
			want: []string{"internal/git/diff.go", "README.md"},
		},
		{
			name: "files in one directory sort by byte",
			want: []string{"internal/cli/render.go", "internal/cli/status.go"},
		},
		{
			name: "the deeper path is not the earlier one",
			want: []string{"a/b/c.go", "a/z.go"},
		},
		{
			name: "a dot sorts below every letter",
			want: []string{".github/ci.yml", "cmd/main.go", ".gitignore", "CLAUDE.md"},
		},
		{
			// The joined strings would answer the other way round: "/" is below
			// "-", so "z/deep.go" would sort above "z-x.go".
			name: "a directory is placed by its own name, not the path under it",
			want: []string{"z/deep.go", "z-x.go"},
		},
		{
			// A file turning into the directory that took its name. Git emits
			// both, and the directory is still a directory.
			name: "a path that is another's prefix is the file, and goes second",
			want: []string{"src/foo/bar.go", "src/foo"},
		},
		{
			name: "the whole shape at once",
			want: []string{
				"docs/spec.md",
				"internal/cli/render.go",
				"internal/review/state.go",
				"internal/review/zz.go",
				"internal/tui/app/app.go",
				"CLAUDE.md",
				"README.md",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := paths(review.Derive(shuffled(c.want), nil, nil))
			if !slices.Equal(got, c.want) {
				t.Errorf("the files came back in the wrong order:\ngot  %s\nwant %s",
					strings.Join(got, " "), strings.Join(c.want, " "))
			}
		})
	}
}

// shuffled is the files in the one order that is wrong for every case: the
// reverse of the wanted one.
func shuffled(want []string) []diff.File {
	in := slices.Clone(want)
	slices.Reverse(in)

	files := make([]diff.File, 0, len(in))
	for _, p := range in {
		files = append(files, diff.File{Path: p})
	}
	return files
}

func paths(c review.Changeset) []string {
	out := make([]string, 0, len(c.Files))
	for _, f := range c.Files {
		out = append(out, f.Diff.Path)
	}
	return out
}
