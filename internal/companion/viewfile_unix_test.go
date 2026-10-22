//go:build !windows

package companion

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestAPipeIsRefusedWithoutWaitingOnIt(t *testing.T) {
	ws, _ := workspaceWith(t, nil)
	if err := syscall.Mkfifo(filepath.Join(ws, "pipe.md"), 0o644); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() { _, err := readWorkspaceFile(ws, "pipe.md"); done <- err }()
	refusedAs(t, <-done, "notFile")
}
