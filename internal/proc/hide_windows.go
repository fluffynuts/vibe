//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is Windows' CREATE_NO_WINDOW: the program runs with no console window. Go's syscall doesn't name it.
const createNoWindow = 0x08000000

var getConsoleWindow = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")

// hasConsole says whether this process has a console: none for the background vibe --headless starts (DETACHED_PROCESS).
func hasConsole() bool {
	if getConsoleWindow.Find() != nil {
		return true
	}
	window, _, _ := getConsoleWindow.Call()
	return window != 0
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = hidden(cmd.SysProcAttr, hasConsole())
}

// hidden is attr with the program's window hidden when there's no console for it to share, keeping what else attr asks.
func hidden(attr *syscall.SysProcAttr, console bool) *syscall.SysProcAttr {
	if console {
		return attr
	}
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.CreationFlags |= createNoWindow
	attr.HideWindow = true
	return attr
}

// AddCreationFlags adds flags to what cmd is started with, keeping those it has (the hiding of its window, say).
func AddCreationFlags(cmd *exec.Cmd, flags uint32) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= flags
}
