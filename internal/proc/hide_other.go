//go:build !windows

package proc

import "os/exec"

// hideWindow does nothing: only Windows opens a window for a program started without a console.
func hideWindow(*exec.Cmd) {}
