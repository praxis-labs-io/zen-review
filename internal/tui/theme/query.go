package theme

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"strconv"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// queryTimeout is what a terminal that answers nothing costs. Anything that
// answers at all answers the device attributes, which ends the read at once, so
// this is paid only by a terminal that replies to none of the three.
const queryTimeout = 500 * time.Millisecond

// Surface is what the terminal says about itself. Any field is nil where
// nothing answered, and a theme is derived from whatever did.
type Surface struct {
	Background color.Color
	Foreground color.Color

	// The two slots the diff tints lean on. A tint is a blend, and blending a
	// slot takes its canonical value rather than the one the reader is looking
	// at, so these two are asked for by name instead of assumed.
	Red   color.Color
	Green color.Color
}

// requestPalette asks for one of the terminal's own palette slots. x/ansi has
// the foreground and background requests and not this one.
func requestPalette(slot int) string {
	return fmt.Sprintf("\x1b]4;%d;?\x07", slot)
}

// Query asks the terminal for its background, its foreground and the two palette
// slots the diff tints lean on.
//
// lipgloss has this for the background alone and does not export the machinery,
// and the foreground is what the shades want to travel toward: blended toward
// pure white or black instead they sit on a different axis from the reader's own
// text. So the four are asked for together, over one round trip.
//
// It must run before Bubble Tea takes the tty, and it puts the terminal in raw
// mode for the length of the exchange.
func Query(in, out *os.File) Surface {
	var s Surface
	if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return s
	}

	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return s
	}
	defer term.Restore(in.Fd(), state) //nolint:errcheck

	// The two slots ride along in the same write and the same read: they cost
	// no second round trip and no second timeout, which is the whole reason
	// asking for them is worth it where it would not be on its own.
	//
	// The device attributes go last and are what ends the read. A terminal
	// answers them and answers them last, so waiting on them is what tells an
	// unanswered color query apart from one still arriving.
	query := ansi.RequestForegroundColor + ansi.RequestBackgroundColor +
		requestPalette(int(slotRed)) + requestPalette(int(slotGreen)) +
		ansi.RequestPrimaryDeviceAttributes

	read(in, out, query, queryTimeout, s.take)
	return s
}

// take files one decoded reply and reports whether to keep reading. It is a
// method rather than a closure so a test drives the same dispatch the terminal
// does: a test that reimplemented it would stay green through 10 and 11 being
// swapped, and the shipped app would derive its greys from the background.
func (s *Surface) take(seq string, pa *ansi.Parser) bool {
	switch {
	case ansi.HasOscPrefix(seq):
		switch pa.Command() {
		case 4:
			switch slot, c := paletteColor(pa); slot {
			case int(slotRed):
				s.Red = c
			case int(slotGreen):
				s.Green = c
			}
		case 10:
			s.Foreground = oscColor(pa)
		case 11:
			s.Background = oscColor(pa)
		}
	case ansi.HasCsiPrefix(seq):
		if pa.Command() == ansi.Command('?', 0, 'c') {
			return false
		}
	}
	return true
}

// oscColor reads the color out of an OSC 10 or 11 reply, whose data is the
// command number and the color separated by a semicolon.
func oscColor(pa *ansi.Parser) color.Color {
	spec := afterSemicolon(string(pa.Data()))
	if spec == "" {
		return nil
	}
	return ansi.XParseColor(spec)
}

// paletteColor reads the slot and the color out of an OSC 4 reply, whose data
// is the command, the slot and the color separated by semicolons. The slot is
// -1 where the reply did not parse, which matches nothing.
func paletteColor(pa *ansi.Parser) (int, color.Color) {
	rest := afterSemicolon(string(pa.Data()))
	spec := afterSemicolon(rest)
	if spec == "" {
		return -1, nil
	}

	slot, err := strconv.Atoi(rest[:len(rest)-len(spec)-1])
	if err != nil {
		return -1, nil
	}
	return slot, ansi.XParseColor(spec)
}

func afterSemicolon(data string) string {
	for i := range len(data) {
		if data[i] == ';' {
			return data[i+1:]
		}
	}
	return ""
}

// read writes the query and feeds decoded sequences to filter until it returns
// false or the timeout cancels the read. The reader is a cancellable one so the
// timeout cannot leave a goroutine parked on the tty, eating the first key the
// reader presses.
//
// The reply is drained to the filter's own stopping point rather than to the
// last color parsed: leaving the device attributes in the buffer means raw mode
// ends, echo comes back, and the terminal prints them before anything is drawn.
func read(in io.Reader, out io.Writer, query string, timeout time.Duration, filter func(string, *ansi.Parser) bool) {
	rd, err := uv.NewCancelReader(in)
	if err != nil {
		return
	}
	defer rd.Close() //nolint:errcheck

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
		case <-time.After(timeout):
			rd.Cancel()
		}
	}()

	if _, err := io.WriteString(out, query); err != nil {
		return
	}

	pa := ansi.GetParser()
	defer ansi.PutParser(pa)

	var acc []byte
	var buf [256]byte
	var state byte
	for {
		n, err := rd.Read(buf[:])
		if err != nil {
			return
		}

		p := buf[:]
		for n > 0 {
			seq, _, count, next := ansi.DecodeSequence(p[:n], state, pa)
			acc = append(acc, seq...)

			if next == ansi.NormalState {
				if !filter(string(acc), pa) {
					return
				}
				acc = acc[:0]
			}

			state = next
			n -= count
			p = p[count:]
		}
	}
}
