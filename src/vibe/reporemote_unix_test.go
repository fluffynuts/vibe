//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// The sandbox can write the repository's config: one that would keep git
// waiting (an include of a FIFO) mustn't keep vibe waiting.
func TestTheRepositoryIsReadWithoutWaitingOnWhatItsConfigNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs on Windows")
	}
	dir := gitRepoWithOrigin(t, "https://github.com/owner/repo.git")
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, ".git", "config")
	f, err := os.OpenFile(config, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("[include]\n\tpath = " + fifo + "\n")
	f.Close()

	done := make(chan struct{})
	go func() {
		repoOf(dir)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		// Unblock git, so the test binary can end.
		if w, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			w.Close()
		}
		t.Fatal("reading the repository's origin waited on a FIFO its config names")
	}
}
