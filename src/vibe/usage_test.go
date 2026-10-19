package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vibe/internal/companion"
	"vibe/internal/state"
)

func procOutput(busy, total int) string {
	return "cpu  " + itoa(busy) + " 0 0 " + itoa(total-busy) + " 0 0 0 0 0 0\ncpu0 1 0 0 1 0 0 0 0 0 0\ncpu1 1 0 0 1 0 0 0 0 0 0\n" +
		"MemTotal:        8192000 kB\nMemAvailable:    6144000 kB\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

type loadStandIn struct {
	running, ok bool
	out         string
	err         error
	reads       int
}

func standInForTheSandbox(t *testing.T) *loadStandIn {
	t.Helper()
	f := &loadStandIn{running: true, ok: true}
	oldRunning, oldRead := sandboxRunning, readSandboxLoad
	sandboxRunning = func(string) (bool, bool) { return f.running, f.ok }
	readSandboxLoad = func(context.Context, string) (string, error) { f.reads++; return f.out, f.err }
	t.Cleanup(func() { sandboxRunning, readSandboxLoad = oldRunning, oldRead })
	return f
}

func TestTheSandboxsLoadIsReadFromInsideItAndLabelledWithWhatItWasGiven(t *testing.T) {
	sandbox := standInForTheSandbox(t)
	lay := updateLayout(t)
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p", Memory: "8g", CPUs: 2}); err != nil {
		t.Fatal(err)
	}
	read := usageFor(lay.Home, "box")

	sandbox.out = procOutput(100, 1000)
	first, err := read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.CPU != nil {
		t.Errorf("a CPU figure from the first sample: %v", *first.CPU)
	}
	if first.Cores != 2 || first.CPUSetting != 2 || first.MemSetting != "8g" || first.MemTotal != 8192000*1024 || first.MemUsed != (8192000-6144000)*1024 {
		t.Errorf("first = %+v", first)
	}

	sandbox.out = procOutput(600, 2000) // 500 of 1000 ticks busy
	second, err := read(context.Background())
	if err != nil || second.CPU == nil || *second.CPU != 50 {
		t.Errorf("second = %+v (%v), want 50%% busy", second, err)
	}
}

func TestASandboxThatIsntRunningIsNotReadOrWoken(t *testing.T) {
	sandbox := standInForTheSandbox(t)
	sandbox.running = false
	if _, err := usageFor(t.TempDir(), "box")(context.Background()); !errors.Is(err, companion.ErrNotRunning) {
		t.Errorf("err = %v, want ErrNotRunning", err)
	}
	if sandbox.reads != 0 {
		t.Errorf("a stopped sandbox was exec'd into %d times", sandbox.reads)
	}
}

// If sbx can't say whether it's running, that isn't "stopped".
func TestNotKnowingWhetherItIsRunningIsAnErrorNotAStoppedSandbox(t *testing.T) {
	sandbox := standInForTheSandbox(t)
	sandbox.ok = false
	_, err := usageFor(t.TempDir(), "box")(context.Background())
	if err == nil || errors.Is(err, companion.ErrNotRunning) {
		t.Errorf("err = %v, want a failure to ask sbx", err)
	}
}

func TestAFailedReadSaysSoInTheUsersTerms(t *testing.T) {
	sandbox := standInForTheSandbox(t)
	sandbox.err = errors.New("exec: timed out")
	_, err := usageFor(t.TempDir(), "box")(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timed out") || errors.Is(err, companion.ErrNotRunning) {
		t.Errorf("err = %v", err)
	}
}

// A sandbox started again counts its ticks afresh, and after being stopped the
// first sample is the first again: no CPU figure from before it stopped.
func TestACPUFigureNeverSpansAStop(t *testing.T) {
	sandbox := standInForTheSandbox(t)
	read := usageFor(t.TempDir(), "box")
	sandbox.out = procOutput(100, 1000)
	read(context.Background())
	sandbox.running = false
	read(context.Background())
	sandbox.running = true
	sandbox.out = procOutput(1100, 2000) // would read as 100% against before the stop
	r, err := read(context.Background())
	if err != nil || r.CPU != nil {
		t.Errorf("CPU %v (%v) from samples either side of a stop", r.CPU, err)
	}
}
