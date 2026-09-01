package review_test

import (
	"slices"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
)

// The patches below are what a generation-to-generation diff looks like for one
// file. Real diff text parsed by the real parser, because what is under test is
// whether a range lands where git says the lines went.
const (
	insertAbove = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -4,3 +4,5 @@ func f() {
 four
+inserted one
+inserted two
 five
 six
`

	insertInside = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -14,3 +14,4 @@ func f() {
 fourteen
+inserted
 fifteen
 sixteen
`

	deleteInside = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -14,3 +14,2 @@ func f() {
 fourteen
-fifteen
 sixteen
`

	// Lines 13 to 15 go, which leaves two separate reviewed ranges touching.
	closeTheGap = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -12,7 +12,4 @@ func f() {
 twelve
-thirteen
-fourteen
-fifteen
 sixteen
 seventeen
 eighteen
`

	// Everything from 10 to 20 replaced.
	rewritten = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -9,13 +9,5 @@ func f() {
 nine
-ten
-eleven
-twelve
-thirteen
-fourteen
-fifteen
-sixteen
-seventeen
-eighteen
-nineteen
-twenty
+alpha
+beta
+gamma
 twentyone
`

	// The agent decided half of its own change was wrong and put 14 to 20 back.
	// This is the case all-or-nothing survival gets wrong: 10 to 13 never moved.
	middleReverted = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -11,11 +11,7 @@ func f() {
 eleven
 twelve
 thirteen
-fourteen
-fifteen
-sixteen
-seventeen
-eighteen
-nineteen
-twenty
+alpha
+beta
+gamma
 twentyone
`

	// No context at all, which is the shape the remap's own diff runs at and the
	// only one where git names the line an insertion follows rather than one
	// inside the hunk.
	insertOnly = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -10,0 +11,3 @@ func f() {
+alpha
+beta
+gamma
`

	// An insertion, then a deletion below it, so the offset a range takes depends
	// on which side of the second hunk it sits.
	twoHunks = `diff --git a/a.go b/a.go
index aaaaaaa..bbbbbbb 100644
--- a/a.go
+++ b/a.go
@@ -4,3 +4,5 @@ func f() {
 four
+inserted one
+inserted two
 five
 six
@@ -20,4 +22,2 @@ func g() {
 twenty
-twentyone
-twentytwo
 twentythree
`

	renamed = `diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go
`

	renamedWithEdits = `diff --git a/old.go b/new.go
similarity index 90%
rename from old.go
rename to new.go
index aaaaaaa..bbbbbbb 100644
--- a/old.go
+++ b/new.go
@@ -4,3 +4,4 @@ func f() {
 four
+inserted
 five
 six
`

	modeChange = `diff --git a/s.sh b/s.sh
old mode 100644
new mode 100755
`

	binaryChange = `diff --git a/logo.png b/logo.png
index aaaaaaa..bbbbbbb 100644
Binary files a/logo.png and b/logo.png differ
`

	deletedFile = `diff --git a/a.go b/a.go
deleted file mode 100644
index aaaaaaa..0000000
--- a/a.go
+++ /dev/null
@@ -1,3 +0,0 @@
-one
-two
-three
`
)

func TestAReviewedRangeKeepsTheLinesThatDidNotChange(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		in    []review.Range
		want  []review.Range
	}{
		{
			name:  "an insertion above it shifts it",
			patch: insertAbove,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  []review.Range{{Start: 12, End: 22}},
		},
		{
			name:  "an insertion inside it splits it around the new line",
			patch: insertInside,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  []review.Range{{Start: 10, End: 14}, {Start: 16, End: 21}},
		},
		{
			name:  "a deleted line inside it closes over the gap",
			patch: deleteInside,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  []review.Range{{Start: 10, End: 19}},
		},
		{
			name:  "two ranges join when the lines between them go",
			patch: closeTheGap,
			in:    []review.Range{{Start: 10, End: 12}, {Start: 16, End: 18}},
			want:  []review.Range{{Start: 10, End: 15}},
		},
		{
			name:  "a wholesale rewrite loses it",
			patch: rewritten,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  nil,
		},
		{
			name:  "reverting the second half keeps the first",
			patch: middleReverted,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  []review.Range{{Start: 10, End: 13}},
		},
		{
			name:  "a rename with no content change carries it untouched",
			patch: renamed,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  []review.Range{{Start: 10, End: 20}},
		},
		{
			name:  "a rename with edits translates it",
			patch: renamedWithEdits,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  []review.Range{{Start: 11, End: 21}},
		},
		{
			name:  "a mode change carries it untouched",
			patch: modeChange,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  []review.Range{{Start: 10, End: 20}},
		},
		{
			name:  "a binary file loses it, because no line can be followed",
			patch: binaryChange,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  nil,
		},
		{
			name:  "a deleted file loses it",
			patch: deletedFile,
			in:    []review.Range{{Start: 10, End: 20}},
			want:  nil,
		},
		{
			name:  "an untouched range far below the change still shifts",
			patch: insertAbove,
			in:    []review.Range{{Start: 100, End: 104}},
			want:  []review.Range{{Start: 102, End: 106}},
		},
		{
			name:  "an insertion-only hunk leaves the line it follows where it was",
			patch: insertOnly,
			in:    []review.Range{{Start: 5, End: 10}},
			want:  []review.Range{{Start: 5, End: 10}},
		},
		{
			name:  "an insertion-only hunk shifts everything after it",
			patch: insertOnly,
			in:    []review.Range{{Start: 11, End: 15}},
			want:  []review.Range{{Start: 14, End: 18}},
		},
		{
			name:  "a range spanning an insertion-only hunk splits around it",
			patch: insertOnly,
			in:    []review.Range{{Start: 8, End: 14}},
			want:  []review.Range{{Start: 8, End: 10}, {Start: 14, End: 17}},
		},
		{
			name:  "the offset a range takes is every hunk above it, not the nearest",
			patch: twoHunks,
			in:    []review.Range{{Start: 10, End: 15}, {Start: 24, End: 26}},
			want:  []review.Range{{Start: 12, End: 17}, {Start: 24, End: 26}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := parseOne(t, tc.patch)
			got := review.Translate(file).Ranges(tc.in)

			if !slices.Equal(got, tc.want) {
				t.Errorf("Ranges(%v) = %v, want %v", tc.in, got, tc.want)
			}
			noChangedLineInside(t, file, got)
		})
	}
}

func TestACommentAnchorClampsToWhatSurvived(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		in    review.Range
		want  review.Range
		held  bool
	}{
		{
			name:  "it spans the edit rather than orphaning over it",
			patch: insertInside,
			in:    review.Range{Start: 10, End: 20},
			want:  review.Range{Start: 10, End: 21},
			held:  true,
		},
		{
			name:  "it clamps to the half that survived",
			patch: middleReverted,
			in:    review.Range{Start: 10, End: 20},
			want:  review.Range{Start: 10, End: 13},
			held:  true,
		},
		{
			name:  "it shifts with an insertion above it",
			patch: insertAbove,
			in:    review.Range{Start: 10, End: 20},
			want:  review.Range{Start: 12, End: 22},
			held:  true,
		},
		{
			name:  "a wholesale rewrite orphans it",
			patch: rewritten,
			in:    review.Range{Start: 10, End: 20},
			held:  false,
		},
		{
			name:  "a deleted file orphans it",
			patch: deletedFile,
			in:    review.Range{Start: 10, End: 20},
			held:  false,
		},
		{
			name:  "a binary file orphans it",
			patch: binaryChange,
			in:    review.Range{Start: 10, End: 20},
			held:  false,
		},
		{
			name:  "a rename with no content change carries it untouched",
			patch: renamed,
			in:    review.Range{Start: 10, End: 20},
			want:  review.Range{Start: 10, End: 20},
			held:  true,
		},
		{
			name:  "an anchor on the file as a whole comes through a rename",
			patch: renamed,
			in:    review.Range{Start: 0, End: 0},
			want:  review.Range{Start: 0, End: 0},
			held:  true,
		},
		{
			name:  "an anchor on the file as a whole survives its bytes changing",
			patch: binaryChange,
			in:    review.Range{Start: 0, End: 0},
			want:  review.Range{Start: 0, End: 0},
			held:  true,
		},
		{
			name:  "an anchor on the file as a whole survives an edit to it",
			patch: insertAbove,
			in:    review.Range{Start: 0, End: 0},
			want:  review.Range{Start: 0, End: 0},
			held:  true,
		},
		{
			name:  "an anchor on the file as a whole survives a wholesale rewrite",
			patch: rewritten,
			in:    review.Range{Start: 0, End: 0},
			want:  review.Range{Start: 0, End: 0},
			held:  true,
		},
		{
			name:  "an anchor on the file as a whole goes when the file does",
			patch: deletedFile,
			in:    review.Range{Start: 0, End: 0},
			held:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, held := review.Translate(parseOne(t, tc.patch)).Anchor(tc.in)

			if held != tc.held {
				t.Fatalf("Anchor(%v) held = %v, want %v", tc.in, held, tc.held)
			}
			if held && got != tc.want {
				t.Errorf("Anchor(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// A whole-file comment anchor and a whole-file reviewed mark take opposite rules
// through the same patch, and this is the pair that says so. A mark is a claim
// about bytes somebody read and an edit voids it; a comment is a remark about
// the file and an edit is what it asked for.
func TestAWholeFileMarkAndCommentPartOnAnEdit(t *testing.T) {
	whole := review.Range{Start: 0, End: 0}

	for _, patch := range []string{insertAbove, rewritten, binaryChange} {
		file := review.Translate(parseOne(t, patch))

		if got := file.Ranges([]review.Range{whole}); got != nil {
			t.Errorf("Ranges kept a whole-file mark through an edit: got %v, want nil", got)
		}
		if _, held := file.Anchor(whole); !held {
			t.Error("Anchor lost a whole-file comment through an edit, which is the feedback being deleted by the change that answers it")
		}
	}
}

// A file with no hunks is marked as a whole, and that mark follows the content
// rather than any line: it survives a move and dies when the bytes change.
func TestAFileMarkedAsAWholeFollowsItsContent(t *testing.T) {
	whole := []review.Range{{Start: 0, End: 0}}

	tests := []struct {
		name  string
		patch string
		want  []review.Range
	}{
		{name: "a rename carries it", patch: renamed, want: whole},
		{name: "a mode change carries it", patch: modeChange, want: whole},
		{name: "changed bytes lose it", patch: binaryChange, want: nil},
		{name: "an edit loses it", patch: insertAbove, want: nil},
		{name: "a deleted file loses it", patch: deletedFile, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := review.Translate(parseOne(t, tc.patch)).Ranges(whole)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Ranges(%v) = %v, want %v", whole, got, tc.want)
			}
		})
	}
}

// A whole-file mark ends at line 0, so a range starting at line 1 looks like it
// touches it. Joining the two would report the file from its first line to its
// last as read, which is the worst thing this can get wrong.
func TestAWholeFileMarkDoesNotSwallowTheRangeBelowIt(t *testing.T) {
	in := []review.Range{{Start: 0, End: 0}, {Start: 1, End: 5}}
	want := []review.Range{{Start: 0, End: 0}, {Start: 1, End: 5}}

	got := review.Translate(parseOne(t, renamed)).Ranges(in)
	if !slices.Equal(got, want) {
		t.Errorf("Ranges(%v) = %v, want %v", in, got, want)
	}
}

func parseOne(t *testing.T, patch string) diff.File {
	t.Helper()

	files := diff.Parse([]byte(patch))
	if len(files) != 1 {
		t.Fatalf("the patch describes %d files, want 1", len(files))
	}
	return files[0]
}

// noChangedLineInside is the safety property the whole feature rests on. A line
// the new side introduces is one nobody has read, and a surviving reviewed range
// covering it would report it as read.
func noChangedLineInside(t *testing.T, f diff.File, ranges []review.Range) {
	t.Helper()

	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Kind != diff.Added {
				continue
			}
			for _, r := range ranges {
				if r.Start <= l.New && l.New <= r.End {
					t.Errorf("line %d is new and sits inside the reviewed range %v", l.New, r)
				}
			}
		}
	}
}
