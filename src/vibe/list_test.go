package main

import (
	"strings"
	"testing"

	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

func renderList(statuses []sbxrun.Status, records []state.Instance, color bool) string {
	return renderListWith(statuses, records, nil, color)
}

func renderListWith(statuses []sbxrun.Status, records []state.Instance, claims map[string][]running.Claim, color bool) string {
	var out strings.Builder
	writeList(&out, listRows(statuses, records, func(target string) []running.Claim { return claims[target] }), color)
	return out.String()
}

func TestListGroupsByStateThenSortsByNameIgnoringCase(t *testing.T) {
	got := renderList([]sbxrun.Status{
		{Name: "zeta", State: "stopped"},
		{Name: "Beta", Running: true, State: "running"},
		{Name: "odd", State: "creating"},
		{Name: "alpha", Running: true, State: "running"},
		{Name: "Gamma", State: "stopped"},
		{Name: "blank"},
	}, nil, false)
	want := "" +
		"RUNNING   alpha (sbx)\n" +
		"RUNNING   Beta (sbx)\n" +
		"UNKNOWN   blank (sbx)\n" +
		"UNKNOWN   odd (sbx)\n" +
		"STOPPED   Gamma (sbx)\n" +
		"STOPPED   zeta (sbx)\n"
	if got != want {
		t.Errorf("list =\n%s\nwant\n%s", got, want)
	}
}

// Only a sandbox vibe didn't make (one made with sbx itself) is marked:
// most are vibe's, and a column saying so on every row says nothing.
func TestListMarksOnlyTheSandboxesMadeWithSbx(t *testing.T) {
	got := renderList([]sbxrun.Status{
		{Name: "handmade", State: "stopped"},
		{Name: "mine", State: "stopped"},
	}, []state.Instance{{Name: "mine", Target: "/work/mine"}}, false)
	want := "" +
		"STOPPED   handmade (sbx)\n" +
		"STOPPED   mine\n"
	if got != want {
		t.Errorf("list =\n%s\nwant\n%s", got, want)
	}
}

func TestListShowsRecordsSbxNoLongerHasAsGoneWithTheirFoldersLast(t *testing.T) {
	got := renderList([]sbxrun.Status{
		{Name: "a-sandbox-with-a-very-long-name", Running: true, State: "running"},
	}, []state.Instance{
		{Name: "a-sandbox-with-a-very-long-name", Target: "/work/long"},
		{Name: "old", Target: "/work/old"},
		{Name: "older-one", Target: "/work/older one"},
	}, false)
	want := "" +
		"RUNNING   a-sandbox-with-a-very-long-name\n" +
		"GONE      old        /work/old\n" +
		"GONE      older-one  /work/older one\n"
	if got != want {
		t.Errorf("list =\n%s\nwant\n%s", got, want)
	}
}

func TestListColoursOnlyTheState(t *testing.T) {
	statuses := []sbxrun.Status{
		{Name: "up", Running: true, State: "running"},
		{Name: "odd", State: "creating"},
		{Name: "down", State: "stopped"},
	}
	records := []state.Instance{{Name: "gone", Target: "/work/gone"}}
	got := renderList(statuses, records, true)
	want := "" +
		"\x1b[32mRUNNING \x1b[0m  up (sbx)\n" +
		"\x1b[33mUNKNOWN \x1b[0m  odd (sbx)\n" +
		"\x1b[31mSTOPPED \x1b[0m  down (sbx)\n" +
		"\x1b[90mGONE    \x1b[0m  gone  /work/gone\n"
	if got != want {
		t.Errorf("list =\n%q\nwant\n%q", got, want)
	}
	if plain := renderList(statuses, records, false); strings.Contains(plain, "\x1b") {
		t.Errorf("uncoloured list has escapes:\n%q", plain)
	}
}

// A sandbox sbx says is stopped, with a vibe still attached, is asleep:
// sbx stops one left idle under its session. It's listed with the running
// ones, as it's still in use. A vibe only rebuilding it doesn't count.
func TestListShowsASandboxStoppedUnderAVibeAsSleeping(t *testing.T) {
	got := renderListWith([]sbxrun.Status{
		{Name: "up", Running: true, State: "running"},
		{Name: "asleep", State: "stopped"},
		{Name: "rebuilding", State: "stopped"},
		{Name: "down", State: "stopped"},
	}, []state.Instance{
		{Name: "asleep", Target: "/w/asleep"}, {Name: "rebuilding", Target: "/w/rebuilding"}, {Name: "down", Target: "/w/down"},
	}, map[string][]running.Claim{
		"/w/asleep":     {{PID: 1}},
		"/w/rebuilding": {{PID: 2, Work: "rebuilding sandbox 'rebuilding'"}},
	}, false)
	want := "" +
		"RUNNING   up (sbx)\n" +
		"SLEEPING  asleep\n" +
		"STOPPED   down\n" +
		"STOPPED   rebuilding\n"
	if got != want {
		t.Errorf("list =\n%s\nwant\n%s", got, want)
	}
	if coloured := renderListWith([]sbxrun.Status{{Name: "asleep", State: "stopped"}},
		[]state.Instance{{Name: "asleep", Target: "/w/asleep"}},
		map[string][]running.Claim{"/w/asleep": {{PID: 1}}}, true); coloured != "\x1b[36mSLEEPING\x1b[0m  asleep\n" {
		t.Errorf("coloured: %q", coloured)
	}
}

func TestListSaysWhatTheSbxMarkMeansOnlyWhenThereIsOne(t *testing.T) {
	said := strings.Join(notesOf(func() {
		explainList([]listRow{{state: listStopped, owner: "sbx", name: "handmade"}, {state: listStopped, owner: "vibe", name: "mine"}})
	}), "\n")
	if !strings.Contains(said, "(sbx)") || !strings.Contains(said, "handmade") || strings.Contains(said, "mine") {
		t.Errorf("said %q, want what (sbx) means, naming the one made with sbx", said)
	}
	if said := notesOf(func() { explainList([]listRow{{state: listStopped, owner: "vibe", name: "mine"}}) }); len(said) != 0 {
		t.Errorf("with none made with sbx, said %q", said)
	}
}

func TestListSaysWhatSleepingMeansAndHowToStopIt(t *testing.T) {
	said := strings.Join(notesOf(func() {
		explainList([]listRow{{state: listSleeping, owner: "vibe", name: "asleep"}, {state: listRunning, owner: "vibe", name: "up"}})
	}), "\n")
	if !strings.Contains(said, "SLEEPING") || !strings.Contains(said, "vibe --stop") || strings.Contains(said, "up") {
		t.Errorf("said %q, want what SLEEPING means and how to stop it, for the sleeping one only", said)
	}
}
