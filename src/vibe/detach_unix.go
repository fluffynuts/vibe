//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a session of its own, away from this terminal: it
// isn't ended when the terminal closes, and can't ask anything on it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
