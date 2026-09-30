//go:build windows

package prompt

import (
	"os"

	"golang.org/x/sys/windows"
)

const readChunk = 64

func openTerminal() (in, out *os.File, closeFn func(), ok bool) {
	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		if !IsTerminal(os.Stdin) || !IsTerminal(os.Stderr) {
			return nil, nil, func() {}, false
		}
		return os.Stdin, os.Stderr, func() {}, true
	}
	conout, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		conin.Close()
		return nil, nil, func() {}, false
	}
	if !IsTerminal(conin) {
		conin.Close()
		conout.Close()
		return nil, nil, func() {}, false
	}
	return conin, conout, func() { conin.Close(); conout.Close() }, true
}

// waitReadable waits for the console input handle to be signalled. That
// happens for any input event — a window resize, say, as well as a key — so
// the read that follows can still block; for telling a lone Escape from the
// start of an arrow key's sequence, that is close enough.
func waitReadable(f *os.File, ms int) bool {
	ev, err := windows.WaitForSingleObject(windows.Handle(f.Fd()), uint32(ms))
	return err == nil && ev == windows.WAIT_OBJECT_0
}

func enableVT(f *os.File) {
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return
	}
	windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
}
