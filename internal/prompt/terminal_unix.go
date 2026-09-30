//go:build !windows

package prompt

import (
	"os"

	"golang.org/x/sys/unix"
)

const readChunk = 1

func openTerminal() (in, out *os.File, closeFn func(), ok bool) {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		return tty, tty, func() { tty.Close() }, true
	}
	if !IsTerminal(os.Stdin) {
		return nil, nil, func() {}, false
	}
	return os.Stdin, os.Stdin, func() {}, true
}

// waitReadable polls the fd directly: on at least one platform this was
// tested on, a raw-mode tty's Read never woke up on an os.File deadline.
func waitReadable(f *os.File, ms int) bool {
	pfd := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
	n, err := unix.Poll(pfd, ms)
	return err == nil && n > 0
}

func enableVT(*os.File) {}
