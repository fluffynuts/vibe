//go:build windows

package proc

import (
	"syscall"
	"testing"
)

func TestWithNoConsoleAProgramStartsWithNoWindowKeepingItsOtherFlags(t *testing.T) {
	got := hidden(&syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}, false)
	if got.CreationFlags != syscall.CREATE_NEW_PROCESS_GROUP|createNoWindow || !got.HideWindow {
		t.Fatalf("want CREATE_NO_WINDOW added to the process group, hidden, got %+v", got)
	}
	if got := hidden(nil, false); got == nil || got.CreationFlags != createNoWindow {
		t.Fatalf("want CREATE_NO_WINDOW for a command with no attributes yet, got %+v", got)
	}
}

func TestWithAConsoleAProgramSharesIt(t *testing.T) {
	if got := hidden(nil, true); got != nil {
		t.Fatalf("want nothing changed for a vibe in a terminal (its programs may need it: sbx run, ssh), got %+v", got)
	}
}

func TestAddingCreationFlagsKeepsTheHiding(t *testing.T) {
	cmd := Command("cmd", "/c", "exit")
	cmd.SysProcAttr = hidden(cmd.SysProcAttr, false)
	AddCreationFlags(cmd, syscall.CREATE_NEW_PROCESS_GROUP)
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 || cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("want both flags, got %#x", cmd.SysProcAttr.CreationFlags)
	}
}

// The test binary runs with the console of whatever started it (none on CI's runner, perhaps): either way, a program it
// starts must run and end.
func TestAProgramStartedThroughProcRuns(t *testing.T) {
	if out, err := Command("cmd", "/c", "echo", "hello").Output(); err != nil || len(out) == 0 {
		t.Fatalf("want the program run, got %q, %v", out, err)
	}
}
