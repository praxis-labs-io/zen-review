package theme

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// reply drives the decode half of Query over a canned terminal answer, which is
// everything but the raw-mode dance.
func reply(t *testing.T, answer string) Surface {
	t.Helper()
	return collect(strings.NewReader(answer), queryTimeout)
}

// collect is reply over any reader, for the cases that need one that does not
// hand its whole answer over at once. It drives Query's own dispatch rather than
// a copy of it, which is the only way these cases say anything about the code
// that ships.
func collect(in io.Reader, timeout time.Duration) Surface {
	var s Surface
	read(in, &bytes.Buffer{}, "", timeout, s.take)
	return s
}

func hexOf(c color.Color) string {
	if c == nil {
		return "nil"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func TestQueryReadsBothReports(t *testing.T) {
	got := reply(t, "\x1b]10;rgb:e0e0/dede/f4f4\x1b\\\x1b]11;rgb:2323/2121/3636\x1b\\\x1b[?62;c")

	if want := "#e0def4"; hexOf(got.Foreground) != want {
		t.Errorf("Foreground = %s, want %s", hexOf(got.Foreground), want)
	}
	if want := "#232136"; hexOf(got.Background) != want {
		t.Errorf("Background = %s, want %s", hexOf(got.Background), want)
	}
}

// The two slots the tints lean on. A tint is a blend, and blending a slot takes
// its canonical value rather than the one the terminal actually paints, so they
// are read off the palette instead of assumed.
func TestQueryReadsThePaletteSlots(t *testing.T) {
	got := reply(t, "\x1b]4;1;rgb:cccc/2424/1d1d\x1b\\\x1b]4;2;rgb:9898/9797/1a1a\x1b\\\x1b[?62;c")

	if want := "#cc241d"; hexOf(got.Red) != want {
		t.Errorf("Red = %s, want %s", hexOf(got.Red), want)
	}
	if want := "#98971a"; hexOf(got.Green) != want {
		t.Errorf("Green = %s, want %s", hexOf(got.Green), want)
	}
}

// A terminal is free to answer for slots nobody asked about, and one that does
// must not have its blue land in the field the added tint is derived from.
func TestQueryIgnoresPaletteSlotsItDidNotAskFor(t *testing.T) {
	got := reply(t, "\x1b]4;4;rgb:0000/0000/ffff\x1b\\\x1b[?62;c")

	if got.Red != nil || got.Green != nil {
		t.Errorf("Surface = %+v, want an unasked-for slot to land nowhere", got)
	}
}

// The palette is the half most likely to go unanswered: a terminal that reports
// its background may still say nothing about slot 1. The rest has to survive it.
func TestQueryTakesTheSurfaceWithoutThePalette(t *testing.T) {
	got := reply(t, "\x1b]11;rgb:2323/2121/3636\x1b\\\x1b[?62;c")

	if got.Background == nil {
		t.Error("Background is nil, want the report that did arrive")
	}
	if got.Red != nil || got.Green != nil {
		t.Errorf("Red = %v and Green = %v, want nil when the palette went unanswered", got.Red, got.Green)
	}
}

// Terminals answer with components of varying width, and a short one scales up
// rather than being read at face value.
func TestQueryScalesShortComponents(t *testing.T) {
	got := reply(t, "\x1b]11;rgb:1c/1c/1c\x1b\\\x1b[?62;c")
	if want := "#1c1c1c"; hexOf(got.Background) != want {
		t.Errorf("Background = %s, want %s", hexOf(got.Background), want)
	}
}

// One of the two answering is the common case on a terminal that supports only
// half of it, and the half that came back has to survive.
func TestQueryTakesWhicheverAnswered(t *testing.T) {
	got := reply(t, "\x1b]11;rgb:2323/2121/3636\x1b\\\x1b[?62;c")

	if got.Foreground != nil {
		t.Errorf("Foreground = %v, want nil when nothing reported one", got.Foreground)
	}
	if got.Background == nil {
		t.Error("Background is nil, want the report that did arrive")
	}
}

// The device attributes end the read. Reading only until the colors parsed would
// leave them in the buffer, and the terminal echoes them the moment raw mode
// ends, before anything has been drawn.
func TestTheDeviceAttributesEndTheRead(t *testing.T) {
	// Nothing follows the attributes here: a read that ran past them would block
	// on a reader with nothing left and be cancelled by the timeout instead.
	done := make(chan Surface, 1)
	go func() { done <- reply(t, "\x1b]11;rgb:2323/2121/3636\x1b\\\x1b[?62;c") }()

	select {
	case got := <-done:
		if got.Background == nil {
			t.Error("Background is nil, want the reply read before the attributes stopped it")
		}
	case <-time.After(queryTimeout / 2):
		t.Fatal("the read ran past the device attributes")
	}
}

// A terminal that answers nothing must not hang the launch, and must not leave
// a reader parked on the tty eating the first key pressed. A reader that ends
// proves neither: it returns of its own accord and the timeout never fires.
//
// It has to be an os.Pipe rather than any blocking io.Reader. The cancel reader
// interrupts a file descriptor and cannot interrupt an arbitrary Read, so a
// hand-written blocking reader tests a path this never takes: what Query passes
// is os.Stdin, and a pipe is the same kind of thing.
func TestASilentTerminalGivesUpAndReportsNothing(t *testing.T) {
	silent, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("opening a pipe: %v", err)
	}
	defer silent.Close() //nolint:errcheck
	defer w.Close()      //nolint:errcheck

	done := make(chan Surface, 1)
	go func() { done <- collect(silent, 20*time.Millisecond) }()

	select {
	case got := <-done:
		if got.Background != nil || got.Foreground != nil {
			t.Errorf("Surface = %+v, want both nil when nothing answered", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the read never gave up, so a silent terminal hangs the launch")
	}
}

// A reply arriving in pieces is the ordinary case over a slow link. The decoder
// carries acc and state across reads, and only a reader that actually returns
// the answer in more than one Read exercises that: string literals beside each
// other are one string by the time the test runs, and a strings.Reader hands
// the whole of it over in a single call.
func TestASplitReplyStillParses(t *testing.T) {
	full := "\x1b]11;rgb:2323/2121/3636\x1b\\\x1b[?62;c"

	// Split inside the color sequence, so the carry is what has to work.
	got := collect(&chunked{parts: []string{full[:12], full[12:]}}, queryTimeout)
	if want := "#232136"; hexOf(got.Background) != want {
		t.Errorf("Background = %s, want %s", hexOf(got.Background), want)
	}
}

// chunked hands its answer over one piece per Read, which strings.Reader will
// not do.
type chunked struct{ parts []string }

func (c *chunked) Read(p []byte) (int, error) {
	if len(c.parts) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.parts[0])
	c.parts = c.parts[1:]
	return n, nil
}
