package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
)

// These run inside the package and over literals, with no repository behind
// them. What is under test is the prose, and building a real changeset to reach
// a sentence would only make it slower to find out which sentence is wrong.

func hunk() diff.Hunk {
	return diff.Hunk{Header: "@@ -1 +1 @@", Lines: []diff.Line{{Kind: diff.Added, Text: "x"}}}
}

// built is a view of a session with one modified file, which the cases below
// vary from.
func built() view {
	return view{
		SessionID: "9f3a1c4d5e6f7081",
		Ref:       "refs/zen-review/sessions/9f3a1c4d5e6f7081",
		Kind:      "branch",
		Branch:    "feature",
		Base:      review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"},
		Generation: review.Generation{
			Seq:       2,
			CommitSha: "cccccccccccccccccccccccccccccccccccccccc",
			BaseSha:   "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
			HeadSha:   "dddddddddddddddddddddddddddddddddddddddd",
			CreatedAt: time.Date(2026, 8, 11, 12, 4, 33, 0, time.UTC),
		},
		Exists: true,
		Files: []diff.File{{
			Path: "a.txt", Status: diff.FileModified,
			Hunks: []diff.Hunk{hunk()}, Additions: 5, Deletions: 1,
		}},
	}
}

func TestTheProseSaysWhatHappened(t *testing.T) {
	rebased := built()
	rebased.Stale = true
	rebased.Base.SHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	touched := built()
	touched.Stale = true

	empty := built()
	empty.Files = nil

	// A generation built on a clean branch, then edited against. Saying there are
	// no changes here contradicts the line above it, and it is the half that
	// sounds like an answer.
	emptyStale := built()
	emptyStale.Files = nil
	emptyStale.Stale = true

	unbuilt := built()
	unbuilt.Exists = false
	unbuilt.Stale = true
	unbuilt.Generation = review.Generation{}
	unbuilt.Files = nil

	partial := built()
	partial.Skipped = []string{"vendored/", "locked.txt"}

	// Nothing built yet, and a path git could not read. There is no generation to
	// have mentioned it earlier, so this is the case that has to say so.
	unbuiltPartial := unbuilt
	unbuiltPartial.Skipped = []string{"locked.txt"}

	for _, tc := range []struct {
		name   string
		v      view
		want   []string
		absent []string
	}{
		{
			name: "a generation that still holds",
			v:    built(),
			want: []string{
				"base        origin/main (a1b2c3d)",
				"generation  2",
				"session     refs/zen-review/sessions/9f3a1c4d5e6f7081",
				"M  a.txt  1 hunk  +5 -1",
				"1 file, 1 hunk",
			},
			absent: []string{"has moved", "the base moved"},
		},
		{
			name:   "the work tree moved",
			v:      touched,
			want:   []string{"the work tree has moved since generation 2 was built"},
			absent: []string{"the base moved"},
		},
		{
			name: "the base moved",
			v:    rebased,
			want: []string{
				"the base moved to eeeeeee since generation 2 was measured from a1b2c3d",
			},
			absent: []string{"the work tree has moved"},
		},
		{
			name:   "nothing to review",
			v:      empty,
			want:   []string{"no changes since origin/main"},
			absent: []string{"1 file"},
		},
		{
			name: "nothing to review, and the tree has moved since",
			v:    emptyStale,
			want: []string{
				"the work tree has moved since generation 2 was built",
				"generation 2 held no changes since origin/main",
			},
			// The present tense would deny the movement reported one line above it.
			absent: []string{"\nno changes since origin/main"},
		},
		{
			name: "no generation yet",
			v:    unbuilt,
			want: []string{"no generation yet, so run zen-review refresh"},
			// The heading would be a lie, and there is no changeset to count.
			absent: []string{"generation  ", "0 files", "no changes since"},
		},
		{
			name: "paths git could not read",
			v:    partial,
			want: []string{
				"git could not read 2 paths just now, so they are not in this review:",
				"  vendored/",
				"  locked.txt",
			},
		},
		{
			name: "paths git could not read, with nothing built yet",
			v:    unbuiltPartial,
			want: []string{
				"no generation yet, so run zen-review refresh",
				"git could not read 1 path just now, so they are not in this review:",
				"  locked.txt",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(tc.v)

			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("output does not contain %q:\n%s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("output should not contain %q:\n%s", absent, got)
				}
			}
			for i, line := range strings.Split(got, "\n") {
				if strings.TrimRight(line, " ") != line {
					t.Errorf("line %d ends in whitespace: %q", i+1, line)
				}
			}
		})
	}
}

// The columns line up on the widest cell, and the last one on a row is never
// padded. A row carrying no churn is the case that used to leave a trailing
// space behind.
func TestTheColumnsLineUpWithoutTrailingWhitespace(t *testing.T) {
	v := built()
	v.Files = []diff.File{
		{Path: "short.txt", Status: diff.FileModified, Hunks: []diff.Hunk{hunk()}, Additions: 1},
		{Path: "a-considerably-longer-name.txt", Status: diff.FileAdded, Omitted: "binary"},
	}

	got := render(v)

	for _, want := range []string{
		"M  short.txt                       1 hunk  +1 -0\n",
		"A  a-considerably-longer-name.txt  binary\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
}

// The engine reports staleness as one bool over two causes, because it compares
// the base and the tree together. Both bases are in the view, so which one moved
// is derivable, and the reader gets the sentence that describes what they did.
func TestStalenessSplitsIntoItsTwoCauses(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    func() view
		want staleness
	}{
		{
			name: "nothing moved",
			v:    built,
			want: fresh,
		},
		{
			name: "the tree moved",
			v: func() view {
				v := built()
				v.Stale = true
				return v
			},
			want: staleTree,
		},
		{
			name: "the base moved",
			v: func() view {
				v := built()
				v.Stale = true
				v.Base.SHA = "eeee"
				return v
			},
			want: staleBase,
		},
		{
			// Nothing has been reviewed against, so no base and no tree moved to
			// make it so. The absent generation is the whole story.
			name: "no generation to be stale against",
			v: func() view {
				v := built()
				v.Exists = false
				v.Stale = true
				return v
			},
			want: fresh,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.v().reason(); got != tc.want {
				t.Errorf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}

// A nil slice marshals to null, and a caller should not have to handle two
// spellings of empty.
func TestEmptyListsArePresentRatherThanNull(t *testing.T) {
	v := built()
	v.Files = nil
	v.Skipped = nil

	p := payloadOf(v)

	if p.Files == nil {
		t.Error("files is nil, so it will marshal to null")
	}
	if p.Skipped == nil {
		t.Error("skipped is nil, so it will marshal to null")
	}
}

// A session that has never refreshed reports a null generation rather than a
// zeroed object beside a flag, which says nothing a reader can act on.
func TestAnAbsentGenerationIsNull(t *testing.T) {
	v := built()
	v.Exists = false

	if p := payloadOf(v); p.Generation != nil {
		t.Errorf("generation = %+v, want null", *p.Generation)
	}
}
