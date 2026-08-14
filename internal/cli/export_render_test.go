package cli

import (
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// The lines under the heading are what a paste has to carry: it lands in front
// of somebody who cannot see the repository, so anything making the references
// below less true than they look has to travel with them.
//
// Over literals, with no repository behind them, for the reason the prose tests
// beside these give: what is under test is the sentence.
func TestTheReportSaysWhatItsReferencesAreWorth(t *testing.T) {
	reported := func(v exportView) exportView {
		v.header = opened()
		v.Title, v.Reviewed, v.Items = "feature", 4, 9
		return v
	}

	rebased := reported(exportView{})
	rebased.Stale = true
	rebased.Base.SHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	touched := reported(exportView{})
	touched.Stale = true

	partial := reported(exportView{})
	partial.Skipped = []string{"vendored/", "locked.txt"}

	unbuilt := reported(exportView{})
	unbuilt.Exists = false
	unbuilt.Generation = review.Generation{}
	unbuilt.Skipped = []string{"locked.txt"}

	stopped := reported(exportView{Comments: []store.Comment{{
		ID:        "9a8b7c6d5e4f",
		Path:      "internal/cli/export.go",
		Side:      store.SideHead,
		Scope:     store.ScopeRange,
		LineRange: store.LineRange{Start: 41, End: 48},
		State:     store.CommentOrphaned,
		Body:      "this one lost its anchor",
	}}})

	commented := reported(exportView{Comments: []store.Comment{{
		ID:        "3f2a1b0c9d8e",
		Path:      "internal/cli/export.go",
		Side:      store.SideHead,
		Scope:     store.ScopeRange,
		LineRange: store.LineRange{Start: 41, End: 48},
		State:     store.CommentOpen,
		Body:      "the early return misses the comment case",
	}}})

	for _, tc := range []struct {
		name   string
		v      exportView
		want   []string
		absent []string
	}{
		{
			name: "a generation that still holds",
			v:    reported(exportView{}),
			want: []string{
				"# Review of feature\n",
				"base `origin/main` (a1b2c3d), generation 2",
				"4 of 9 reviewed, nothing unresolved",
			},
			absent: []string{"has moved", "could not read"},
		},
		{
			name:   "the base moved under it",
			v:      rebased,
			want:   []string{"The base has moved to eeeeeee since generation 2 was measured"},
			absent: []string{"work tree"},
		},
		{
			name:   "the work tree moved under it",
			v:      touched,
			want:   []string{"The work tree has moved since generation 2 was built"},
			absent: []string{"The base has moved"},
		},
		{
			// An edit nobody can see is the failure this tool exists to prevent, and a
			// report counting what it read without naming what it could not is that
			// failure with a number beside it.
			name: "a path git could not read",
			v:    partial,
			want: []string{"git could not read 2 paths just now", "vendored/, locked.txt"},
		},
		{
			// The one case with no earlier warning that a file is missing from what is
			// being reported, so it is the one that has to carry it.
			name: "nothing built yet, and a path git could not read",
			v:    unbuilt,
			want: []string{
				"No generation yet, so nothing has been reviewed.", "zen-review refresh",
				"git could not read 1 path just now", "locked.txt",
			},
			absent: []string{"reviewed, ", "generation 2"},
		},
		{
			name: "something still to answer",
			v:    commented,
			want: []string{
				"4 of 9 reviewed, 1 comment unresolved",
				"## internal/cli/export.go\n",
				"**`internal/cli/export.go:41-48`** head, open, `3f2a1b0c9d8e`",
				"\nthe early return misses the comment case\n",
			},
			absent: []string{"nothing unresolved", "stopped moving"},
		},
		{
			// The line is where the anchor was when the comment settled, which is not
			// where the generation named above puts that file. The reader of a paste
			// cannot check, and the state word beside it does not say so.
			name: "a comment that has stopped moving",
			v:    stopped,
			want: []string{
				"An addressed or orphaned comment stopped moving when it was settled",
				"**`internal/cli/export.go:41-48`** head, orphaned, `9a8b7c6d5e4f`",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.v.markdown()

			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("the report does not say %q:\n%s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("the report says %q and should not:\n%s", absent, got)
				}
			}
			if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
				t.Errorf("the report does not end in exactly one newline:\n%q", got)
			}
		})
	}
}
