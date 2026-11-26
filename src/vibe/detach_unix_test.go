//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"testing"
)

// What else a command was set to start with (by internal/proc, say) is kept when it is detached.
func TestDetachingKeepsHowTheCommandWasSetToStart(t *testing.T) {
	cmd := exec.Command("true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Noctty: true}
	detach(cmd)
	if !cmd.SysProcAttr.Setsid || !cmd.SysProcAttr.Noctty {
		t.Fatalf("want a session of its own added, the rest kept, got %+v", cmd.SysProcAttr)
	}
}
