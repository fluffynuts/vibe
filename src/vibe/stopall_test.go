package main

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStopSandboxesStopsInParallelAndReportsFailures(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	stop := func(name string) error {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		if name == "bad" {
			return errors.New("boom\nsecond line")
		}
		return nil
	}
	var out bytes.Buffer
	failed := stopSandboxes(&out, false, []string{"a", "bad", "c"}, stop)

	if len(failed) != 1 || failed[0] != "bad" {
		t.Errorf("failed = %v, want [bad]", failed)
	}
	if peak < 2 {
		t.Errorf("stops ran one at a time (peak %d in flight)", peak)
	}
	for _, want := range []string{"a    Stopping", "a    Stopped", "bad  FAILED: boom second line", "c    Stopped"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestStopSandboxesRedrawsInPlaceOnATerminal(t *testing.T) {
	var out bytes.Buffer
	stopSandboxes(&out, true, []string{"a", "b"}, func(string) error { return nil })
	s := out.String()
	if !strings.Contains(s, "\x1b[2A") {
		t.Errorf("expected the two rows to be redrawn in place:\n%q", s)
	}
	if !strings.HasSuffix(s, "b  Stopped\n") {
		t.Errorf("last row should end as Stopped:\n%q", s)
	}
}
