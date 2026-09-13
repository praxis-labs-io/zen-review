package app_test

import (
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
)

func TestRIgnoresASelection(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.press("v", "j", "r")

	want := []string{"MarkHunk a.go head:1 gen=2"}
	if got := s.calls(); !equal(got, want) {
		t.Errorf("the reader wrote %v, want %v", got, want)
	}
}

func TestRAdvancesOutOfASelection(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)

	s.wrote(testchangeset.Derive(t, ringPatch, testchangeset.Head("a.go", 1, 1)))
	s.press("v", "j", "r")

	if got := heading(t, s); !strings.Contains(got, "@@ -10,0 +11,1 @@") {
		t.Errorf("the cursor is on %q, want the next unread hunk", got)
	}
}

func TestTheBarNamesTheKeysThatEndASelection(t *testing.T) {
	s := over(t, testchangeset.Derive(t, ringPatch), 100, 16)
	s.press("v", "j")

	got := s.bar()
	for _, want := range []string{"j/k extend", "c comment", "esc cancel"} {
		if !strings.Contains(got, want) {
			t.Errorf("the bar reads %q, want it to name %q", got, want)
		}
	}

	s.press("h")

	got = s.bar()
	if strings.Contains(got, "extend") {
		t.Errorf("the bar reads %q, want no claim that j extends from the tree", got)
	}
	for _, want := range []string{"enter open", "c comment", "esc cancel"} {
		if !strings.Contains(got, want) {
			t.Errorf("the bar reads %q, want it to name %q", got, want)
		}
	}

	s.press("esc")
	if got := s.bar(); strings.Contains(got, "esc cancel") {
		t.Errorf("the bar reads %q, want the selection gone", got)
	}
}
