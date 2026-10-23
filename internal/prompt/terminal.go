package prompt

import (
	"io"
	"os"

	"golang.org/x/term"
)

// Terminal is where an interactive prompt reads keys from and draws to. On
// Unix both are the one /dev/tty; a Windows console has separate input and
// output handles (CONIN$ and CONOUT$), so the two are kept apart here.
type Terminal struct {
	in, out *os.File
	closeFn func()
	buf     []byte
	// restoreRaw puts the terminal back out of raw mode while it is in it.
	restoreRaw func()
	// onInterrupt, when set, is called as Ctrl-C is read. In raw mode Ctrl-C is
	// a key, not a signal, so without it a prompt could only take Ctrl-C for
	// "skip this question", like q or Esc, and the process would go on to the
	// next. A Terminal made by Open ends the process; one without (a test's)
	// leaves Ctrl-C a cancel.
	onInterrupt func()
}

// exit ends the process; a variable for tests.
var exit = os.Exit

// exitCodeInterrupted is the status a process ended by Ctrl-C gives, by the
// shells' convention (128 and SIGINT's 2).
const exitCodeInterrupted = 130

// exitOnInterrupt is what Ctrl-C does at a prompt of a Terminal made by Open:
// wipe the prompt and put the terminal back out of raw mode, as for a process
// ending anyway (see Abort), then end, as Ctrl-C does anywhere else.
func (t *Terminal) exitOnInterrupt() {
	if !Abort() && t.restoreRaw != nil {
		t.restoreRaw()
	}
	io.WriteString(t.out, "\r\n")
	exit(exitCodeInterrupted)
}

// Open opens the terminal this process can ask a question on — never a
// redirected stdin, which would silently answer for the user. ok is false
// when there is no terminal at all.
func Open() (t *Terminal, ok bool) {
	in, out, closeFn, ok := openTerminal()
	if !ok {
		return nil, false
	}
	enableVT(out)
	return newTerminal(in, out, closeFn), true
}

// newTerminal is a Terminal on in and out as Open makes one: Ctrl-C ends the
// process (see onInterrupt).
func newTerminal(in, out *os.File, closeFn func()) *Terminal {
	t := &Terminal{in: in, out: out, closeFn: closeFn}
	t.onInterrupt = t.exitOnInterrupt
	return t
}

// OpenInput opens the same terminal as Open, for reading only — for a
// line-at-a-time question that doesn't need raw mode.
func OpenInput() (in *os.File, closeFn func(), ok bool) {
	in, _, closeFn, ok = openTerminal()
	return in, closeFn, ok
}

// Close releases whatever Open opened.
func (t *Terminal) Close() {
	if t != nil && t.closeFn != nil {
		t.closeFn()
	}
}

// Write draws on the terminal.
func (t *Terminal) Write(p []byte) (int, error) {
	return t.out.Write(p)
}

var _ io.Writer = (*Terminal)(nil)

// ReadMasked reads a line typed or pasted on the terminal, showing a * for
// each character rather than the character itself — for a token, which
// shouldn't be left on the screen or in its scrollback, but whose length
// is worth seeing: a paste that came out short, or twice over, shows.
// Backspace takes the last character back, Ctrl-U all of them; Esc
// gives up (ok=false), and Ctrl-C ends vibe (see onInterrupt). The caller prints the question first.
func (t *Terminal) ReadMasked() (line string, ok bool) {
	restore, err := t.makeRaw()
	if err != nil {
		return "", false
	}
	defer restore()
	return readMasked(t, t)
}

// makeRaw puts the terminal's input into raw mode, returning the function
// that puts it back.
func (t *Terminal) makeRaw() (restore func(), err error) {
	fd := int(t.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	restore = func() {
		term.Restore(fd, state)
		t.restoreRaw = nil
	}
	t.restoreRaw = restore
	return restore, nil
}

// fill reads whatever is waiting on the terminal into t.buf. readChunk is 1
// on Unix, so nothing typed ahead is taken from the tty that a later
// question should see; Windows' console reader hands back everything it
// has at once, so a key sequence has to be caught in one read there.
func (t *Terminal) fill() bool {
	chunk := make([]byte, readChunk)
	n, err := t.in.Read(chunk)
	if err != nil || n == 0 {
		return false
	}
	t.buf = append(t.buf, chunk[:n]...)
	return true
}

// readByte blocks for the next byte of input. ok is false on a read error.
func (t *Terminal) readByte() (b byte, ok bool) {
	if len(t.buf) == 0 && !t.fill() {
		return 0, false
	}
	b, t.buf = t.buf[0], t.buf[1:]
	if b == 3 && t.onInterrupt != nil {
		t.onInterrupt()
	}
	return b, true
}

// pollByte is readByte, but gives up after escapeWait when nothing arrives.
func (t *Terminal) pollByte() (b byte, ok bool) {
	if len(t.buf) == 0 && !waitReadable(t.in, escapeWait) {
		return 0, false
	}
	return t.readByte()
}

// waitByte is readByte, but gives up after ms milliseconds when nothing
// arrives.
func (t *Terminal) waitByte(ms int) (b byte, ok bool) {
	if len(t.buf) == 0 && !waitReadable(t.in, ms) {
		return 0, false
	}
	return t.readByte()
}

// IsTerminal reports whether f is a terminal a question could be asked on.
func IsTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// EnableVT makes f — stdout or stderr, say — understand the ANSI escapes
// vibe draws with. Only a Windows console needs telling; elsewhere it is a
// no-op.
func EnableVT(f *os.File) {
	enableVT(f)
}

// width is how many columns wide the terminal is, or 0 when it won't say.
// It is asked afresh on every redraw, since the window can be resized
// while a prompt is up.
func (t *Terminal) width() int {
	return Width(t.out, 0)
}

// Width is how many columns wide the terminal f is, or fallback when f
// isn't a terminal or won't say.
func Width(f *os.File, fallback int) int {
	if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
		return w
	}
	return fallback
}
