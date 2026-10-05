package prompt

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// frame is the block of lines a prompt draws, redrawn in place on every
// keypress. It remembers how wide each line it drew was, so it can find its
// way back to the top of them however the terminal has re-wrapped them
// since: narrow a window that reflows its contents (Windows Terminal, most
// modern Unix terminals) and every line that no longer fits takes up two
// rows, so moving up one row per line drawn stops short of the top and each
// redraw leaves the last one behind.
//
// Lines are cut to the terminal's width as they are drawn, so none of them
// wraps to begin with, and a redraw erases everything from the top of the
// frame to the end of the screen rather than line by line.
type frame struct {
	w     io.Writer
	width func() int // columns, or 0 when unknown

	shown   []int // visible width of each line last drawn; nil when nothing is
	restore func()
	once    sync.Once
	dead    bool // erased by Abort: the process is on its way out
}

// screenMu serialises drawing with Abort, which runs on another goroutine,
// and guards active.
var (
	screenMu sync.Mutex
	active   *frame
)

// openFrame puts t into raw mode and starts a frame on it. close must be
// called when the prompt is done with it.
func openFrame(t *Terminal) (*frame, error) {
	restore, err := t.makeRaw()
	if err != nil {
		return nil, err
	}
	f := &frame{w: t, width: t.width, restore: restore}
	screenMu.Lock()
	active = f
	screenMu.Unlock()
	return f, nil
}

// close puts the terminal back the way openFrame found it.
func (f *frame) close() {
	screenMu.Lock()
	if active == f {
		active = nil
	}
	screenMu.Unlock()
	f.once.Do(func() {
		if f.restore != nil {
			f.restore()
		}
	})
}

// Abort wipes whatever prompt is showing and puts the terminal back out of
// raw mode, for a process about to exit from another goroutine while a
// prompt waits on a keypress. The cursor is left at the start of the line
// the prompt began on. It reports whether there was a prompt to abort.
func Abort() bool {
	screenMu.Lock()
	defer screenMu.Unlock()
	f := active
	if f == nil {
		return false
	}
	active = nil
	f.eraseLocked()
	f.dead = true
	f.once.Do(func() {
		if f.restore != nil {
			f.restore()
		}
	})
	return true
}

// draw replaces what the frame last drew with lines. Raw mode means every
// break is CRLF; the last line is left without one, for the cursor to sit
// at the end of.
func (f *frame) draw(lines []string) {
	screenMu.Lock()
	defer screenMu.Unlock()
	if f.dead {
		return
	}
	f.eraseLocked()
	width := f.width()
	var b strings.Builder
	f.shown = make([]int, len(lines))
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		if width > 0 {
			// One short of the width: a line that exactly fills a row leaves
			// some terminals waiting to wrap, and others already wrapped.
			line = truncate(line, width-1)
		}
		b.WriteString(line)
		f.shown[i] = visibleWidth(line)
	}
	io.WriteString(f.w, b.String())
}

// clear erases what the frame last drew, leaving the cursor at the start of
// its first line.
func (f *frame) clear() {
	screenMu.Lock()
	defer screenMu.Unlock()
	if f.dead {
		return
	}
	f.eraseLocked()
}

// eraseLocked moves back to the start of the frame and erases to the end of
// the screen. The cursor is at the end of the last line drawn, so the rows
// to go up are all those the lines take up now, at the terminal's current
// width, less the one it is on.
func (f *frame) eraseLocked() {
	if f.shown == nil {
		return
	}
	rows := 0
	width := f.width()
	for _, w := range f.shown {
		rows += rowsFor(w, width)
	}
	if rows > 1 {
		fmt.Fprintf(f.w, "\r\x1b[%dA\x1b[J", rows-1)
	} else {
		io.WriteString(f.w, "\r\x1b[J")
	}
	f.shown = nil
}

// rowsFor is how many terminal rows a line of visible width w takes up on a
// terminal width columns wide (0: unknown, so assumed not to wrap).
func rowsFor(w, width int) int {
	if width <= 0 || w <= width {
		return 1
	}
	return (w + width - 1) / width
}

// visibleWidth counts the columns s takes up: its runes, less any CSI
// escape sequences (colours, mostly). Every rune vibe draws in a prompt is
// one column wide.
func visibleWidth(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if l := escapeLen(s[i:]); l > 0 {
			i += l
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return n
}

// truncate cuts s to max visible columns, ending it with an ellipsis and a
// colour reset when anything had to go. Escape sequences are kept intact.
func truncate(s string, max int) string {
	if max < 1 || visibleWidth(s) <= max {
		return s
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(s); {
		if l := escapeLen(s[i:]); l > 0 {
			b.WriteString(s[i : i+l])
			i += l
			continue
		}
		if n == max-1 {
			break
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+size])
		i += size
		n++
	}
	b.WriteString("…\x1b[0m")
	return b.String()
}

// escapeLen is the length of the CSI sequence ("ESC [ params final") s
// starts with, or 0 when it doesn't start with one.
func escapeLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			return i + 1
		}
	}
	return len(s)
}
