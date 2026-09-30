//go:build !windows

package sbxrun

import (
	"os"
	"syscall"
)

// terminate asks p to end, the way a user closing the terminal would.
func terminate(p *os.Process) {
	p.Signal(syscall.SIGTERM)
}
