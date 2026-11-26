//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a session of its own, away from this terminal: it
// isn't ended when the terminal closes, and can't ask anything on it. Added to
// how cmd starts, keeping what else it was given (internal/proc's settings, say).
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
