//go:build !windows

package review

import "syscall"

// nonBlocking opens a FIFO without waiting for a writer: it's then seen not
// to be a plain file, and refused.
const nonBlocking = syscall.O_NONBLOCK
