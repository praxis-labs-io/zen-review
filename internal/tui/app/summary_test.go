package app_test

import (
	"strings"
	"testing"
)

// TestTheNoteOpensOverWhatTheSessionSays. A session resumed days later already
// has a note, and C is how it is added to rather than replaced.
func TestTheNoteOpensOverWhatTheSessionSays(t *testing.T) {
	s := noting(t, "the remap is the part to look at", 100, 24)
	s.press("C")

	got := s.frame()
	if !strings.Contains(got, "Session note") {
		t.Fatalf("C opened nothing titled:\n%s", got)
	}
	if !strings.Contains(got, "the remap is the part to look at") {
		t.Errorf("the box came up empty of what the session says:\n%s", got)
	}
	if !strings.Contains(got, "ctrl+s save") || !strings.Contains(got, "esc discard") {
		t.Errorf("the box does not say how to get out of it:\n%s", got)
	}
}

// TestTheSaveKeyWritesTheNote through the same seam every other write goes
// through, and says so on the bar.
func TestTheSaveKeyWritesTheNote(t *testing.T) {
	s := noting(t, "", 100, 24)
	s.press("C", "h", "i")
	s.press("ctrl+s")

	if got := s.calls(); len(got) != 1 || got[0] != `SetSummary "hi"` {
		t.Fatalf("the writes were %v, want the note", got)
	}
	if got := s.bar(); !strings.Contains(got, "note saved") {
		t.Errorf("the bar reads %q, want the write reported", got)
	}

	// The box is down, so the next key is the reader's again.
	if got := s.frame(); strings.Contains(got, "Session note") {
		t.Errorf("the box stayed up after the save:\n%s", got)
	}
}

// TestAnEmptiedNoteReportsItself. Empty is the only way to take a note back, so
// it is a thing that happened rather than a write that did nothing.
func TestAnEmptiedNoteReportsItself(t *testing.T) {
	s := noting(t, "x", 100, 24)
	s.press("C", "backspace", "ctrl+s")

	if got := s.calls(); len(got) != 1 || got[0] != `SetSummary ""` {
		t.Fatalf("the writes were %v, want the note cleared", got)
	}
	if got := s.bar(); !strings.Contains(got, "note cleared") {
		t.Errorf("the bar reads %q, want the clear reported", got)
	}
}

// TestDiscardingTheNoteWritesNothing, and leaves what the session says where it
// was: esc is the way out that costs nothing.
func TestDiscardingTheNoteWritesNothing(t *testing.T) {
	s := noting(t, "what the session says", 100, 24)
	s.press("C", "n", "o", "esc")

	if got := s.calls(); len(got) != 0 {
		t.Fatalf("esc wrote %v", got)
	}

	s.press("C")
	got := s.frame()
	if !strings.Contains(got, "what the session says") {
		t.Errorf("the discarded box came back:\n%s", got)
	}
	if strings.Contains(got, "what the session saysno") {
		t.Errorf("the typing survived the discard:\n%s", got)
	}
}

// TestTheBoxTakesTheKeysTheReaderOtherwiseOwns. q ends the session everywhere
// else, and a note lost to the letter q cannot be taken back.
func TestTheBoxTakesTheKeysTheReaderOtherwiseOwns(t *testing.T) {
	s := noting(t, "", 100, 24)
	s.press("C", "q", "?")

	got := s.frame()
	if !strings.Contains(got, "Session note") {
		t.Fatalf("a key meant for the body reached the reader:\n%s", got)
	}
	if strings.Contains(got, "previous unread") {
		t.Errorf("? opened the help over the box:\n%s", got)
	}

	s.press("ctrl+s")
	if got := s.calls(); len(got) != 1 || got[0] != `SetSummary "q?"` {
		t.Errorf("the writes were %v, want both keys typed into the body", got)
	}
}
