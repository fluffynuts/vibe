package main

import (
	"context"
	"sort"

	"vibe/internal/layout"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// inventory is everything vibe has made on this machine, grouped by
// profile: what --cleanup, --update-sandboxes and the Profiles tab all work
// from, so that they agree about what exists.
type inventory struct {
	profiles []inventoryProfile
	// unknown are the sandboxes sbx has that vibe has no record of: made
	// with sbx directly, or left behind when ~/.vibe was cleared.
	unknown []sbxrun.Status
}

// inventoryProfile is a profile, with the sandboxes vibe built from it.
type inventoryProfile struct {
	name string
	// exists is false for a profile vibe's records name that is gone.
	exists bool
	// bundled is a profile that ships with vibe: it can't be deleted, as
	// the next upgrade would bring it back.
	bundled  bool
	features []string
	// standing is how it stands against the library; err when that
	// couldn't be told.
	standing  profileOutcome
	err       error
	sandboxes []inventorySandbox
}

// inventorySandbox is a sandbox vibe has a record of.
type inventorySandbox struct {
	inst state.Instance
	// inSbx is false when sbx no longer has it: only vibe's record is left.
	inSbx, running bool
	// attached is set when a vibe holds its folder.
	attached   bool
	folderGone bool
	// outOfDate says why it's out of date ("" when it isn't, or that
	// couldn't be told, which err says).
	outOfDate string
	err       error
}

// likelyTrash is a sandbox that's no use as it is: gone from sbx, or built
// for a folder that's gone.
func (s inventorySandbox) likelyTrash() bool {
	return !s.inSbx || s.folderGone
}

// likelyTrash is a profile of the user's whose sandboxes are all trash, or
// that has none.
func (p inventoryProfile) likelyTrash() bool {
	if !p.exists || p.bundled {
		return false
	}
	for _, s := range p.sandboxes {
		if !s.likelyTrash() {
			return false
		}
	}
	return true
}

// gatherInventory takes stock: every profile in both layers, and every one
// a record names; every sandbox vibe has a record of, under its profile,
// with what sbx says of it; and the sandboxes sbx has that vibe has no
// record of. ctx bounds the call to sbx.
func gatherInventory(ctx context.Context, lay layout.Layout) (inventory, error) {
	statuses, err := sbxrun.ListContext(ctx)
	if err != nil {
		return inventory{}, err
	}
	instances, err := state.List(lay.Home)
	if err != nil {
		return inventory{}, err
	}
	inSbx := make(map[string]sbxrun.Status, len(statuses))
	for _, s := range statuses {
		inSbx[s.Name] = s
	}

	byProfile := map[string][]state.Instance{}
	recorded := map[string]bool{}
	for _, inst := range instances {
		byProfile[inst.Profile] = append(byProfile[inst.Profile], inst)
		recorded[inst.Name] = true
	}
	names := append([]string{}, lay.Profiles()...)
	for name := range byProfile {
		if !lay.ProfileExists(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var inv inventory
	for _, name := range names {
		p := inventoryProfile{name: name, exists: lay.ProfileExists(name)}
		if p.exists {
			p.bundled = !fileExists(lay.HomePath("profiles", name))
			p.standing, p.features, p.err = assessProfile(lay, name)
		}
		sandboxes := byProfile[name]
		sort.Slice(sandboxes, func(i, j int) bool { return sandboxes[i].Name < sandboxes[j].Name })
		for _, inst := range sandboxes {
			status, found := inSbx[inst.Name]
			s := inventorySandbox{
				inst: inst, inSbx: found, running: status.Running,
				attached:   len(running.Others(lay.Home, inst.Target)) > 0,
				folderGone: folderGone(inst.Target) != "",
			}
			if found && p.exists {
				s.outOfDate, s.err = sandboxOutOfDate(lay, inst)
			}
			p.sandboxes = append(p.sandboxes, s)
		}
		inv.profiles = append(inv.profiles, p)
	}
	for _, s := range statuses {
		if !recorded[s.Name] {
			inv.unknown = append(inv.unknown, s)
		}
	}
	sort.Slice(inv.unknown, func(i, j int) bool { return inv.unknown[i].Name < inv.unknown[j].Name })
	return inv, nil
}
