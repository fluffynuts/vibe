package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// gitRepoWithOrigin makes a repository whose origin is remote.
func gitRepoWithOrigin(t *testing.T, remote string) string {
	t.Helper()
	need(t, "git")
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestTheRepositoryIsReadFromItsOrigin(t *testing.T) {
	dir := gitRepoWithOrigin(t, "git@github.com:owner/repo.git")
	repo := repoOf(dir)
	if repo == nil || repo.String() != "owner/repo" || !repo.SSH {
		t.Errorf("repoOf = %+v, want owner/repo over ssh", repo)
	}
	if repo := repoOf(t.TempDir()); repo != nil {
		t.Errorf("repoOf a folder that's no repository = %+v, want nil", repo)
	}
	if repo := repoOf(gitRepoWithOrigin(t, "https://example.com/o/r.git")); repo != nil {
		t.Errorf("repoOf a repository not on GitHub = %+v, want nil", repo)
	}
}

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
