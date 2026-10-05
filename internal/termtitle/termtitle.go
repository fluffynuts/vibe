// Package termtitle sets the terminal's title — the text on its tab, and
// the window's title while that tab is showing — with the OSC 0 escape
// sequence Konsole, Terminator, GNOME Terminal, Windows Terminal, iTerm2
// and most others honour. Some only show it if their settings let them
// (Konsole's tab title format needs %w, say), and inside tmux or screen it
// names the pane unless they're set to pass titles on.
//
// The title that was there before is pushed onto the terminal's title stack
// first and popped back by Restore. Terminals without one (Windows
// Terminal, Konsole) ignore both, and keep vibe's title until something
// else — often the shell's prompt — sets another.
package termtitle

import (
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

const (
	push = "\x1b[22;0t" // XTWINOPS: save icon and window title
	pop  = "\x1b[23;0t" // XTWINOPS: restore them
)

var (
	mu  sync.Mutex
	out io.Writer // where Set wrote, for Restore; nil until it has
)

// Set titles the terminal, if vibe's output goes to one.
func Set(title string) {
	w := terminal()
	if w == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if out == nil {
		io.WriteString(w, push)
	}
	io.WriteString(w, sequence(title))
	out = w
}

// Restore puts back the title Set replaced, where the terminal can; it does
// nothing if Set hasn't titled anything, and is safe to call more than once.
func Restore() {
	mu.Lock()
	defer mu.Unlock()
	if out == nil {
		return
	}
	io.WriteString(out, pop)
	out = nil
}

// sequence is the OSC 0 sequence that sets title, less any control
// characters: they'd end the sequence early, or worse.
func sequence(title string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, title)
	return "\x1b]0;" + clean + "\a"
}

// terminal is whichever of stdout and stderr is a terminal (stdout first),
// or nil when neither is, or the terminal is a dumb one.
func terminal() io.Writer {
	if os.Getenv("TERM") == "dumb" {
		return nil
	}
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if term.IsTerminal(int(f.Fd())) {
			return f
		}
	}
	return nil
}
