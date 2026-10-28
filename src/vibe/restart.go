package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"vibe/internal/running"
	"vibe/internal/state"
)

// agentAnswering asks the companion page of the vibe running sandbox name
// whether its agent is part-way through a turn. known is false when the page
// couldn't be asked (it isn't served, say). A variable for tests.
var agentAnswering = func(vibeHome, name string) (answering, known bool) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found || inst.CompanionPort == 0 {
		return false, false
	}
	token, err := state.CompanionToken(vibeHome, name)
	if err != nil {
		return false, false
	}
	// Asking for what comes after the end asks only how things stand.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/conversation?after=%d", inst.CompanionPort, 1<<31-1), nil)
	if err != nil {
		return false, false
	}
	req.Host = fmt.Sprintf("localhost:%d", inst.CompanionPort)
	req.Header.Set("X-Vibe-Token", token)
	client := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	res, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer res.Body.Close()
	var body struct {
		Running bool `json:"running"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&body) != nil {
		return false, false
	}
	return body.Running, true
}

// confirmRestart asks whether to restart after all, for what it would
// interrupt. A variable so tests can stand in for the user.
var confirmRestart = func() bool {
	return confirmDefault(false, false, "Restart anyway?")
}

// restartFolder is --restart's first half: whatever vibe has target, in a
// terminal or headless, is ended (saving its agent's memories), leaving the
// sandbox, for this one to take over (a new build of vibe, say, or a
// terminal on a headless sandbox). Ending one is asked about first when it
// interrupts something — another terminal's session, or an agent part-way
// through a turn — unless force; with no terminal to ask on, it's refused,
// saying why. A folder held for a rebuild is refused.
func restartFolder(vibeHome, name, target string, force bool) (runMode, error) {
	return restart(vibeHome, name, target, running.Claims(vibeHome, target), force)
}

// canAsk says whether there's a terminal to ask on. A variable for tests.
var canAsk = interactive

// restart is restartFolder, for the claims other vibes have on target. It
// says which mode the vibe it ended was in, for the new one to start in too.
func restart(vibeHome, name, target string, claims []running.Claim, force bool) (runMode, error) {
	if len(claims) == 0 {
		note("no vibe was running for %s — starting one", target)
		return modeNone, nil
	}
	interrupts, busy := interruptionsOf(vibeHome, name, target, claims)
	if busy != nil {
		return modeNone, fmt.Errorf("%w, then restart", busy)
	}
	if len(interrupts) > 0 && !force {
		if !canAsk() {
			return modeNone, fmt.Errorf("not restarting: %s — run it again with -f to restart anyway", strings.Join(interrupts, "; "))
		}
		for _, line := range interrupts {
			note("%s", line)
		}
		if !confirmRestart() {
			return modeNone, fmt.Errorf("aborted — nothing restarted")
		}
	}
	if err := endVibes(vibeHome, claims, "nothing was restarted"); err != nil {
		return modeNone, err
	}
	return modeOf(claims), nil
}

// interruptionsOf is what ending the vibes among claims, for target's sandbox
// name, would interrupt: another terminal's session, an agent part-way
// through a turn, or one that couldn't be asked. A folder held for a rebuild
// can't have its vibes ended: that is busy, saying what to do (not what to do
// after, which is for what asked: a restart, or a stop).
func interruptionsOf(vibeHome, name, target string, claims []running.Claim) (interrupts []string, busy error) {
	for _, c := range claims {
		switch {
		case c.Work != "":
			return nil, fmt.Errorf("vibe (PID %d) is %s for %s — wait for that, or cancel it where it was started", c.PID, c.Work, target)
		case !c.Headless:
			interrupts = append(interrupts, fmt.Sprintf("vibe (PID %d) has a terminal session in %s: ending it ends that session, and whatever its agent is doing", c.PID, target))
		default:
			if answering, known := agentAnswering(vibeHome, name); answering {
				interrupts = append(interrupts, fmt.Sprintf("the agent of the headless vibe (PID %d) is part-way through a turn: ending it cuts the turn off", c.PID))
			} else if !known {
				interrupts = append(interrupts, fmt.Sprintf("the headless vibe (PID %d) couldn't be asked whether its agent is part-way through a turn", c.PID))
			}
		}
	}
	return interrupts, nil
}
