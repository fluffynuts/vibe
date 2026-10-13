package main

import (
	"fmt"
	"sort"
	"strings"

	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// cleanupRow is one sandbox offered for deletion: what sbx knows about it,
// plus what vibe's instance record adds — the profile it was built from and
// the folder it was built for, which is what makes a list of terse sandbox
// names identifiable months later.
type cleanupRow struct {
	name    string
	running bool
	profile string
	target  string
}

// cleanupRows pairs every sandbox sbx knows about with vibe's record of it,
// by name, sorted. A sandbox with no record — made by hand, by an older
// vibe, or left behind when ~/.vibe was cleared — is offered all the same:
// cleanup is about what sbx is holding, not about what vibe remembers
// creating, and an unrecognised sandbox is exactly the kind that accumulates.
func cleanupRows(statuses []sbxrun.Status, instances []state.Instance) []cleanupRow {
	known := make(map[string]state.Instance, len(instances))
	for _, inst := range instances {
		known[inst.Name] = inst
	}
	rows := make([]cleanupRow, 0, len(statuses))
	for _, s := range statuses {
		row := cleanupRow{name: s.Name, running: s.Running}
		if inst, ok := known[s.Name]; ok {
			row.profile, row.target = inst.Profile, inst.Target
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return rows
}

// label renders a row for the checklist, in the same name/status columns
// --list prints, with whatever vibe knows about the sandbox after them.
func (r cleanupRow) label() string {
	status := "not running"
	if r.running {
		status = "running"
	}
	detail := "vibe has no record of it"
	switch {
	case r.profile != "" && r.target != "":
		detail = fmt.Sprintf("profile %s, %s", r.profile, r.target)
	case r.profile != "":
		detail = "profile " + r.profile
	case r.target != "":
		detail = r.target
	}
	return fmt.Sprintf("%-16s %-11s  (%s)", r.name, status, detail)
}

// doCleanup offers every sandbox sbx knows about as a checklist and deletes
// the ones the user checks — and nothing else. Profiles are deliberately
// left alone: a profile outlives the sandboxes built from it, so deleting
// one here would silently turn the next plain `vibe` in that folder back
// into a guided-creation prompt. Removing a profile is what --re-create is
// for, and it says so up front.
//
// The checklist's own confirmation — which lists what is checked and
// defaults to going ahead — is the only one asked. A second question after
// it would be asking the same thing twice: the user came here to delete
// sandboxes and has just read back the list of them. What that leaves no
// room for is the warning, so it goes above the list, where it is on screen
// the whole time the choice is being made. --re-init and friends still ask
// their own question, because there the deletion is a side effect of
// something else the user asked for.
func doCleanup(vibeHome string) error {
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		note("no sandboxes found — nothing to clean up")
		return nil
	}
	instances, err := state.List(vibeHome)
	if err != nil {
		return err
	}
	rows := cleanupRows(statuses, instances)
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = r.label()
	}
	if !interactive() {
		return fmt.Errorf("--cleanup needs a terminal to pick sandboxes on")
	}
	// Unlike --re-init, nothing is rebuilt afterwards: whatever is only
	// inside these sandboxes — uncommitted work, agent memories no re-init
	// ever backed up — goes with them.
	note("deleting a sandbox cannot be undone, and takes any agent memories it holds with it")
	picked, ok := checklistFrom("Check the sandboxes to delete (their profiles are kept):", labels, nil)
	if !ok {
		return fmt.Errorf("aborted — nothing deleted")
	}
	if len(picked) == 0 {
		note("nothing checked — nothing deleted")
		return nil
	}
	names := make([]string, len(picked))
	for i, idx := range picked {
		names[i] = rows[idx].name
	}
	return removeSandboxes(vibeHome, names, sbxrun.Remove)
}

// removeSandboxes deletes each named sandbox and drops vibe's record of it,
// carrying on past a failure so one stuck sandbox doesn't strand the rest
// and reporting at the end what it could not remove.
//
// The instance record has to go with the sandbox here. --re-init keeps it on
// purpose, because the sandbox it rebuilds re-claims the host ports the
// record remembers — but nothing is coming back from a cleanup, and
// allocatePublish treats every port a record claims as taken whether or not
// anything is listening on it, so a record left behind would reserve that
// sandbox's published ports against every future sandbox, forever.
//
// remove is sbxrun.Remove in production, and a stub under test.
func removeSandboxes(vibeHome string, names []string, remove func(name string, force bool) error) error {
	var failed []string
	for _, name := range names {
		note("removing sandbox '%s'", name)
		// Force: the checklist shows which are running, and having checked
		// one the user does not want to be told it is up.
		if err := remove(name, true); err != nil {
			note("  WARNING: could not remove '%s': %s", name, err)
			failed = append(failed, name)
			continue
		}
		forgetGitSecret(vibeHome, name)
		if err := state.Remove(vibeHome, name); err != nil {
			note("  WARNING: %s", err)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not remove: %s", strings.Join(failed, ", "))
	}
	if len(names) == 1 {
		note("removed sandbox '%s'; the profile it was built from was left alone", names[0])
	} else {
		note("removed %d sandboxes; the profiles they were built from were left alone", len(names))
	}
	return nil
}
