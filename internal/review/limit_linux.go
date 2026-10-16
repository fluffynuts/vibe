//go:build linux

package review

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// limitOwnMemory caps the helper's address space, so an allocation past
// its limit fails at once (watchMemory catches the rest): what it has
// mapped already, and room for limit four times over, as Go's heap maps
// more than it uses.
func limitOwnMemory(limit int64) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmSize:"); ok {
			kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			if err != nil {
				return
			}
			max := uint64(kb<<10 + 4*limit)
			syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: max, Max: max})
			return
		}
	}
}

func limitChildMemory(*os.Process, int64) func() { return func() {} }
