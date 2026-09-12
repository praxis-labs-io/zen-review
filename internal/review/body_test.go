package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// lines is a file of numbered lines, long enough that what the diff shows of it
// is a fraction of what the whole of it is. Each line names its own number, so a
// body asserts against the line it came from.
func lines(n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(out, "\n") + "\n"
}

// bodied is a session over a file of twenty lines with one of them rewritten, so
// the changeset shows three lines of context and the file has seventeen more.
func bodied(t *testing.T) (*fixture, *review.Session, review.Generation) {
	t.Helper()

	f := newFixture(t)
	f.Write("long.txt", lines(20))
	f.commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")

	f.Write("long.txt", strings.Replace(lines(20), "line 10", "line ten", 1))
	s := f.mustOpen("")
	return f, s, f.refresh(s)
}

// TestBodyIsTheWholeFile. Three lines of context are what the read exists to get
// past, so what it hands back is every line and not the ones the diff showed.
func TestBodyIsTheWholeFile(t *testing.T) {
	f, s, g := bodied(t)

	b, err := s.Body(f.t.Context(), g, "long.txt")
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}

	if b.Side != store.SideHead {
		t.Errorf("the body came back on the %s side, want head", b.Side)
	}
	if len(b.Lines) != 20 {
		t.Fatalf("the body is %d lines, want 20", len(b.Lines))
	}
	if b.Lines[9] != "line ten" {
		t.Errorf("line 10 reads %q, want the rewritten one", b.Lines[9])
	}
	if b.Lines[19] != "line 20" {
		t.Errorf("the last line reads %q", b.Lines[19])
	}
}

// TestBodyIsTheGenerationAndNotTheWorkTree. A generation is a snapshot, and a
// read measured against the working tree would move under a reader mid-hunk.
func TestBodyIsTheGenerationAndNotTheWorkTree(t *testing.T) {
	f, s, g := bodied(t)
	f.Write("long.txt", "everything else\n")

	b, err := s.Body(f.t.Context(), g, "long.txt")
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(b.Lines) != 20 {
		t.Errorf("the body followed the work tree: %d lines", len(b.Lines))
	}
}

// TestBodyOfADeletedFileIsItsBase. A deleted file has no head bytes, and the
// side a whole-file mark lands on is the side its text is read from.
func TestBodyOfADeletedFileIsItsBase(t *testing.T) {
	f := newFixture(t)
	f.Write("gone.txt", lines(12))
	f.commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	f.Git("rm", "-q", "gone.txt")

	s := f.mustOpen("")
	g := f.refresh(s)

	b, err := s.Body(f.t.Context(), g, "gone.txt")
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if b.Side != store.SideBase {
		t.Errorf("a deleted file came back on the %s side, want base", b.Side)
	}
	if len(b.Lines) != 12 {
		t.Errorf("the body is %d lines, want the 12 it had", len(b.Lines))
	}
}

// TestBodyFollowsARename. The text is read by the name the changeset lists the
// file under, which a rename makes a different one from the base's.
func TestBodyFollowsARename(t *testing.T) {
	f := newFixture(t)
	f.Write("was.txt", lines(14))
	f.commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	f.Git("mv", "was.txt", "now.txt")
	f.Write("now.txt", strings.Replace(lines(14), "line 7", "line seven", 1))

	s := f.mustOpen("")
	g := f.refresh(s)

	b, err := s.Body(f.t.Context(), g, "now.txt")
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(b.Lines) != 14 || b.Lines[6] != "line seven" {
		t.Errorf("the body is %d lines and line 7 reads %q", len(b.Lines), b.Lines[6])
	}
}

// TestBodyOfAFileTheGenerationHasNotGot. Nothing answered is not a failure: the
// reader asked about a file, and the answer is that there is nothing to draw.
func TestBodyOfAFileTheGenerationHasNotGot(t *testing.T) {
	f, s, g := bodied(t)

	b, err := s.Body(f.t.Context(), g, "never.txt")
	if err != nil {
		t.Fatalf("reading a file the generation has not got: %v", err)
	}
	if len(b.Lines) != 0 {
		t.Errorf("it came back with %d lines", len(b.Lines))
	}
}

// TestBodyOfABinaryFile. The bytes come back as they are. Nothing here is a text
// file, and the pane is what refuses to draw one.
func TestBodyOfABinaryFile(t *testing.T) {
	f := newFixture(t)
	f.commit("first")
	f.TrackOrigin("main")
	f.Git("checkout", "-q", "-b", "feature")
	f.Write("logo.png", "\x89PNG\x00\x01\x02")

	s := f.mustOpen("")
	g := f.refresh(s)

	if _, err := s.Body(f.t.Context(), g, "logo.png"); err != nil {
		t.Fatalf("reading a binary file: %v", err)
	}
}
