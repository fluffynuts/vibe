package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"vibe/internal/prompt"
	"vibe/internal/running"
)

// session is this process's claim on the folder it has open, if any; see
// guardFolder. It lives for the whole run, since dropping it drops the claim.
var session *running.Session

// stopOtherTimeout is how long to wait for another vibe to wind down after
// being asked to stop: long enough for it to save its agent's memories.
const stopOtherTimeout = 60 * time.Second

// guardFolder checks for other vibe processes with target open — two agents
// taking instructions against one workspace is easy to do by accident — and
// asks what to do if there are any. It then claims target for this process.
// With force, or no terminal to ask on, it says so and carries on.
func guardFolder(vibeHome, target string, force bool) error {
	if others := running.Others(vibeHome, target); len(others) > 0 {
		if err := resolveOthers(vibeHome, target, others, force); err != nil {
			return err
		}
	}
	s, err := running.Register(vibeHome, target)
	if err != nil {
		note("WARNING: could not record this vibe as running for %s: %s", target, err)
		return nil
	}
	session = s
	go exitOnStopRequest(s)
	return nil
}

// sessionMu guards inSession, which records whether finish has handed the
// terminal to sbx: from then on, a stop request is finish's to act on.
var (
	sessionMu sync.Mutex
	inSession bool
)

// exitOnStopRequest handles another vibe asking this one to stop before its
// session has started — at a prompt, say — by simply exiting. Once the
// session is running, finish ends it properly instead.
func exitOnStopRequest(s *running.Session) {
	<-s.StopRequested()
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if inSession {
		return
	}
	// A picker waiting on a keypress has the terminal in raw mode, with the
	// cursor at the end of its hint line: wipe it and put the terminal back
	// before saying why. Any other question has the cursor after it.
	if !prompt.Abort() {
		fmt.Fprintln(os.Stderr)
	}
	note("asked to stop by another vibe for this folder — exiting")
	s.Release()
	os.Exit(1)
}

func resolveOthers(vibeHome, target string, others []int, force bool) error {
	pids := make([]string, len(others))
	for i, pid := range others {
		pids[i] = fmt.Sprint(pid)
	}
	which, instance := "PID", "that instance"
	if len(others) > 1 {
		which, instance = "PIDs", "those instances"
	}
	already := fmt.Sprintf("vibe is already running for %s (%s %s)", target, which, strings.Join(pids, ", "))

	if force {
		note("WARNING: %s — continuing anyway (--force)", already)
		return nil
	}
	if !interactive() {
		return fmt.Errorf("%s", already)
	}
	note("%s", already)
	choice, ok := chooseFrom("WARNING: vibe is already running in this folder! What would you like to do?", []string{
		"exit",
		"continue anyway",
		"stop " + instance + ", then continue",
	}, 0)
	switch {
	case !ok || choice == 0:
		return fmt.Errorf("already running for %s — exiting", target)
	case choice == 1:
		return nil
	}
	for _, pid := range others {
		status := newStatusLine()
		status.show("Stopping vibe (PID %d)", pid)
		killed, err := running.Stop(vibeHome, pid, stopOtherTimeout)
		switch {
		case err != nil:
			status.done("Stopping vibe (PID %d) failed: %s", pid, err)
			return fmt.Errorf("could not stop the other vibe for %s", target)
		case killed:
			status.done("Killed vibe (PID %d): it did not stop within %s, so its memories may not have been saved", pid, stopOtherTimeout)
		default:
			status.done("Stopped vibe (PID %d)", pid)
		}
	}
	return nil
}

// stopAsked says another vibe for this folder has asked this one to stop.
func stopAsked() bool {
	select {
	case <-session.StopRequested():
		return true
	default:
		return false
	}
}
