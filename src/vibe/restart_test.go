package main

import (
	"errors"
	"strings"
	"testing"

	"vibe/internal/running"
)

// restartStandIns stands in for the other vibe's page, the user and ending
// vibes, recording which were ended and whether the user was asked.
func restartStandIns(t *testing.T, answering, known, terminal, yes bool) (ended *[]int, asked *bool) {
	t.Helper()
	ended, asked = new([]int), new(bool)
	oldAnswering, oldConfirm, oldStop, oldAsk := agentAnswering, confirmRestart, stopVibe, canAsk
	t.Cleanup(func() { agentAnswering, confirmRestart, stopVibe, canAsk = oldAnswering, oldConfirm, oldStop, oldAsk })
	agentAnswering = func(string, string) (bool, bool) { return answering, known }
	confirmRestart = func() bool { *asked = true; return yes }
	stopVibe = func(_ string, pid int) (bool, error) { *ended = append(*ended, pid); return false, nil }
	canAsk = func() bool { return terminal }
	return ended, asked
}

func TestRestartingAnIdleHeadlessVibeDoesntAsk(t *testing.T) {
	ended, asked := restartStandIns(t, false, true, true, false)
	if err := restart(t.TempDir(), "box", "/work", []running.Claim{{PID: 7, Headless: true}}, false); err != nil {
		t.Fatal(err)
	}
	if *asked || len(*ended) != 1 || (*ended)[0] != 7 {
		t.Errorf("asked %v, ended %v; want it ended unasked", *asked, *ended)
	}
}

// Cutting off an agent's turn, or another terminal's session, is asked
// about first; saying no ends nothing.
func TestRestartingAsksBeforeInterruptingAnything(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claims []running.Claim
		busy   bool
	}{
		{"an agent part-way through a turn", []running.Claim{{PID: 7, Headless: true}}, true},
		{"a terminal session", []running.Claim{{PID: 8}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ended, asked := restartStandIns(t, tc.busy, true, true, false)
			err := restart(t.TempDir(), "box", "/work", tc.claims, false)
			if !*asked || len(*ended) != 0 || err == nil || !strings.Contains(err.Error(), "nothing restarted") {
				t.Errorf("asked %v, ended %v, err %v; want asked, and nothing ended on a no", *asked, *ended, err)
			}
			ended, _ = restartStandIns(t, tc.busy, true, true, true)
			if err := restart(t.TempDir(), "box", "/work", tc.claims, false); err != nil || len(*ended) != 1 {
				t.Errorf("on a yes: ended %v, err %v", *ended, err)
			}
		})
	}
}

func TestForceRestartsWithoutAsking(t *testing.T) {
	ended, asked := restartStandIns(t, true, true, false, false)
	if err := restart(t.TempDir(), "box", "/work", []running.Claim{{PID: 8}, {PID: 9, Headless: true}}, true); err != nil {
		t.Fatal(err)
	}
	if *asked || len(*ended) != 2 {
		t.Errorf("asked %v, ended %v; want both ended unasked", *asked, *ended)
	}
}

// With nobody to ask, what would be interrupted is said, and nothing is.
func TestRestartingWithNoTerminalRefusesToInterrupt(t *testing.T) {
	ended, _ := restartStandIns(t, false, false, false, true)
	err := restart(t.TempDir(), "box", "/work", []running.Claim{{PID: 7, Headless: true}}, false)
	if err == nil || !strings.Contains(err.Error(), "couldn't be asked") || !strings.Contains(err.Error(), "-f") || len(*ended) != 0 {
		t.Errorf("err %v, ended %v", err, *ended)
	}
}

func TestAFolderBeingRebuiltIsntRestarted(t *testing.T) {
	ended, _ := restartStandIns(t, false, true, true, true)
	err := restart(t.TempDir(), "box", "/work", []running.Claim{{PID: 7, Work: "rebuilding sandbox 'box'"}}, true)
	if err == nil || !strings.Contains(err.Error(), "rebuilding sandbox 'box'") || len(*ended) != 0 {
		t.Errorf("err %v, ended %v", err, *ended)
	}
}

func TestAVibeThatWontEndRestartsNothingAndSaysSo(t *testing.T) {
	restartStandIns(t, false, true, true, true)
	stopVibe = func(string, int) (bool, error) { return false, errors.New("no such process") }
	err := restart(t.TempDir(), "box", "/work", []running.Claim{{PID: 7, Headless: true}}, true)
	if err == nil || !strings.Contains(err.Error(), "PID 7") || !strings.Contains(err.Error(), "nothing was restarted") {
		t.Errorf("err = %v, want which vibe, and that nothing was restarted", err)
	}
}
