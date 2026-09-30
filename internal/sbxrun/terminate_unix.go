//go:build !windows

package sbxrun

import (
	"os"
	"os/exec"
	"syscall"
)

// terminate asks p to end, the way a user closing the terminal would.
func terminate(p *os.Process) {
	p.Signal(syscall.SIGTERM)
}

// ignoreTerminalInterrupt starts cmd in a process group of its own, so the
// SIGINT a terminal sends its foreground group on Ctrl-C doesn't reach it.
// Cancelling it kills that whole group, so nothing sbx started is left
// holding its output open.
func ignoreTerminalInterrupt(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
