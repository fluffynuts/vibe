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
	if claims := running.Claims(vibeHome, target); len(claims) > 0 {
		if err := resolveOthers(vibeHome, target, claims, force); err != nil {
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
	// From the moment it has the folder, so 'vibe --cancel' is answered
	// even before there's a page (at a prompt, or while sbx creates the
	// sandbox) rather than taken for a vibe that's stuck.
	go answerCancels(s, cancelPage, nil)
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

// othersSay is what to tell the user about the claims other vibes have on
// target: why the folder is taken, and, for the choice to stop them, what
// stopping them ends. A vibe holding it for work (a rebuild from the
// Profiles page, or --update-sandboxes) says what it's doing, as stopping it
// ends that part-way, and that vibe's own session with it.
func othersSay(target string, claims []running.Claim) (why []string, stop string, busy bool) {
	var sessions, pids []string
	seen := map[int]bool{}
	for _, c := range claims {
		if !seen[c.PID] {
			seen[c.PID] = true
			pids = append(pids, fmt.Sprint(c.PID))
		}
		if c.Work == "" {
			sessions = append(sessions, fmt.Sprint(c.PID))
			continue
		}
		busy = true
		why = append(why, fmt.Sprintf("vibe (PID %d) is %s for %s — the Profiles page it was started from, or the terminal running --update-sandboxes, shows how it's going", c.PID, c.Work, target))
	}
	if len(sessions) > 0 {
		which := "PID"
		if len(sessions) > 1 {
			which = "PIDs"
		}
		why = append([]string{fmt.Sprintf("vibe is already running for %s (%s %s)", target, which, strings.Join(sessions, ", "))}, why...)
	}
	which := "vibe (PID " + strings.Join(pids, ", ") + ")"
	if len(pids) > 1 {
		which = "those vibes (PIDs " + strings.Join(pids, ", ") + ")"
	}
	stop = "stop " + which + ", then continue"
	if busy {
		stop = "stop " + which + ", ending its work part-way (a rebuild stopped part-way can leave the sandbox half-made), then continue"
	}
	return why, stop, busy
}

func resolveOthers(vibeHome, target string, claims []running.Claim, force bool) error {
	why, stopLabel, busy := othersSay(target, claims)
	already := strings.Join(why, "; ")

	if force {
		note("WARNING: %s — continuing anyway (--force)", already)
		return nil
	}
	if !interactive() {
		return fmt.Errorf("%s", already)
	}
	for _, line := range why {
		note("%s", line)
	}
	exit := "exit"
	question := "WARNING: vibe is already running in this folder! What would you like to do?"
	if busy {
		exit = "exit, and run vibe again once that's done"
		question = "WARNING: another vibe is working on this folder's sandbox! What would you like to do?"
	}
	choice, ok := chooseFrom(question, []string{exit, "continue anyway", stopLabel}, 0)
	switch {
	case !ok || choice == 0:
		return fmt.Errorf("already in use for %s — exiting", target)
	case choice == 1:
		return nil
	}
	others := running.Others(vibeHome, target)
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
