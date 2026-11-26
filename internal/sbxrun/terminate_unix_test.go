//go:build !windows

package sbxrun

import (
	"os/exec"
	"syscall"
	"testing"
)

// What else a command was set to start with (by internal/proc, say) is kept: replacing it is how Windows' hidden windows
// were nearly lost there.
func TestIgnoringTheTerminalsInterruptKeepsHowTheCommandWasSetToStart(t *testing.T) {
	cmd := exec.Command("true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Noctty: true}
	ignoreTerminalInterrupt(cmd)
	if !cmd.SysProcAttr.Setpgid || !cmd.SysProcAttr.Noctty {
		t.Fatalf("want its own process group added, the rest kept, got %+v", cmd.SysProcAttr)
	}
	bare := exec.Command("true")
	ignoreTerminalInterrupt(bare)
	if bare.SysProcAttr == nil || !bare.SysProcAttr.Setpgid {
		t.Fatalf("want a process group for a command given nothing, got %+v", bare.SysProcAttr)
	}
}
