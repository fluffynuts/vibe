//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachedProcess is Windows' DETACHED_PROCESS: no console at all, so
// closing this one doesn't end it.
const detachedProcess = 0x00000008

// detach starts cmd away from this console: it isn't ended when the console
// closes, and can't ask anything on it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
}
