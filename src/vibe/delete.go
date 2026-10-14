package main

import (
	"fmt"
	"os"
	"strings"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// deletion is what --delete can remove for one folder: its sandbox, when sbx
// has it, and its profile, when ~/.vibe has a copy to delete — a profile that
// only ships with vibe would come straight back with the next upgrade.
type deletion struct {
	name       string
	sandbox    bool   // sbx has a sandbox called name
	profile    string // the profile name was (or would be) built from
	profileDir string // the overlay copy of profile; empty when there is none
	sharedWith []string
}

// options lists what d offers, as checklist labels with whether each starts
// ticked, and which it is: the sandbox, ticked, since a re-run of vibe
// rebuilds it; the profile, not, since that is work that can't be redone
// as easily.
func (d deletion) options() (labels []string, checked []bool, isSandbox []bool) {
	if d.sandbox {
		labels = append(labels, fmt.Sprintf("remove the sandbox %s", d.name))
		checked = append(checked, true)
		isSandbox = append(isSandbox, true)
	}
	if d.profileDir != "" {
		label := fmt.Sprintf("remove the profile %s", d.profile)
		if len(d.sharedWith) > 0 {
			label += fmt.Sprintf(" (also used by %s)", strings.Join(d.sharedWith, ", "))
		}
		labels = append(labels, label)
		checked = append(checked, false)
		isSandbox = append(isSandbox, false)
	}
	return labels, checked, isSandbox
}

// pickDeletions asks which of labels to delete, starting from checked,
// returning the indices picked; ok is false when the user quit. A variable
// so tests can stand in for the user.
var pickDeletions = func(labels []string, checked []bool) ([]int, bool) {
	return checklistFrom("Select what to delete (this cannot be undone):", labels, checked)
}

// choose settles what to delete: everything d offers with -f, else what
// the user ticks.
func (d deletion) choose(force bool) (sandbox, profile bool, err error) {
	labels, checked, isSandbox := d.options()
	if len(labels) == 0 {
		return false, false, nil
	}
	if force {
		return d.sandbox, d.profileDir != "", nil
	}
	if !interactive() {
		return false, false, fmt.Errorf("--delete needs a terminal to confirm on, or -f to delete the sandbox and its profile unasked")
	}
	picked, ok := pickDeletions(labels, checked)
	if !ok {
		return false, false, fmt.Errorf("aborted — nothing deleted")
	}
	for _, i := range picked {
		if isSandbox[i] {
			sandbox = true
		} else {
			profile = true
		}
	}
	return sandbox, profile, nil
}

// run deletes the sandbox and/or profile, saying what it removed and what it
// left. The sandbox's instance record goes with it, as with --cleanup, so
// it doesn't hold on to its published ports. remove is sbxrun.Remove in
// production, and a stub under test.
func (d deletion) run(vibeHome string, sandbox, profile bool, remove func(name string, force bool) error) error {
	if sandbox {
		note("removing sandbox '%s'", d.name)
		// Force: the user has just ticked it, and doesn't want to be told
		// it is running.
		if err := remove(d.name, true); err != nil {
			return fmt.Errorf("could not remove sandbox '%s': %w", d.name, err)
		}
		forgetGitSecret(vibeHome, d.name)
		if err := state.Remove(vibeHome, d.name); err != nil {
			note("WARNING: %s", err)
		}
		note("removed sandbox '%s'", d.name)
	} else if d.sandbox {
		note("kept sandbox '%s'", d.name)
	}
	if profile {
		if err := os.RemoveAll(d.profileDir); err != nil {
			return fmt.Errorf("could not delete profile '%s': %w", d.profile, err)
		}
		note("deleted profile '%s' from %s", d.profile, displayPath(d.profileDir))
	} else if d.profileDir != "" {
		note("kept profile '%s'", d.profile)
	}
	return nil
}

// doDelete deletes the sandbox for target, its profile, or both, as picked
// from a checklist — or both with -f.
func doDelete(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}
	d := deletion{name: name, sandbox: sbxrun.Exists(name), profile: profile}
	if dir := lay.HomePath("profiles", profile); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			d.profileDir = dir
		}
	}
	if d.profileDir == "" {
		if info, err := os.Stat(lay.BundlePath("profiles", profile)); err == nil && info.IsDir() {
			note("profile '%s' ships with vibe, so it isn't offered for deletion", profile)
		}
	}
	instances, err := state.List(lay.Home)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Profile == profile && inst.Name != name {
			d.sharedWith = append(d.sharedWith, inst.Name)
		}
	}

	sandbox, prof, err := d.choose(args.Force)
	if err != nil {
		return err
	}
	if !sandbox && !prof {
		if !d.sandbox && d.profileDir == "" {
			note("no sandbox '%s' and no profile '%s' in %s — nothing to delete", name, profile, displayPath(lay.HomePath("profiles")))
		} else {
			note("nothing checked — nothing deleted")
		}
		return nil
	}
	return d.run(lay.Home, sandbox, prof, sbxrun.Remove)
}
