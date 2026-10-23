//go:build windows

package fscopy

import (
	"errors"
	"syscall"
)

// Windows' sharing and lock violations.
const (
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

// InUse says err is the file being open elsewhere, which Windows doesn't allow
// to be replaced (or, for the moment it is being replaced, opened): not a
// problem with the file, and gone the next moment. Always false elsewhere,
// where a file is replaced under whoever has it open.
func InUse(err error) bool {
	return errors.Is(err, errorSharingViolation) || errors.Is(err, errorLockViolation)
}
