// Package running keeps track of the vibe processes that have a folder open,
// so a second vibe for the same folder can say so before two agents start
// taking instructions against one workspace.
//
// Each process holds a lock on its own file under ~/.vibe/running for as
// long as it lives. The OS drops the lock when the process dies, however it
// dies, so a file whose lock can be taken is stale — no PID-reuse guesswork.
//
// Asking another vibe to stop is done with a file too, rather than a signal:
// Windows has no SIGTERM, and a request file works the same everywhere.
package running

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dir returns the directory the per-process lock files live in.
func Dir(vibeHome string) string {
	return filepath.Join(vibeHome, "running")
}

func lockPath(vibeHome string, pid int) string {
	return filepath.Join(Dir(vibeHome), strconv.Itoa(pid)+".lock")
}

func stopPath(vibeHome string, pid int) string {
	return filepath.Join(Dir(vibeHome), strconv.Itoa(pid)+".stop")
}

// Session is this process's claim on a folder. The file must stay open for
// the claim to hold, so keep the Session until the process exits.
type Session struct {
	file     *os.File
	stopFile string
	stop     chan struct{}
	done     chan struct{}
}

// stopPollInterval is how often a Session looks for a stop request.
const stopPollInterval = 250 * time.Millisecond

// Register claims target for this process.
func Register(vibeHome, target string) (*Session, error) {
	return register(vibeHome, target, os.Getpid())
}

func register(vibeHome, target string, pid int) (*Session, error) {
	if err := os.MkdirAll(Dir(vibeHome), 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", Dir(vibeHome), err)
	}
	// A request left over from an earlier process with this PID is not
	// meant for this one.
	os.Remove(stopPath(vibeHome, pid))
	f, err := os.OpenFile(lockPath(vibeHome, pid), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("locking %s: %w", f.Name(), err)
	}
	if _, err := f.WriteString(target + "\n"); err != nil {
		f.Close()
		return nil, err
	}
	s := &Session{
		file:     f,
		stopFile: stopPath(vibeHome, pid),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go s.watch()
	return s, nil
}

func (s *Session) watch() {
	ticker := time.NewTicker(stopPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if _, err := os.Stat(s.stopFile); err == nil {
				close(s.stop)
				return
			}
		}
	}
}

// StopRequested is closed when another vibe asks this one to stop. A nil
// Session never is.
func (s *Session) StopRequested() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.stop
}

// Release gives up the claim and removes its files. Calling it more than
// once, or on a nil Session, is harmless.
func (s *Session) Release() {
	if s == nil || s.file == nil {
		return
	}
	close(s.done)
	name := s.file.Name()
	// Closed before removal: Windows won't delete a file that is open.
	s.file.Close()
	s.file = nil
	os.Remove(name)
	os.Remove(s.stopFile)
}

// held reports whether a live process still holds the lock file at path,
// removing the file when it turns out to be stale.
func held(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	if lock(f) != nil {
		f.Close()
		return true
	}
	f.Close()
	os.Remove(path)
	os.Remove(strings.TrimSuffix(path, ".lock") + ".stop")
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

// killWait is how long Stop gives a killed process to be gone.
const killWait = 5 * time.Second

// Stop asks the vibe process pid to end its session — which it does by
// ending its sbx session, saving its agent's memories and exiting — and
// waits up to timeout for it to be gone. One that doesn't go in time is
// killed outright, and killed reports that it came to that.
func Stop(vibeHome string, pid int, timeout time.Duration) (killed bool, err error) {
	path := lockPath(vibeHome, pid)
	if !held(path) {
		return false, nil
	}
	if err := os.WriteFile(stopPath(vibeHome, pid), nil, 0o644); err != nil {
		return false, fmt.Errorf("asking PID %d to stop: %w", pid, err)
	}
	if waitGone(path, timeout) {
		return false, nil
	}
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
	if waitGone(path, killWait) {
		return true, nil
	}
	return true, fmt.Errorf("PID %d is still running, even after being killed", pid)
}

func waitGone(path string, timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); ; {
		if !held(path) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}
