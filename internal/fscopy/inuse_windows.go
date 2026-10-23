//go:build windows

package fscopy

import (
	"errors"
	"syscall"
)

// What Windows says of a file that is open elsewhere. A sharing or lock
// violation is it plainly; "access denied" is what it says, too, of replacing
// or opening a file whose last instance is only waiting for its readers to let
// go of it (delete pending), which is exactly what replacing a file under a
// reader leaves for a moment (found by the race tests on CI: the rename of one
// replacement onto the name another had just made was denied, not "in use").
const (
	errorAccessDenied     syscall.Errno = 5
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

// InUse says err is the file being open elsewhere, which Windows doesn't allow
// to be replaced (or, for the moment it is being replaced, opened): not a
// problem with the file, and gone the next moment. Access denied is taken for
// it too, as Windows says no more than that; a file that really can't be
// written is still an error, only after the short wait. Always false
// elsewhere, where a file is replaced under whoever has it open.
func InUse(err error) bool {
	return errors.Is(err, errorAccessDenied) || errors.Is(err, errorSharingViolation) || errors.Is(err, errorLockViolation)
}
