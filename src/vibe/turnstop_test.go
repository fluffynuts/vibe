//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"vibe/internal/companion"
)

// alive says process pid is still running (a zombie, reaped by nobody here,
// counts as gone).
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return err != nil || !strings.Contains(string(stat), ") Z ")
}

// A turn stopped from the page used to end only vibe's side of it (the sbx
// exec on the host): the agent in the sandbox carried on, and the next
// prompt started a second one on the same conversation. The turn's wrapper
// records where the agent is, so stopping it stops the agent and all it
// started.
func TestStoppingATurnStopsTheAgentAndWhatItStarted(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sandbox's shell scripts are run here as they run in it: on Linux")
	}
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep isn't installed")
	}
	pidFile := filepath.Join(t.TempDir(), "turn.pid")
	childFile := filepath.Join(t.TempDir(), "child.pid")
	// The stand-in agent starts a long command of its own, as a tool call
	// would, then works on: neither ends by itself within the test.
	agent := "sleep 300 & echo $! > " + shellQuote(childFile) + "; sleep 300"
	turn := exec.Command("sh", "-c", wrapTurn(agent, pidFile))
	if err := turn.Start(); err != nil {
		t.Fatal(err)
	}
	go turn.Wait() // reaped as it ends, so it doesn't linger as a zombie
	var wrapper, child int
	deadline := time.Now().Add(5 * time.Second)
	for (wrapper == 0 || child == 0) && time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			wrapper, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if data, err := os.ReadFile(childFile); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if wrapper == 0 || child == 0 {
		t.Fatal("the turn never recorded where it was")
	}
	t.Cleanup(func() { syscall.Kill(child, syscall.SIGKILL); syscall.Kill(wrapper, syscall.SIGKILL) })

	out, err := exec.Command("sh", "-c", stopTurnScript(pidFile)).CombinedOutput()
	if err != nil {
		t.Fatalf("stopping the turn: %v\n%s", err, out)
	}
	deadline = time.Now().Add(2 * time.Second)
	for (alive(wrapper) || alive(child)) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(wrapper) {
		t.Error("the agent is still running")
	}
	if alive(child) {
		t.Error("what the agent started is still running")
	}
	if _, err := os.Stat(pidFile); err == nil {
		t.Error("the turn's record is left behind")
	}
}

func TestStoppingATurnThatHasEndedDoesNothing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sandbox's shell scripts are run here as they run in it: on Linux")
	}
	pidFile := filepath.Join(t.TempDir(), "turn.pid")
	if out, err := exec.Command("sh", "-c", wrapTurn("true", pidFile)).CombinedOutput(); err != nil {
		t.Fatalf("the turn: %v\n%s", err, out)
	}
	if _, err := os.Stat(pidFile); err == nil {
		t.Error("a turn that ended left its record behind")
	}
	if out, err := exec.Command("sh", "-c", stopTurnScript(pidFile)).CombinedOutput(); err != nil || len(out) != 0 {
		t.Errorf("stopping it: %v, said %q", err, out)
	}
}

func TestATurnsAgentGetsItsPromptAndItsExitStatusCounts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sandbox's shell scripts are run here as they run in it: on Linux")
	}
	pidFile := filepath.Join(t.TempDir(), "turn.pid")
	cmd := exec.Command("sh", "-c", wrapTurn("cat; exit 3", pidFile))
	cmd.Stdin = strings.NewReader("the prompt")
	out, err := cmd.Output()
	if string(out) != "the prompt" {
		t.Errorf("the agent read %q", out)
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 3 {
		t.Errorf("exit: %v, want the agent's 3", err)
	}
}

// A stop that reaches the sandbox before the turn's shell has written where
// it is finds nothing to stop; the agent must not then start unseen.
func TestATurnStoppedBeforeItsShellRecordedItselfNeverStartsTheAgent(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "turn.pid")
	started := filepath.Join(t.TempDir(), "agent-started")
	if out, err := exec.Command("sh", "-c", stopTurnScript(pidFile)).CombinedOutput(); err != nil {
		t.Fatalf("stopping a turn not yet begun: %v\n%s", err, out)
	}
	turn := exec.Command("sh", "-c", wrapTurn("touch "+shellQuote(started)+"; sleep 300", pidFile))
	if err := turn.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- turn.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		turn.Process.Kill()
		t.Fatal("the turn went on after it had been stopped")
	}
	if _, err := os.Stat(started); err == nil {
		t.Error("the agent started after the turn was stopped")
	}
	for _, f := range []string{pidFile, pidFile + ".stop"} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s is left behind", f)
		}
	}
}

// When vibe can't confirm in the sandbox that a stopped turn's agent has
// ended, it says so: the page would otherwise report a clean stop of an
// agent that may still be running.
func TestAStopThatCouldNotBeConfirmedInTheSandboxSaysSo(t *testing.T) {
	dir := t.TempDir()
	fake := "#!/bin/sh\nfor a; do [ \"$a\" = -i ] && exec sleep 30; done\necho 'sandbox unreachable' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	err := converse("box", "/work", func() (turnChoice, error) { return turnChoice{}, nil })(ctx, "hi", "", func() {}, func([]byte) {})
	if !errors.Is(err, companion.ErrStopUnconfirmed) {
		t.Fatalf("error = %v, want one saying the stop could not be confirmed", err)
	}
	if !strings.Contains(err.Error(), "box") {
		t.Errorf("%q doesn't name the sandbox", err)
	}
}
