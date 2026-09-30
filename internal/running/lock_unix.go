//go:build !windows

package running

import (
	"os"

	"golang.org/x/sys/unix"
)

// lock takes an exclusive lock on f without waiting for it.
func lock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
