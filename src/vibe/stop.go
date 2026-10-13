package main

import (
	"fmt"
	"os"

	"vibe/internal/running"
	"vibe/internal/sbxrun"
)

func doStop(vibeHome, name, target string, force bool) error {
	s := sbxStopper
	// A vibe attached to this sandbox would take its session ending for
	// the agent being quit: tell it the stop is on purpose, so it keeps its
	// companion page up to start the sandbox again from.
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
	code, err := stopSandbox(s, name, target, force)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

// stopper is what --stop asks of sbx: sbxStopper, or a stub under test.
type stopper struct {
	exists func(name string) bool
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
	if !s.exists(name) {
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
	stopped := !s.exists(name) || (known && !running)
	if !stopped && s.unhold != nil {
		s.unhold()
	}
	if force && stopped {
		note("sbx couldn't stop '%s', but it isn't running", name)
		return 0, nil
	}
	return code, err
}
