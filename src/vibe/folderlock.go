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

// stopOtherQuiet is how long another vibe asked to stop may go without
// reporting a step of winding down (see running.Stop) before it's killed:
// longer than any one step takes, the longest being waking a sandbox that
// sbx has stopped, to save its agent's memories.
const stopOtherQuiet = 90 * time.Second

// guardFolder checks for other vibe processes with target open — two agents
// taking instructions against one workspace is easy to do by accident — and
// asks what to do if there are any. It then claims target for this process.
// With force it says so and carries on; with no terminal to ask on, it
// refuses. A terminal attaching (rather than rebuilding or deleting) where
// only headless vibes hold target goes alongside them without asking: they
// serve the page, and this attaches the terminal the user came for.
// destroying says the sandbox is about to be removed or rebuilt, which
// whatever else is using it can't go on beside.
func guardFolder(vibeHome, target string, force, attaching, destroying bool) error {
	if claims := running.Claims(vibeHome, target); len(claims) > 0 {
		if pids := alongsideHeadless(claims, attaching); pids != nil {
			headlessHere = pids
			note("attaching alongside the headless vibe (%s), which carries on serving the companion page", pidList(pids))
		} else if err := resolveOthers(vibeHome, target, claims, force, destroying); err != nil {
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

// headlessHere is the PIDs of the headless vibes this one is attached
// alongside, if any (see guardFolder).
var headlessHere []int

// alongsideHeadless is the PIDs of the vibes holding the folder, when a
// terminal attaching can go alongside them: every one is a headless vibe.
func alongsideHeadless(claims []running.Claim, attaching bool) []int {
	if !attaching || len(claims) == 0 {
		return nil
	}
	var pids []int
	for _, c := range claims {
		if !c.Headless || c.Work != "" {
			return nil
		}
		pids = append(pids, c.PID)
	}
	return pids
}

func pidList(pids []int) string {
	which := "PID"
	if len(pids) > 1 {
		which = "PIDs"
	}
	strs := make([]string, len(pids))
	for i, p := range pids {
		strs[i] = fmt.Sprint(p)
	}
	return which + " " + strings.Join(strs, ", ")
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

// whichVibes names the vibes holding the folder, and whether any of them is
// busy with work (a rebuild from the Profiles page, or --update-sandboxes).
func whichVibes(claims []running.Claim) (which string, pids []string, busy bool) {
	seen := map[int]bool{}
	for _, c := range claims {
		if !seen[c.PID] {
			seen[c.PID] = true
			pids = append(pids, fmt.Sprint(c.PID))
		}
		if c.Work != "" {
			busy = true
		}
	}
	which = "vibe (PID " + strings.Join(pids, ", ") + ")"
	if len(pids) > 1 {
		which = "those vibes (PIDs " + strings.Join(pids, ", ") + ")"
	}
	return which, pids, busy
}

// othersSay is what to tell the user about the claims other vibes have on
// target: why the folder is taken, and, for the choice to stop them, what
// stopping them ends. A vibe holding it for work (a rebuild from the
// Profiles page, or --update-sandboxes) says what it's doing, as stopping it
// ends that part-way, and that vibe's own session with it.
func othersSay(target string, claims []running.Claim) (why []string, stop string, busy bool) {
	var sessions []string
	seen := map[int]bool{}
	for _, c := range claims {
		if c.Work == "" {
			if !seen[c.PID] {
				seen[c.PID] = true
				sessions = append(sessions, fmt.Sprint(c.PID))
			}
			continue
		}
		why = append(why, fmt.Sprintf("vibe (PID %d) is %s for %s — the Profiles page it was started from, or the terminal running --update-sandboxes, shows how it's going", c.PID, c.Work, target))
	}
	if len(sessions) > 0 {
		which := "PID"
		if len(sessions) > 1 {
			which = "PIDs"
		}
		why = append([]string{fmt.Sprintf("vibe is already running for %s (%s %s)", target, which, strings.Join(sessions, ", "))}, why...)
	}
	which, pids, busy := whichVibes(claims)
	saves := "it saves its agent's memories first"
	if len(pids) > 1 {
		saves = "they save their agents' memories first"
	}
	stop = "stop " + which + " — " + saves + " — then continue"
	if busy {
		stop = "stop " + which + ", ending its work part-way (a rebuild stopped part-way can leave the sandbox half-made), then continue"
	}
	return why, stop, busy
}

// folderChoice is what a menu entry does about the other vibes.
type folderChoice int

const (
	chooseExit folderChoice = iota
	chooseContinue
	chooseStop
)

// folderMenu is what the user is offered when other vibes hold the folder.
// Carrying on beside them isn't offered when the sandbox is about to be
// rebuilt or removed: it would go from under them.
func folderMenu(claims []running.Claim, destroying bool) (menu struct {
	question string
	labels   []string
	choices  []folderChoice
}) {
	which, _, busy := whichVibes(claims)
	_, stop, _ := othersSay("", claims)
	exit := "exit"
	menu.question = "WARNING: vibe is already running in this folder! What would you like to do?"
	if busy {
		exit = "exit, and run vibe again once that's done"
		menu.question = "WARNING: another vibe is working on this folder's sandbox! What would you like to do?"
	}
	if destroying {
		menu.question = strings.TrimSuffix(menu.question, " What would you like to do?") + " This would take its sandbox away. What would you like to do?"
	}
	menu.labels, menu.choices = []string{exit}, []folderChoice{chooseExit}
	if !destroying {
		menu.labels = append(menu.labels, "continue anyway, leaving "+which+" running beside this one")
		menu.choices = append(menu.choices, chooseContinue)
	}
	menu.labels = append(menu.labels, stop)
	menu.choices = append(menu.choices, chooseStop)
	return menu
}

func resolveOthers(vibeHome, target string, claims []running.Claim, force, destroying bool) error {
	why, _, busy := othersSay(target, claims)
	already := strings.Join(why, "; ")

	if force {
		if !destroying {
			note("WARNING: %s — continuing anyway (--force)", already)
			return nil
		}
		// Beside another vibe, the sandbox would go from under it: it's
		// stopped first, as the menu's third choice does. But not in the
		// middle of work, which -f is no reason to end part-way.
		if busy {
			return fmt.Errorf("%s — it's in the middle of work, which -f won't end part-way: wait for it, or run again without -f to choose", already)
		}
		note("WARNING: %s — stopping it first (--force), as this takes the sandbox away", already)
		return stopOthers(vibeHome, target)
	}
	if !interactive() {
		return fmt.Errorf("%s", already)
	}
	for _, line := range why {
		note("%s", line)
	}
	menu := folderMenu(claims, destroying)
	choice, ok := chooseFrom(menu.question, menu.labels, 0)
	switch {
	case !ok || menu.choices[choice] == chooseExit:
		return fmt.Errorf("already in use for %s — exiting", target)
	case menu.choices[choice] == chooseContinue:
		return nil
	}
	return stopOthers(vibeHome, target)
}

// stopOthers asks the other vibes holding target to stop, and waits for them.
func stopOthers(vibeHome, target string) error {
	others := running.Others(vibeHome, target)
	for _, pid := range others {
		status := newStatusLine()
		status.show("Stopping vibe (PID %d)", pid)
		killed, err := running.Stop(vibeHome, pid, stopOtherQuiet, func(step string) {
			status.done("Stopping vibe (PID %d): %s", pid, step)
			status.show("Stopping vibe (PID %d)", pid)
		})
		switch {
		case err != nil:
			status.done("Stopping vibe (PID %d) failed: %s", pid, err)
			return fmt.Errorf("could not stop the other vibe for %s", target)
		case killed:
			status.done("Killed vibe (PID %d): it went %s without getting any further, so its memories may not have been saved", pid, stopOtherQuiet)
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
