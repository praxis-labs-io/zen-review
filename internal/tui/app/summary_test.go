package app_test

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// TestAFailedSaveKeepsTheWords. The write is a local transaction that landed or
// did not, and the only thing it can cost is what the reader typed.
func TestAFailedSaveKeepsTheWords(t *testing.T) {
	s := noting(t, "", 100, 24)
	s.src.wroteErr = errors.New("the database is locked")
	s.press("C", "h", "i", "ctrl+s")

	got := s.frame()
	if !strings.Contains(got, "the database is locked") {
		t.Fatalf("the failure was not reported:\n%s", got)
	}
	if !strings.Contains(got, "Session note") {
		t.Errorf("the box came down on a write that did not land:\n%s", got)
	}
	if !strings.Contains(got, "hi") {
		t.Errorf("the words went with it:\n%s", got)
	}

	// And the retry writes them, rather than the reader typing them again.
	s.src.wroteErr = nil
	s.press("ctrl+s")

	if got := s.calls(); len(got) != 1 || got[0] != `SetSummary "hi"` {
		t.Errorf("the writes were %v, want the retry to carry the same words", got)
	}
}

// TestTypingOnThroughASaveKeepsTheBox. The write is out for as long as it takes,
// and a close landing on top of it would drop what was added meanwhile.
func TestTypingOnThroughASaveKeepsTheBox(t *testing.T) {
	s := noting(t, "", 100, 24)
	s.press("C", "h", "i")

	// Held, so the write is still out when the next key lands.
	saving := s.hold(keystroke("ctrl+s"))
	s.press("!")
	s.drain(saving)

	got := s.frame()
	if !strings.Contains(got, "Session note") {
		t.Fatalf("the box came down on the write it was typed past:\n%s", got)
	}
	if !strings.Contains(got, "hi!") {
		t.Errorf("what was typed after the save is gone:\n%s", got)
	}
}

// TestCtrlCReachesOutOfTheBox. Raw mode sends no interrupt, so the box would
// otherwise be the one place in the program with no way out but esc.
func TestCtrlCReachesOutOfTheBox(t *testing.T) {
	s := noting(t, "", 100, 24)
	s.press("C")

	cmd := s.hold(keystroke("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c in the box returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+c in the box did not quit")
	}
}

// TestAPasteReachesTheBox. Bracketed paste arrives as a message of its own
// rather than as keys, and a note is the one thing here anybody pastes.
func TestAPasteReachesTheBox(t *testing.T) {
	s := noting(t, "", 100, 24)
	s.press("C")
	s.send(tea.PasteMsg{Content: "pasted from somewhere else"})
	s.press("ctrl+s")

	want := `SetSummary "pasted from somewhere else"`
	if got := s.calls(); len(got) != 1 || got[0] != want {
		t.Errorf("the writes were %v, want %q", got, want)
	}
}

// TestTheBoxDrawsOnAFrameTooSmallForThePanes. It owns the keys wherever it is
// up, and one that is not on screen is a reader pressing q into nothing.
func TestTheBoxDrawsOnAFrameTooSmallForThePanes(t *testing.T) {
	s := noting(t, "what the session says", 50, 10)

	if got := s.frame(); !strings.Contains(got, "this needs") {
		t.Fatalf("the frame is not the too-small one:\n%s", got)
	}

	s.press("C")
	got := s.frame()
	if !strings.Contains(got, "Session note") {
		t.Errorf("the box took the keys and drew nothing:\n%s", got)
	}
	if !strings.Contains(got, "esc discard") {
		t.Errorf("the way out is not on screen:\n%s", got)
	}

	// The composite pads what it trimmed, and this frame is built by a different
	// path from the one the size table walks.
	for i, line := range s.lines() {
		if w := lipgloss.Width(line); w != 50 {
			t.Errorf("line %d is %d columns, want 50: %q", i, w, line)
		}
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
