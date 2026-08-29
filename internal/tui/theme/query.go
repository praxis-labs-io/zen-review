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

const queryTimeout = 500 * time.Millisecond

// Surface is what the terminal reported about itself. Any field is nil where
// nothing answered.
type Surface struct {
	Background color.Color
	Foreground color.Color
	Red        color.Color
	Green      color.Color
}

func requestPalette(slot int) string {
	return fmt.Sprintf("\x1b]4;%d;?\x07", slot)
}

// Query asks the terminal for its background, foreground and the two slots the
// diff tints lean on. It must run before Bubble Tea takes the tty.
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

	// The attributes go last and are what ends the read: a terminal answers
	// them and answers them last.
	query := ansi.RequestForegroundColor + ansi.RequestBackgroundColor +
		requestPalette(int(slotRed)) + requestPalette(int(slotGreen)) +
		ansi.RequestPrimaryDeviceAttributes

	read(in, out, query, queryTimeout, s.take)
	return s
}

// take files one decoded reply and reports whether to keep reading. A method so
// the tests drive this dispatch rather than a copy of it.
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

func oscColor(pa *ansi.Parser) color.Color {
	spec := afterSemicolon(string(pa.Data()))
	if spec == "" {
		return nil
	}
	return ansi.XParseColor(spec)
}

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
// false or the timeout cancels the read.
func read(in io.Reader, out io.Writer, query string, timeout time.Duration, filter func(string, *ansi.Parser) bool) {
	// A cancellable reader, or the timeout leaves a goroutine parked on the tty
	// eating the first key pressed.
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
