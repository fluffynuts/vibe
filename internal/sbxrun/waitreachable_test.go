package sbxrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vibe/internal/sbxtest"
)

// How long to wait for a sandbox is the caller's to say, as a duration (the memory restore, the start nudge and Start each
// want their own), and one wait does it for all: it is bounded by the clock, not by a number of probes, so a probe that takes
// long doesn't stretch it. A probe is begun until the time is up, and never after.

// probesTo is an sbx whose exec fails after takes seconds (a probe that is slow), noting each in the file it returns.
func probesTo(t *testing.T, takes string) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "probes")
	sbxtest.OnPath(t, "case \"$1\" in\n  exec) echo probe >>'"+log+"'; sleep "+takes+"; echo 'still starting' >&2; exit 1 ;;\nesac\n")
	return log
}

func probeCount(t *testing.T, log string) int {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "probe")
}

func TestAWaitLastsAsLongAsItWasGivenWhateverTheProbesTake(t *testing.T) {
	probesTo(t, "0.1")
	began := time.Now()
	if WaitReachable(context.Background(), "box", 2500*time.Millisecond) {
		t.Fatal("a sandbox that never answered was waited for and reported reachable")
	}
	took := time.Since(began)
	if took < 2500*time.Millisecond {
		t.Errorf("gave up after %s, with 2.5 s given", took)
	}
	if took > 8*time.Second {
		t.Errorf("waited %s, with 2.5 s given: a slow probe stretched it", took)
	}
}

func TestAWaitGivenNoTimeTriesOnce(t *testing.T) {
	log := probesTo(t, "0")
	if WaitReachable(context.Background(), "box", 0) {
		t.Fatal("reachable")
	}
	if n := probeCount(t, log); n != 1 {
		t.Errorf("%d probes, want the one every wait makes", n)
	}
}

func TestAWaitThatIsEndedByItsContextBeginsNoMoreProbes(t *testing.T) {
	log := probesTo(t, "0.2")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if WaitReachable(ctx, "box", time.Minute) {
		t.Fatal("reachable")
	}
	if n := probeCount(t, log); n > 2 {
		t.Errorf("%d probes after its context had ended", n)
	}
}
