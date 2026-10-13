package main

import (
	"testing"
)

// fakeStopper is sbx as --stop sees it: whether the sandbox is there and
// running, and what stopping it does.
type fakeStopper struct {
	there, running, known bool
	code                  int
	stopsAnyway           bool // a failed stop still leaves it stopped
	stopped               int
}

func (f *fakeStopper) stopper() stopper {
	return stopper{
		exists:  func(string) bool { return f.there },
		running: func(string) (bool, bool) { return f.running, f.known },
		stop: func(string) (int, error) {
			f.stopped++
			if f.code == 0 || f.stopsAnyway {
				f.running = false
			}
			return f.code, nil
		},
	}
}

func TestStopSucceedsWhenThereIsNothingToStopOnlyWithForce(t *testing.T) {
	gone := &fakeStopper{}
	if _, err := stopSandbox(gone.stopper(), "box", "/w", false); err == nil {
		t.Error("stopping a sandbox that isn't there didn't fail without -f")
	}
	if code, err := stopSandbox(gone.stopper(), "box", "/w", true); err != nil || code != 0 {
		t.Errorf("with -f: %d, %v", code, err)
	}
}

func TestStopLeavesAStoppedSandboxAlone(t *testing.T) {
	for _, force := range []bool{false, true} {
		f := &fakeStopper{there: true, known: true}
		if code, err := stopSandbox(f.stopper(), "box", "/w", force); err != nil || code != 0 || f.stopped != 0 {
			t.Errorf("force %v: %d, %v, stopped %d times", force, code, err, f.stopped)
		}
	}
}

func TestStopReportsAFailureUnlessForceFindsItStoppedAnyway(t *testing.T) {
	stuck := &fakeStopper{there: true, running: true, known: true, code: 1}
	if code, _ := stopSandbox(stuck.stopper(), "box", "/w", true); code != 1 {
		t.Errorf("a sandbox still running after a failed stop: exit %d, want 1", code)
	}
	half := &fakeStopper{there: true, running: true, known: true, code: 1, stopsAnyway: true}
	if code, _ := stopSandbox(half.stopper(), "box", "/w", false); code != 1 {
		t.Errorf("without -f, a failed stop: exit %d, want 1", code)
	}
	half = &fakeStopper{there: true, running: true, known: true, code: 1, stopsAnyway: true}
	if code, err := stopSandbox(half.stopper(), "box", "/w", true); code != 0 || err != nil {
		t.Errorf("with -f, a failed stop that stopped it anyway: %d, %v", code, err)
	}
}
