package main

import (
	"strings"
	"testing"

	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

func renderList(statuses []sbxrun.Status, records []state.Instance, color bool) string {
	var out strings.Builder
	writeList(&out, listRows(statuses, records), color)
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
		"RUNNING  sbx   alpha\n" +
		"RUNNING  sbx   Beta\n" +
		"UNKNOWN  sbx   blank\n" +
		"UNKNOWN  sbx   odd\n" +
		"STOPPED  sbx   Gamma\n" +
		"STOPPED  sbx   zeta\n"
	if got != want {
		t.Errorf("list =\n%s\nwant\n%s", got, want)
	}
}

func TestListSaysWhichSandboxesVibeMade(t *testing.T) {
	got := renderList([]sbxrun.Status{
		{Name: "handmade", State: "stopped"},
		{Name: "mine", State: "stopped"},
	}, []state.Instance{{Name: "mine", Target: "/work/mine"}}, false)
	want := "" +
		"STOPPED  sbx   handmade\n" +
		"STOPPED  vibe  mine\n"
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
		"RUNNING  vibe  a-sandbox-with-a-very-long-name\n" +
		"GONE     vibe  old        /work/old\n" +
		"GONE     vibe  older-one  /work/older one\n"
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
		"\x1b[32mRUNNING\x1b[0m  sbx   up\n" +
		"\x1b[33mUNKNOWN\x1b[0m  sbx   odd\n" +
		"\x1b[31mSTOPPED\x1b[0m  sbx   down\n" +
		"\x1b[90mGONE   \x1b[0m  vibe  gone  /work/gone\n"
	if got != want {
		t.Errorf("list =\n%q\nwant\n%q", got, want)
	}
	if plain := renderList(statuses, records, false); strings.Contains(plain, "\x1b") {
		t.Errorf("uncoloured list has escapes:\n%q", plain)
	}
}
