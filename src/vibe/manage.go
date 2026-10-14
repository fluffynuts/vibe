package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vibe/internal/backup"
	"vibe/internal/layout"
	agentmem "vibe/internal/memory"
	"vibe/internal/state"
)

// What can be picked for deleting, as the IDs the CLI's checklist and the
// Profiles page select by: a whole profile, one sandbox vibe has a record
// of, or a sandbox it hasn't.
const (
	pickProfile = "profile:"
	pickSandbox = "sandbox:"
	pickUnknown = "unknown:"
)

// profileFlags are what's worth knowing about p at a glance when deciding
// whether it's trash.
func profileFlags(p inventoryProfile) []string {
	var flags []string
	switch {
	case !p.exists:
		flags = append(flags, "profile gone")
	case p.bundled:
		flags = append(flags, "vibe's own")
	}
	if p.exists && len(p.sandboxes) == 0 {
		flags = append(flags, "no sandboxes")
	} else if len(p.sandboxes) > 0 {
		allGone := true
		for _, s := range p.sandboxes {
			if !s.likelyTrash() {
				allGone = false
			}
		}
		if allGone {
			flags = append(flags, "every sandbox or folder gone")
		}
	}
	switch p.standing.state {
	case profileOutOfDate:
		flags = append(flags, "out of date")
	case profileEdited:
		flags = append(flags, "edited by hand")
	case profileMissingFeatures:
		flags = append(flags, "features missing from the library")
	}
	return flags
}

// sandboxFlags are what's worth knowing about s at a glance.
func sandboxFlags(s inventorySandbox) []string {
	var flags []string
	switch {
	case !s.inSbx:
		flags = append(flags, "gone from sbx")
	case s.running:
		flags = append(flags, "running")
	default:
		flags = append(flags, "stopped")
	}
	if s.folderGone {
		flags = append(flags, "folder gone")
	}
	if s.attached {
		flags = append(flags, "a vibe is attached")
	}
	if s.outOfDate != "" {
		flags = append(flags, "out of date")
	}
	return flags
}

// removal is what a selection comes to: whole profiles (with all their
// sandboxes), single sandboxes (their profiles kept), and sandboxes vibe
// has no record of.
type removal struct {
	profiles  []inventoryProfile
	sandboxes []inventorySandbox
	unknown   []string
}

func (d removal) empty() bool {
	return len(d.profiles) == 0 && len(d.sandboxes) == 0 && len(d.unknown) == 0
}

// whyKept says why sandbox s can't be deleted, or "" when it can. own is
// the sandbox whose page is asking ("" from a terminal): deleting it would
// end the page.
func whyKept(s inventorySandbox, own string) string {
	switch {
	case own != "" && s.inst.Name == own:
		return "it's the sandbox this page is for — use 'vibe --manage', or another sandbox's page"
	case s.attached:
		return "a vibe is attached to it — stop it first ('vibe --stop " + s.inst.Target + "')"
	}
	return ""
}

// whyProfileKept says why profile p can't be deleted, or "" when it can.
func whyProfileKept(p inventoryProfile, own string) string {
	switch {
	case !p.exists:
		return "it's already gone"
	case p.bundled:
		return "it ships with vibe, and the next upgrade would bring it back"
	}
	for _, s := range p.sandboxes {
		if why := whyKept(s, own); why != "" {
			return fmt.Sprintf("its sandbox '%s' can't be deleted: %s", s.inst.Name, why)
		}
	}
	return ""
}

// planRemoval turns picked IDs into what's to be deleted, refusing any
// that can't be, and dropping a sandbox picked along with its profile.
func planRemoval(inv inventory, picked []string, own string) (removal, error) {
	profiles := map[string]inventoryProfile{}
	sandboxes := map[string]inventorySandbox{}
	for _, p := range inv.profiles {
		profiles[p.name] = p
		for _, s := range p.sandboxes {
			sandboxes[s.inst.Name] = s
		}
	}
	unknown := map[string]bool{}
	for _, s := range inv.unknown {
		unknown[s.Name] = true
	}

	var d removal
	wholeProfile := map[string]bool{}
	var singles []inventorySandbox
	for _, id := range picked {
		switch {
		case strings.HasPrefix(id, pickProfile):
			p, ok := profiles[strings.TrimPrefix(id, pickProfile)]
			if !ok {
				return removal{}, fmt.Errorf("there's no profile '%s'", strings.TrimPrefix(id, pickProfile))
			}
			if why := whyProfileKept(p, own); why != "" {
				return removal{}, fmt.Errorf("profile '%s' can't be deleted: %s", p.name, why)
			}
			if !wholeProfile[p.name] {
				wholeProfile[p.name] = true
				d.profiles = append(d.profiles, p)
			}
		case strings.HasPrefix(id, pickSandbox):
			s, ok := sandboxes[strings.TrimPrefix(id, pickSandbox)]
			if !ok {
				return removal{}, fmt.Errorf("vibe has no record of a sandbox '%s'", strings.TrimPrefix(id, pickSandbox))
			}
			if why := whyKept(s, own); why != "" {
				return removal{}, fmt.Errorf("sandbox '%s' can't be deleted: %s", s.inst.Name, why)
			}
			singles = append(singles, s)
		case strings.HasPrefix(id, pickUnknown):
			name := strings.TrimPrefix(id, pickUnknown)
			if !unknown[name] {
				return removal{}, fmt.Errorf("sbx has no unrecorded sandbox '%s'", name)
			}
			d.unknown = append(d.unknown, name)
		default:
			return removal{}, fmt.Errorf("'%s' isn't something that can be picked", id)
		}
	}
	seen := map[string]bool{}
	for _, s := range singles {
		if !wholeProfile[s.inst.Profile] && !seen[s.inst.Name] {
			seen[s.inst.Name] = true
			d.sandboxes = append(d.sandboxes, s)
		}
	}
	return d, nil
}

// memoryStoreOf is where sandbox inst's agent memories are kept on the
// host, or "" when that's not a folder of its own (which only happens if a
// record were edited to point elsewhere): one shared with anything else is
// never deleted.
func memoryStoreOf(lay layout.Layout, inst state.Instance) string {
	store := inst.MemoryStore
	if store == "" {
		store = agentmem.StoreFor(lay.HomePath("memories"), inst.Name)
	}
	if filepath.Base(store) != inst.Name {
		return ""
	}
	return store
}

// profileBackups are the backups of profile p: taken under its own name, or
// a sandbox of its.
func profileBackups(lay layout.Layout, p inventoryProfile) []string {
	names := []string{p.name}
	for _, s := range p.sandboxes {
		names = append(names, s.inst.Name)
	}
	var paths []string
	seen := map[string]bool{}
	for _, name := range names {
		list, _ := backup.List(lay.HomePath("backup"), name)
		for _, b := range list {
			if !seen[b.Path] {
				seen[b.Path] = true
				paths = append(paths, b.Path)
			}
		}
	}
	return paths
}

// describe lists everything d deletes, by name and path, for the user to
// confirm: exactly what will go.
func (d removal) describe(lay layout.Layout) []string {
	var lines []string
	sandboxLines := func(s inventorySandbox, indent string) {
		what := "its record"
		if s.inSbx {
			what = "the sandbox, and its record"
		}
		lines = append(lines, fmt.Sprintf("%ssandbox '%s' (for %s): %s", indent, s.inst.Name, displayPath(s.inst.Target), what))
		if store := memoryStoreOf(lay, s.inst); store != "" && fileExists(store) {
			lines = append(lines, indent+"  agent memories: "+displayPath(store))
		}
		if dir := state.ClipboardPath(lay.Home, s.inst.Name); fileExists(dir) {
			lines = append(lines, indent+"  clipboard history: "+displayPath(dir))
		}
		if file := state.ConversationPath(lay.Home, s.inst.Name); fileExists(file) {
			lines = append(lines, indent+"  conversation: "+displayPath(file))
		}
		if s.inst.GitToken != "" {
			lines = append(lines, indent+"  its GitHub token")
		}
	}
	for _, p := range d.profiles {
		lines = append(lines, fmt.Sprintf("profile '%s': %s", p.name, displayPath(lay.HomePath("profiles", p.name))))
		if backups := profileBackups(lay, p); len(backups) > 0 {
			lines = append(lines, fmt.Sprintf("  its %d backup(s), in %s", len(backups), displayPath(lay.HomePath("backup"))))
		}
		for _, s := range p.sandboxes {
			sandboxLines(s, "  ")
		}
	}
	for _, s := range d.sandboxes {
		sandboxLines(s, "")
	}
	for _, name := range d.unknown {
		lines = append(lines, fmt.Sprintf("sandbox '%s' (vibe has no record of it)", name))
	}
	return lines
}

// run deletes everything d holds, carrying on past a failure, and says
// what it couldn't delete. A profile is kept when one of its sandboxes
// can't be removed, so that deleting it can be tried again. remove is
// sbxrun.Remove, and a stand-in under test.
func (d removal) run(lay layout.Layout, remove func(name string, force bool) error) error {
	var failed []string
	for _, p := range d.profiles {
		ok := true
		for _, s := range p.sandboxes {
			if err := deleteSandbox(lay, s, remove); err != nil {
				note("  WARNING: %s", err)
				failed = append(failed, "sandbox '"+s.inst.Name+"'")
				ok = false
			}
		}
		if !ok {
			note("  kept profile '%s', as not all its sandboxes could be removed", p.name)
			failed = append(failed, "profile '"+p.name+"'")
			continue
		}
		for _, path := range profileBackups(lay, p) {
			if err := os.Remove(path); err != nil {
				note("  WARNING: couldn't delete backup %s: %s", displayPath(path), err)
			}
		}
		if err := os.RemoveAll(lay.HomePath("profiles", p.name)); err != nil {
			note("  WARNING: couldn't delete profile '%s': %s", p.name, err)
			failed = append(failed, "profile '"+p.name+"'")
			continue
		}
		note("deleted profile '%s'", p.name)
	}
	for _, s := range d.sandboxes {
		if err := deleteSandbox(lay, s, remove); err != nil {
			note("  WARNING: %s", err)
			failed = append(failed, "sandbox '"+s.inst.Name+"'")
		}
	}
	for _, name := range d.unknown {
		note("removing sandbox '%s'", name)
		if err := remove(name, true); err != nil {
			note("  WARNING: couldn't remove sandbox '%s': %s", name, err)
			failed = append(failed, "sandbox '"+name+"'")
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("couldn't delete %s — see above", strings.Join(failed, ", "))
	}
	return nil
}

// deleteSandbox removes sandbox s, when sbx still has it, and everything
// vibe keeps for it. Its record goes too: a record outliving its sandbox
// would hold its published ports against every sandbox made after it. When
// sbx can't remove it, nothing of vibe's is deleted, so it can be tried
// again.
func deleteSandbox(lay layout.Layout, s inventorySandbox, remove func(name string, force bool) error) error {
	name := s.inst.Name
	if s.inSbx {
		note("removing sandbox '%s'", name)
		// Forced: whether it's running was shown when it was picked.
		if err := remove(name, true); err != nil {
			return fmt.Errorf("couldn't remove sandbox '%s': %w", name, err)
		}
	}
	forgetGitSecret(lay.Home, name)
	if err := state.Remove(lay.Home, name); err != nil {
		return fmt.Errorf("sandbox '%s': %w", name, err)
	}
	if store := memoryStoreOf(lay, s.inst); store != "" {
		if err := os.RemoveAll(store); err != nil {
			return fmt.Errorf("sandbox '%s': couldn't delete its memories: %w", name, err)
		}
	}
	note("deleted sandbox '%s'", name)
	return nil
}
