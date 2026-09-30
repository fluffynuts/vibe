//go:build windows

package running

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset is where the locked byte sits: far past the folder path written
// at the start of the file, since Windows locks are mandatory and a locked
// region can't be read by anyone else — and the path is what Others reads.
const lockOffset uint64 = 1 << 62

// lock takes an exclusive lock on f without waiting for it.
func lock(f *os.File) error {
	ol := &windows.Overlapped{Offset: uint32(lockOffset & 0xffffffff), OffsetHigh: uint32(lockOffset >> 32)}
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
}
