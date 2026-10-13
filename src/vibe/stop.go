package main

import (
	"fmt"
	"os"

	"vibe/internal/sbxrun"
)

func doStop(name, target string, force bool) error {
	code, err := stopSandbox(sbxStopper, name, target, force)
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
	code, err := s.stop(name)
	if err == nil && code == 0 {
		return 0, nil
	}
	if force {
		if running, known := s.running(name); !s.exists(name) || (known && !running) {
			note("sbx couldn't stop '%s', but it isn't running", name)
			return 0, nil
		}
	}
	return code, err
}
