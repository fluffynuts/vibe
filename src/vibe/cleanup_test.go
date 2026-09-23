package main

import (
	"fmt"
	"strings"
	"testing"

	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// TestCleanupRowsPairSandboxesWithVibesRecords covers what the checklist is
// for: sbx only knows a sandbox's name and whether it is up, which is not
// enough to decide whether it can go. The profile and folder come from
// vibe's own records, and a sandbox vibe has no record of still has to be
// offered — those are exactly the ones that pile up.
func TestCleanupRowsPairSandboxesWithVibesRecords(t *testing.T) {
	statuses := []sbxrun.Status{
		{Name: "zeta", Running: true},
		{Name: "alpha"},
		{Name: "orphan", Running: true},
	}
	instances := []state.Instance{
		{Name: "alpha", Profile: "yumbi", Target: "/home/d/code/alpha"},
		{Name: "zeta", Profile: "zeta"},
		{Name: "gone", Profile: "stale"}, // a record whose sandbox sbx no longer has
	}

	rows := cleanupRows(statuses, instances)

	var names []string
	for _, r := range rows {
		names = append(names, r.name)
	}
	if got, want := strings.Join(names, ","), "alpha,orphan,zeta"; got != want {
		t.Errorf("rows = %s, want %s (sorted, and only what sbx has)", got, want)
	}
	if rows[0].profile != "yumbi" || rows[0].target != "/home/d/code/alpha" || rows[0].running {
		t.Errorf("alpha row = %+v", rows[0])
	}
	if rows[1].profile != "" || !rows[1].running {
		t.Errorf("orphan row = %+v", rows[1])
	}

	labels := []string{rows[0].label(), rows[1].label(), rows[2].label()}
	for _, want := range []string{"alpha", "not running", "profile yumbi, /home/d/code/alpha"} {
		if !strings.Contains(labels[0], want) {
			t.Errorf("label %q does not mention %q", labels[0], want)
		}
	}
	if !strings.Contains(labels[1], "no record") {
		t.Errorf("an unrecorded sandbox should say so: %q", labels[1])
	}
	if !strings.Contains(labels[2], "running") || strings.Contains(labels[2], "not running") {
		t.Errorf("a running sandbox should say so: %q", labels[2])
	}
	if !strings.Contains(labels[2], "profile zeta") || strings.Contains(labels[2], ", ") {
		t.Errorf("a record with no target should show the profile alone: %q", labels[2])
	}
}

// TestRemoveSandboxesFreesTheirPublishedPorts is the reason cleanup deletes
// the instance record and --re-init does not: allocatePublish treats a port
// any record claims as taken whether or not anything is listening, so a
// record outliving its sandbox would reserve that port against every sandbox
// created afterwards.
func TestRemoveSandboxesFreesTheirPublishedPorts(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	saveInstance(t, home, "old", state.PublishRecord{Name: "diffity", ContainerPort: base, HostPort: base})

	var removed []string
	err := removeSandboxes(home, []string{"old"}, func(name string, force bool) error {
		if !force {
			t.Errorf("cleanup should force-remove %s: the user checked it knowing it was running", name)
		}
		removed = append(removed, name)
		return nil
	})
	if err != nil {
		t.Fatalf("removeSandboxes: %v", err)
	}
	if strings.Join(removed, ",") != "old" {
		t.Errorf("removed = %v, want [old]", removed)
	}
	if _, found, err := state.Load(home, "old"); err != nil || found {
		t.Errorf("the instance record outlived the sandbox (found=%v, err=%v)", found, err)
	}

	mappings, err := allocatePublish(home, "new", publishOne(base))
	if err != nil {
		t.Fatalf("allocatePublish: %v", err)
	}
	if got := hostPorts(mappings); len(got) != 1 || got[0] != base {
		t.Errorf("host ports = %v, want [%d] — the deleted sandbox is still holding its port", got, base)
	}
}

// TestRemoveSandboxesCarriesOnPastAFailure keeps one wedged sandbox from
// stranding the rest of a batch the user has already confirmed, while still
// failing loudly enough to name what is left over.
func TestRemoveSandboxesCarriesOnPastAFailure(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"first", "stuck", "last"} {
		saveInstance(t, home, name)
	}

	var attempted []string
	err := removeSandboxes(home, []string{"first", "stuck", "last"}, func(name string, force bool) error {
		attempted = append(attempted, name)
		if name == "stuck" {
			return fmt.Errorf("container is wedged")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "stuck") {
		t.Errorf("error = %v, want one naming 'stuck'", err)
	}
	if strings.Join(attempted, ",") != "first,stuck,last" {
		t.Errorf("attempted = %v, want every name tried", attempted)
	}
	for _, name := range []string{"first", "last"} {
		if _, found, _ := state.Load(home, name); found {
			t.Errorf("'%s' was removed but kept its instance record", name)
		}
	}
	// The record of what could not be deleted stays: the sandbox is still
	// there, and still holding whatever ports it was given.
	if _, found, _ := state.Load(home, "stuck"); !found {
		t.Error("'stuck' was not removed, so its instance record should remain")
	}
}

// TestRemoveSandboxesToleratesAnUnrecordedSandbox covers the orphan case the
// checklist deliberately offers: no instance record to delete is not an error.
func TestRemoveSandboxesToleratesAnUnrecordedSandbox(t *testing.T) {
	if err := removeSandboxes(t.TempDir(), []string{"orphan"}, func(string, bool) error { return nil }); err != nil {
		t.Errorf("removeSandboxes: %v", err)
	}
}
