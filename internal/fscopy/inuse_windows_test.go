//go:build windows

package fscopy

import (
	"os"
	"syscall"
	"testing"
)

func TestWindowsSaysAFileIsInUseInThreeWays(t *testing.T) {
	for name, errno := range map[string]syscall.Errno{"access denied": 5, "sharing violation": 32, "lock violation": 33} {
		if err := (&os.LinkError{Op: "rename", Old: "a", New: "b", Err: errno}); !InUse(err) {
			t.Errorf("%s (%d) wasn't taken for a file in use", name, errno)
		}
	}
	for name, errno := range map[string]syscall.Errno{"file not found": 2, "path not found": 3, "already exists": 183} {
		if err := (&os.PathError{Op: "open", Path: "a", Err: errno}); InUse(err) {
			t.Errorf("%s (%d) was taken for a file in use", name, errno)
		}
	}
}
