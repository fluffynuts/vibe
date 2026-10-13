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

func cancelPath(vibeHome string, pid int) string {
	return filepath.Join(Dir(vibeHome), strconv.Itoa(pid)+".cancel")
}

func answerPath(vibeHome string, pid int) string {
	return filepath.Join(Dir(vibeHome), strconv.Itoa(pid)+".cancelled")
}

func holdPath(vibeHome string, pid int) string {
	return filepath.Join(Dir(vibeHome), strconv.Itoa(pid)+".hold")
}

// Session is this process's claim on a folder. The file must stay open for
// the claim to hold, so keep the Session until the process exits.
type Session struct {
	file     *os.File
	stopFile string
	holdFile string
	// cancelFile is a request to cancel what the page has under way, which
	// is answered in answerFile; cancels passes each request on.
	cancelFile, answerFile string
	cancels                chan struct{}
	stop                   chan struct{}
	done                   chan struct{}
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
	os.Remove(holdPath(vibeHome, pid))
	os.Remove(cancelPath(vibeHome, pid))
	os.Remove(answerPath(vibeHome, pid))
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
		file:       f,
		stopFile:   stopPath(vibeHome, pid),
		holdFile:   holdPath(vibeHome, pid),
		cancelFile: cancelPath(vibeHome, pid),
		answerFile: answerPath(vibeHome, pid),
		cancels:    make(chan struct{}, 1),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
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
			if os.Remove(s.cancelFile) == nil {
				select {
				case s.cancels <- struct{}{}:
				default: // one is already waiting to be handled
				}
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
	os.Remove(s.holdFile)
	os.Remove(s.cancelFile)
	os.Remove(s.answerFile)
}

// CancelRequested delivers each request another vibe makes (AskToCancel)
// for this one to cancel what its companion page has under way. A nil
// Session never has any.
func (s *Session) CancelRequested() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.cancels
}

// AnswerCancel tells the vibe that asked what was cancelled.
func (s *Session) AnswerCancel(answer string) error {
	return os.WriteFile(s.answerFile, []byte(answer), 0o644)
}

// AskToCancel asks the vibe with pid to cancel what its companion page has
// under way (the agent's turn, an Admin action), and returns its answer.
// within is a backstop: answering takes a moment, so a vibe that hasn't
// answered by then is stuck, and that is the error.
func AskToCancel(vibeHome string, pid int, within time.Duration) (string, error) {
	if !held(lockPath(vibeHome, pid)) {
		return "", fmt.Errorf("vibe (PID %d) isn't running any more", pid)
	}
	answer := answerPath(vibeHome, pid)
	os.Remove(answer)
	if err := os.WriteFile(cancelPath(vibeHome, pid), nil, 0o644); err != nil {
		return "", fmt.Errorf("asking vibe (PID %d) to cancel: %w", pid, err)
	}
	for deadline := time.Now().Add(within); ; {
		if data, err := os.ReadFile(answer); err == nil {
			os.Remove(answer)
			return string(data), nil
		}
		if time.Now().After(deadline) {
			os.Remove(cancelPath(vibeHome, pid))
			return "", fmt.Errorf("vibe (PID %d) didn't answer within %s — it may be stuck", pid, within)
		}
		time.Sleep(stopPollInterval)
	}
}

// AskToHold tells the vibe with pid that its sandbox is about to be stopped
// on purpose, from outside it ('vibe --stop'): when its session ends, that
// is why, and it should keep its companion page up rather than end.
func AskToHold(vibeHome string, pid int) error {
	return os.WriteFile(holdPath(vibeHome, pid), nil, 0o644)
}

// CancelHold takes AskToHold back, for a stop that didn't happen.
func CancelHold(vibeHome string, pid int) {
	os.Remove(holdPath(vibeHome, pid))
}

// HoldAsked says whether another vibe has asked this one to hold (see
// AskToHold), and takes the request: it answers for one session ending.
func (s *Session) HoldAsked() bool {
	if s == nil || s.holdFile == "" {
		return false
	}
	return os.Remove(s.holdFile) == nil
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
	os.Remove(strings.TrimSuffix(path, ".lock") + ".hold")
	os.Remove(strings.TrimSuffix(path, ".lock") + ".cancel")
	os.Remove(strings.TrimSuffix(path, ".lock") + ".cancelled")
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
