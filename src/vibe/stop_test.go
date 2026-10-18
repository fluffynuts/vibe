package main

import (
	"errors"
	"strings"
	"testing"

	"vibe/internal/running"
)

// fakeStopper is sbx as --stop sees it: whether the sandbox is there and
// running, and what stopping it does.
type fakeStopper struct {
	there, running, known bool
	existsErr             error
	code                  int
	stopsAnyway           bool // a failed stop still leaves it stopped
	stopped               int
}

func (f *fakeStopper) stopper() stopper {
	return stopper{
		exists:  func(string) (bool, error) { return f.there, f.existsErr },
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

// 'vibe --stop' ends every vibe attached to the sandbox, terminal or
// headless (each saves its agent's memories as it ends), but not one only
// holding the folder for a rebuild.
func TestStoppingEndsEveryAttachedVibe(t *testing.T) {
	var ended []int
	old := stopVibe
	t.Cleanup(func() { stopVibe = old })
	stopVibe = func(_ string, pid int) (bool, error) { ended = append(ended, pid); return false, nil }
	err := endVibes(t.TempDir(), []running.Claim{{PID: 10}, {PID: 11, Headless: true}, {PID: 12, Work: "rebuilding sandbox 'a'"}}, "the sandbox was left running")
	if err != nil || len(ended) != 2 || ended[0] != 10 || ended[1] != 11 {
		t.Errorf("endVibes = %v, ended %v; want the terminal and the headless one", err, ended)
	}
	stopVibe = func(string, int) (bool, error) { return false, errors.New("no such process") }
	if err := endVibes(t.TempDir(), []running.Claim{{PID: 10}}, "the sandbox was left running"); err == nil || !strings.Contains(err.Error(), "left running") {
		t.Errorf("when it can't be ended: %v", err)
	}
}

func TestStopSaysWhenSbxCannotTellWhetherTheSandboxIsThere(t *testing.T) {
	unsure := &fakeStopper{existsErr: errors.New("sbx ls --json: daemon not running")}
	for _, force := range []bool{false, true} {
		if _, err := stopSandbox(unsure.stopper(), "box", "/w", force); err == nil || !strings.Contains(err.Error(), "daemon not running") {
			t.Errorf("force %v: err = %v, want sbx's reason", force, err)
		}
	}
	if unsure.stopped != 0 {
		t.Error("asked sbx to stop a sandbox it couldn't say was there")
	}
}
