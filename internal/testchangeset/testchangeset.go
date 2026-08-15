// Package testchangeset builds changesets for the render tests.
//
// It runs no git and opens no database. diff.Parse and review.Derive are both
// pure, so a patch and a list of ranges are the whole fixture, and a test that
// wants a repository behind it uses internal/testrepo instead.
//
// Test-only.
package testchangeset

import (
	"testing"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// Derive is the changeset a patch and a set of read ranges make. No file reads
// as changed after review: that fact comes from a refresh, and there is none
// here.
func Derive(t *testing.T, patch string, reviewed ...store.ReviewedRange) review.Changeset {
	t.Helper()

	files := diff.Parse([]byte(patch))
	if len(files) == 0 {
		t.Fatalf("the patch parsed to no files, so the fixture is malformed")
	}
	return review.Derive(files, reviewed, nil)
}

// Head is one read range on the head side.
func Head(path string, start, end int) store.ReviewedRange {
	return store.ReviewedRange{
		Path:      path,
		Side:      store.SideHead,
		LineRange: store.LineRange{Start: start, End: end},
	}
}

// Comment is one open head-side comment. The scope follows the lines: 0:0 names
// the file, a run of them is a range, and anything else is one line.
func Comment(id, path string, start, end int, body string) store.Comment {
	scope := store.ScopeLine
	switch {
	case start == 0 && end == 0:
		scope = store.ScopeFile
	case start != end:
		scope = store.ScopeRange
	}

	return store.Comment{
		ID:                  id,
		SessionID:           "session",
		GenerationID:        2,
		CreatedGenerationID: 2,
		Path:                path,
		Side:                store.SideHead,
		LineRange:           store.LineRange{Start: start, End: end},
		Scope:               scope,
		Body:                body,
		State:               store.CommentOpen,
	}
}

// In is a comment in a state other than open, for the ladder the card draws.
func In(c store.Comment, s store.CommentState) store.Comment {
	c.State = s
	return c
}

// OnBase moves a comment to the base side, where it is recorded under the name
// the file has on the base rather than the one the changeset lists it under.
func OnBase(c store.Comment) store.Comment {
	c.Side = store.SideBase
	return c
}

// NestedComments is one of every shape a card takes, against NestedPatch: a
// line, a range, the file itself, a stray, and one of each settled state.
func NestedComments() []store.Comment {
	const state = "internal/review/state.go"

	return []store.Comment{
		Comment("aaaaaaaaaaaa", "README.md", 0, 0, "Does this still read right?"),
		Comment("bbbbbbbbbbbb", state, 13, 13, "unreviewed is the longer word and the clearer one."),
		Comment("cccccccccccc", state, 124, 125,
			"These two say the same thing twice. Cut the second."),
		In(Comment("dddddddddddd", state, 126, 126, "Derive takes the rows now."),
			store.CommentAddressed),
		In(Comment("eeeeeeeeeeee", state, 900, 900, "This line left the changeset."),
			store.CommentOrphaned),
		In(Comment("ffffffffffff", "README.md", 3, 3, "The old line said it better."),
			store.CommentResolved),
	}
}

// Nested is the fixture the tree and the panes are drawn from. It holds every
// shape a row can take:
//
//   - a file at the repository root
//   - a chain of directories nothing branches off, which folds into one row
//   - three directories under one parent, which does not fold
//   - a path far wider than the tree pane
//   - a file with two hunks, one of them read, so the file is partial
//   - a binary file, which has no hunks and no churn to print
func Nested(t *testing.T) review.Changeset {
	t.Helper()

	return Derive(t, NestedPatch,
		Head("docs/superpowers/specs/design.md", 1, 3),
		Head("internal/review/state.go", 124, 125),
	)
}

// NestedPatch is unified diff text in the shape git writes it.
//
// Every context line carries text. A context line for a blank source line is a
// lone space, which an editor trimming trailing whitespace would eat, and the
// fixture would then fail somewhere far from the edit that broke it.
const NestedPatch = `diff --git a/README.md b/README.md
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/README.md
+++ b/README.md
@@ -1,3 +1,3 @@
 # zen-review
 Drew's local code review engine.
-A diff viewer with review features bolted on.
+A review engine with a TUI attached.
diff --git a/assets/logo.png b/assets/logo.png
new file mode 100644
index 0000000000000000000000000000000000000000..a6a3e7f3bad02f24f974be4ecd3e7b95afcfa21e
Binary files /dev/null and b/assets/logo.png differ
diff --git a/docs/superpowers/specs/design.md b/docs/superpowers/specs/design.md
new file mode 100644
index 0000000000000000000000000000000000000000..66a52ee7a1d803dc57859c3e95ac9dcdc87c0164
--- /dev/null
+++ b/docs/superpowers/specs/design.md
@@ -0,0 +1,3 @@
+# Design
+Two panes and a status bar.
+Tree left at 32 columns, diff right.
diff --git a/internal/cli/render.go b/internal/cli/render.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/internal/cli/render.go
+++ b/internal/cli/render.go
@@ -14,4 +14,4 @@ const label = "%-10s  %s\n"
 func render(v view) string {
 	var b strings.Builder
 	writeFiles(&b, v.Files)
-	return b.String()
+	return strings.TrimSpace(b.String())
diff --git a/internal/review/state.go b/internal/review/state.go
index bab081fdb7372d4e471fcbb12b886e1a7cddcae2..a59766543cc0c21a4435adcb73723af1b039aafb 100644
--- a/internal/review/state.go
+++ b/internal/review/state.go
@@ -10,5 +10,5 @@ package review
 // State is how much of a hunk or a file has been read.
 type State string
 const (
-	Unreviewed State = "unread"
+	Unreviewed State = "unreviewed"
 	Partial    State = "partial"
@@ -120,5 +120,7 @@ func (s *Session) Changeset(ctx context.Context) (Changeset, error) {
 	rows, err := s.db.ReviewedRanges(ctx, g.ID)
 	if err != nil {
 		return Changeset{}, err
 	}
+	// Ranges are read off the generation.
+	// Never off the working tree.
 	return Derive(files, rows), nil
diff --git a/internal/tui/diffpane/painting_the_unified_rows.go b/internal/tui/diffpane/painting_the_unified_rows.go
new file mode 100644
index 0000000000000000000000000000000000000000..66a52ee7a1d803dc57859c3e95ac9dcdc87c0164
--- /dev/null
+++ b/internal/tui/diffpane/painting_the_unified_rows.go
@@ -0,0 +1,2 @@
+package diffpane
+// The pane that paints the rows.
`
