//go:build !windows

package review

import (
	"os"
	"syscall"
)

// openNoFollow opens path to read without following a symbolic link there,
// nor waiting on a FIFO: the caller then checks it's a plain file.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
