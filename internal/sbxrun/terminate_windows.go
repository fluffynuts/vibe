//go:build windows

package sbxrun

import (
	"os"
	"os/exec"
	"syscall"
)

// terminate ends p. Windows has no signal to ask a console program to end
// that can be sent to just one process, so it is killed; the sandbox itself
// is untouched, as it is when sbx is ended any other way.
func terminate(p *os.Process) {
	p.Kill()
}

// ignoreTerminalInterrupt starts cmd in a console process group of its own,
// which Windows doesn't deliver Ctrl-C to.
func ignoreTerminalInterrupt(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
