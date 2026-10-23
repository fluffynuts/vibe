//go:build !windows

package fscopy

// InUse says err is the file being open elsewhere, which Windows doesn't allow
// to be replaced (or, for the moment it is being replaced, opened). Always false
// here, where a file is replaced under whoever has it open.
func InUse(err error) bool { return false }
