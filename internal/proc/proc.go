// Package proc is how vibe starts other programs: Command and CommandContext, as os/exec's, which on Windows keep a program
// started by a vibe with no console of its own (the background vibe --headless leaves, say) from opening a console window
// of its own. Windows gives each console program started that way a new console, which flashes up and goes: with sbx,
// gh and vibe's own helper started every few seconds, the screen was never still. A vibe in a terminal shares that
// terminal with what it starts, which then needs it (sbx run, ssh, an installer), so nothing is hidden there.
//
// A test holds every other package to it: os/exec's Command and CommandContext are used here alone.
package proc

import (
	"context"
	"os/exec"
)

// Command is exec.Command, its window hidden on Windows when vibe has no console (see the package's doc).
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	return cmd
}

// CommandContext is exec.CommandContext, its window hidden on Windows when vibe has no console (see the package's doc).
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	return cmd
}
