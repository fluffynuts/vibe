// Package running keeps track of the vibe processes that have a folder open,
// so a second vibe for the same folder can say so before two agents start
// taking instructions against one workspace.
//
// Each process holds an flock on its own file under ~/.vibe/running for as
// long as it lives. The kernel drops the lock when the process dies, however
// it dies, so a file whose lock can be taken is stale — no PID-reuse guesswork.
package running

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Dir returns the directory the per-process lock files live in.
func Dir(vibeHome string) string {
	return filepath.Join(vibeHome, "running")
}

func lockPath(vibeHome string, pid int) string {
	return filepath.Join(Dir(vibeHome), strconv.Itoa(pid)+".lock")
}

// Session is this process's claim on a folder. The file must stay open for
// the claim to hold, so keep the Session until the process exits.
type Session struct {
	file *os.File
}

// Register claims target for this process.
func Register(vibeHome, target string) (*Session, error) {
	return register(vibeHome, target, os.Getpid())
}

func register(vibeHome, target string, pid int) (*Session, error) {
	if err := os.MkdirAll(Dir(vibeHome), 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", Dir(vibeHome), err)
	}
	f, err := os.OpenFile(lockPath(vibeHome, pid), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("locking %s: %w", f.Name(), err)
	}
	if _, err := f.WriteString(target + "\n"); err != nil {
		f.Close()
		return nil, err
	}
	return &Session{file: f}, nil
}

// Release gives up the claim and removes its file.
func (s *Session) Release() {
	if s == nil || s.file == nil {
		return
	}
	os.Remove(s.file.Name())
	s.file.Close()
	s.file = nil
}

// held reports whether a live process still holds the lock file at path,
// removing the file when it turns out to be stale.
func held(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return true
	}
	os.Remove(path)
	return false
}

// Others returns the PIDs of every other live vibe process that has target
// open, lowest first.
func Others(vibeHome, target string) []int {
	entries, err := os.ReadDir(Dir(vibeHome))
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".lock"))
		if err != nil || pid == self || !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		path := filepath.Join(Dir(vibeHome), e.Name())
		data, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(data)) != target {
			continue
		}
		if held(path) {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids
}

// Stop asks the vibe process pid to end its session (SIGTERM, which vibe
// passes on to its sbx session before saving memories and exiting) and waits
// up to timeout for it to be gone.
func Stop(vibeHome string, pid int, timeout time.Duration) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("signalling PID %d: %w", pid, err)
	}
	path := lockPath(vibeHome, pid)
	for deadline := time.Now().Add(timeout); ; {
		if !held(path) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("PID %d is still running after %s", pid, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
