package main

import (
	"fmt"
	"os"

	"vibe/internal/running"
	"vibe/internal/sbxrun"
)

func doStop(vibeHome, name, target string, force bool) error {
	if err := endHeadlessVibes(vibeHome, running.Claims(vibeHome, target)); err != nil {
		return err
	}
	code, err := stopSandbox(holdingStopper(vibeHome, target), name, target, force)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

// endHeadlessVibes ends each headless vibe among claims (saving its agent's
// memories as it ends), leaving the sandbox as it is: 'vibe --stop' then
// stops the sandbox, as nobody would see a headless vibe carry on without
// it, and --restart starts a fresh one. A vibe with a terminal is left to
// keep its page up, as its terminal says.
func endHeadlessVibes(vibeHome string, claims []running.Claim) error {
	for _, c := range claims {
		if !c.Headless {
			continue
		}
		note("ending headless vibe (PID %d) — it saves its agent's memories first", c.PID)
		killed, err := stopVibe(vibeHome, c.PID)
		switch {
		case err != nil:
			return fmt.Errorf("couldn't end headless vibe (PID %d), so the sandbox was left running: %w", c.PID, err)
		case killed:
			note("  it didn't end within %s and was killed, so its agent's memories may not have been saved", stopOtherTimeout)
		default:
			note("  it has ended")
		}
	}
	return nil
}

// holdingStopper is sbxStopper, telling a vibe attached to target that its
// session ending is on purpose: it would otherwise take that for the agent
// being quit, and end, rather than keep its companion page up to start the
// sandbox again from.
func holdingStopper(vibeHome, target string) stopper {
	s := sbxStopper
	attached := running.Others(vibeHome, target)
	s.hold = func() {
		for _, pid := range attached {
			running.AskToHold(vibeHome, pid)
		}
	}
	s.unhold = func() {
		for _, pid := range attached {
			running.CancelHold(vibeHome, pid)
		}
	}
	return s
}

// stopper is what --stop asks of sbx: sbxStopper, or a stub under test.
type stopper struct {
	exists func(name string) (bool, error)
	// running says whether the sandbox is running, and known whether sbx
	// could say.
	running func(name string) (running, known bool)
	stop    func(name string) (int, error)
	// hold tells whatever is attached to the sandbox, just before it is
	// stopped, that the stop is on purpose; unhold takes that back when the
	// sandbox is still running after all. Either may be nil.
	hold, unhold func()
}

var sbxStopper = stopper{
	exists: sbxrun.Exists,
	running: func(name string) (bool, bool) {
		statuses, err := sbxrun.List()
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
	if s.hold != nil {
		s.hold()
	}
	code, err := s.stop(name)
	if err == nil && code == 0 {
		return 0, nil
	}
	running, known := s.running(name)
	stillThere, existsErr := s.exists(name)
	stopped := (existsErr == nil && !stillThere) || (known && !running)
	if !stopped && s.unhold != nil {
		s.unhold()
	}
	if force && stopped {
		note("sbx couldn't stop '%s', but it isn't running", name)
		return 0, nil
	}
	return code, err
}
