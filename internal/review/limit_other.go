//go:build !linux && !windows

package review

import "os"

// limitOwnMemory: macOS doesn't enforce an address-space limit, so the
// helper's heap is held to its limit by watchMemory alone.
func limitOwnMemory(int64) {}

func limitChildMemory(*os.Process, int64) func() { return func() {} }
