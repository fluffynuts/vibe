package main

import (
	"context"
	"fmt"
	"os"

	"vibe/internal/running"
	"vibe/internal/sbxrun"
)

func doStop(vibeHome, name, target string, force bool) error {
	if err := endVibes(vibeHome, running.Claims(vibeHome, target), "the sandbox was left running"); err != nil {
		return err
	}
	code, err := stopSandbox(sbxStopper, name, target, force)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

// endVibes ends each vibe among claims, terminal or headless, saving its
// agent's memories as it ends (waking a sandbox that has stopped under it
// to), before its sandbox is stopped or it is restarted: stopping is for
// the whole workspace. One only holding the folder for a rebuild is left to
// finish. One that can't be ended is an error, saying what that leaves as
// it is: unlessEnded ("the sandbox was left running").
func endVibes(vibeHome string, claims []running.Claim, unlessEnded string) error {
	for _, c := range sessions(claims) {
		note("ending vibe (PID %d) — it saves its agent's memories first", c.PID)
		killed, err := stopVibe(vibeHome, c.PID)
		switch {
		case err != nil:
			return fmt.Errorf("couldn't end vibe (PID %d), so %s: %w", c.PID, unlessEnded, err)
		case killed:
			note("  it went %s without getting any further and was killed, so its agent's memories may not have been saved", stopOtherQuiet)
		default:
			note("  it has ended")
		}
	}
	return nil
}

// sessions is claims without those only holding a folder for a rebuild:
// the vibes attached to its sandbox.
func sessions(claims []running.Claim) []running.Claim {
	var out []running.Claim
	for _, c := range claims {
		if c.Work == "" {
			out = append(out, c)
		}
	}
	return out
}

// stopper is what --stop asks of sbx: sbxStopper, or a stub under test.
type stopper struct {
	exists func(name string) (bool, error)
	// running says whether the sandbox is running, and known whether sbx
	// could say.
	running func(name string) (running, known bool)
	stop    func(name string) (int, error)
}

var sbxStopper = stopper{
	exists: sbxrun.Exists,
	running: func(name string) (bool, bool) {
		statuses, err := sbxrun.List(context.Background())
		if err != nil {
			return false, false
		}
		for _, st := range statuses {
			if st.Name == name {
				return st.Running, true
			}
		}
		return false, true
	},
	stop: sbxrun.Stop,
}

// stopSandbox is --stop, returning sbx's exit code. A sandbox that isn't
// running is already what was asked for. With force, so is one that isn't
// there at all, or that sbx failed to stop but has stopped anyway (as one
// half-made by an interrupted vibe can).
func stopSandbox(s stopper, name, target string, force bool) (int, error) {
	exists, err := s.exists(name)
	if err != nil {
		return 0, err
	}
	if !exists {
		if force {
			note("no sandbox named '%s' for %s — nothing to stop", name, target)
			return 0, nil
		}
		return 0, fmt.Errorf("no sandbox named '%s' for %s — nothing to stop", name, target)
	}
	if running, known := s.running(name); known && !running {
		note("sandbox '%s' isn't running — nothing to stop", name)
		return 0, nil
	}
	note("stopping sandbox '%s'", name)
	code, err := s.stop(name)
	if err == nil && code == 0 {
		return 0, nil
	}
	running, known := s.running(name)
	stillThere, existsErr := s.exists(name)
	stopped := (existsErr == nil && !stillThere) || (known && !running)
	if force && stopped {
		note("sbx couldn't stop '%s', but it isn't running", name)
		return 0, nil
	}
	return code, err
}
