//go:build windows

package review

// nonBlocking is nothing on Windows, which has no FIFOs a sandbox could
// leave in the folder.
const nonBlocking = 0
