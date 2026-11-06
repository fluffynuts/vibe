//go:build !windows

// Package filelock takes advisory locks on open files, without waiting, for claims that must go when the process holding
// them does, however it ends: vibe's own claims on a folder (internal/running) and on host ports (internal/portalloc).
package filelock

import (
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes an exclusive lock on f without waiting for it, and fails when someone else has it.
func TryLock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
