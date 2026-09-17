// Package prompt provides small interactive terminal prompts for vibe's
// command line — an arrow-key list picker in the style of inquirer.
package prompt

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Select renders labels as an arrow-key-navigable list on rw — which must be
// both readable and writable and refer to a real terminal, since it is put
// into raw mode for the duration of the call — and returns the chosen
// index. def is the index highlighted first. Up/Down (and k/j) move the
// selection, enter confirms, q/Esc/Ctrl-C cancel (ok=false).
//
// The caller is expected to have already printed the question above the
// list; Select only draws the options and collapses them to the chosen
// answer once the user is done.
func Select(rw *os.File, labels []string, def int) (choice int, ok bool) {
	if len(labels) == 0 {
		return 0, false
	}
	fd := int(rw.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return 0, false
	}
	defer term.Restore(fd, state)

	sel := def
	if sel < 0 || sel >= len(labels) {
		sel = 0
	}
	lines := len(labels) + 1
	draw(rw, labels, sel)

	buf := make([]byte, 1)
	for {
		n, err := rw.Read(buf)
		if err != nil || n == 0 {
			clear(rw, lines)
			return 0, false
		}
		switch buf[0] {
		case 3, 'q', 'Q': // Ctrl-C, q
			clear(rw, lines)
			return 0, false
		case '\r', '\n':
			clear(rw, lines)
			fmt.Fprintf(rw, "  \x1b[32m✔\x1b[0m %s\r\n", labels[sel])
			return sel, true
		case 'k':
			if sel > 0 {
				sel--
			}
		case 'j':
			if sel < len(labels)-1 {
				sel++
			}
		case 0x1b:
			switch readEscape(rw) {
			case escUp:
				if sel > 0 {
					sel--
				}
			case escDown:
				if sel < len(labels)-1 {
					sel++
				}
			case escBare:
				clear(rw, lines)
				return 0, false
			default:
				continue
			}
		default:
			continue
		}
		up(rw, lines)
		draw(rw, labels, sel)
	}
}

type escKey int

const (
	escOther escKey = iota
	escUp
	escDown
	escBare
)

// escapeWait is how long readEscape waits for each byte of the rest of an
// arrow-key sequence before giving up and treating the ESC as a bare
// Escape keypress. Real terminals emit the whole sequence in one burst, so
// this only ever matters for a genuine standalone Escape.
//
// os.File's own read deadline isn't used here: on at least one platform
// this was tested on, a raw-mode tty fd's Read never woke up on its
// deadline, hanging indefinitely on a lone Escape. Polling the fd directly
// sidesteps that.
const escapeWait = 30 // milliseconds

// readEscape reads what follows an already-consumed ESC byte.
func readEscape(rw *os.File) escKey {
	b1, ok := pollByte(rw)
	if !ok || b1 != '[' {
		return escBare
	}
	b2, ok := pollByte(rw)
	if !ok {
		return escBare
	}
	switch b2 {
	case 'A':
		return escUp
	case 'B':
		return escDown
	default:
		return escOther
	}
}

// pollByte waits up to escapeWait for rw to become readable and reads a
// single byte from it. ok is false on timeout or read error.
func pollByte(rw *os.File) (b byte, ok bool) {
	pfd := []unix.PollFd{{Fd: int32(rw.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(pfd, escapeWait); err != nil || n == 0 {
		return 0, false
	}
	buf := make([]byte, 1)
	if n, err := rw.Read(buf); err != nil || n == 0 {
		return 0, false
	}
	return buf[0], true
}

func draw(w *os.File, labels []string, sel int) {
	for i, label := range labels {
		fmt.Fprint(w, "\x1b[2K\r")
		if i == sel {
			fmt.Fprintf(w, "\x1b[36m❯ %s\x1b[0m\r\n", label)
		} else {
			fmt.Fprintf(w, "  %s\r\n", label)
		}
	}
	fmt.Fprint(w, "\x1b[2K\r\x1b[2m(↑/↓ to move, enter to select, q to quit)\x1b[0m")
}

// up moves the cursor back to the first drawn line, ready to redraw in
// place. lines is the total number of lines draw prints, including the
// trailing hint line.
func up(w *os.File, lines int) {
	fmt.Fprintf(w, "\r\x1b[%dA", lines-1)
}

// clear wipes every line draw printed and leaves the cursor at column 0 of
// what was the first line.
func clear(w *os.File, lines int) {
	fmt.Fprintf(w, "\r\x1b[%dA", lines-1)
	for i := 0; i < lines; i++ {
		fmt.Fprint(w, "\x1b[2K")
		if i < lines-1 {
			fmt.Fprint(w, "\r\n")
		}
	}
	fmt.Fprint(w, "\r")
}
