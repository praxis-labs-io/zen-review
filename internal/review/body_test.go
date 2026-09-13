package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func lines(n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(out, "\n") + "\n"
}

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
