// Package portalloc probes the host for a free TCP port. The port a sandbox
// ends up with is remembered in its instance record (see internal/state), not
// here — this package only answers "can I bind this right now".
package portalloc

import (
	"fmt"
	"net"
)

// InUse reports whether something is already listening on the given port.
func InUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

// FindFree walks upward from start until it finds a port that is neither
// listed in taken nor being listened on, giving up above limit. A port can be
// claimed without anything listening on it — the sandbox holding it may be
// stopped, or it may have been handed out earlier in the same allocation
// pass — so taken is consulted first and InUse only confirms the rest.
func FindFree(start, limit int, taken map[int]bool) (int, error) {
	for port := start; port < limit; port++ {
		if taken[port] || InUse(port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("no free port found in %d-%d", start, limit-1)
}
