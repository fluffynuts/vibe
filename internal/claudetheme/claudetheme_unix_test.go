//go:build !windows

package claudetheme

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The sandbox's settings are the agent's to make: one that never ends (a
// FIFO) gives up when the caller's time is up, rather than holding it.
func TestFromSandboxGivesUpWhenItsTimeIsUp(t *testing.T) {
	home := fakeSandbox(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)
	if err := syscall.Mkfifo(settings, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	began := time.Now()
	if _, ok := FromSandbox(ctx, "any"); ok {
		t.Error("a settings file that was never read was taken as an answer")
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("took %s, after its time was up at 500ms", took)
	}
}
