package main

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
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

// --stop-all stops every running sandbox, and every one asleep under a vibe
// (sbx says it's stopped, but a vibe is attached): that vibe is ended too,
// as it is for each running one. A vibe only holding a folder for a rebuild
// isn't one to end.
func TestStopAllTakesInSleepingSandboxesAndTheirVibes(t *testing.T) {
	statuses := []sbxrun.Status{
		{Name: "web", Running: true, State: "running"},
		{Name: "api", State: "stopped"},
		{Name: "old", State: "stopped"},
		{Name: "mid", State: "stopped"},
		{Name: "handmade", Running: true, State: "running"},
	}
	records := []state.Instance{
		{Name: "web", Target: "/w/web"}, {Name: "api", Target: "/w/api"},
		{Name: "old", Target: "/w/old"}, {Name: "mid", Target: "/w/mid"},
	}
	claims := map[string][]running.Claim{
		"/w/web": {{PID: 1}},
		"/w/api": {{PID: 2, Headless: true}},
		"/w/mid": {{PID: 3, Work: "rebuilding sandbox 'mid'"}},
	}
	plan := planStopAll(statuses, records, func(target string) []running.Claim { return claims[target] })
	if got, want := strings.Join(plan.names, ","), "api,handmade,web"; got != want {
		t.Errorf("stops %q, want %q", got, want)
	}
	if !plan.sleeping["api"] || plan.sleeping["web"] || plan.sleeping["handmade"] {
		t.Errorf("sleeping = %v, want just api", plan.sleeping)
	}
	if len(plan.vibes["web"]) != 1 || len(plan.vibes["api"]) != 1 || len(plan.vibes["handmade"]) != 0 {
		t.Errorf("vibes = %v, want web's and api's", plan.vibes)
	}
}
