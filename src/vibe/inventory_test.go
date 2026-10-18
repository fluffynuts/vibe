package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/state"
)

func record(t *testing.T, lay layout.Layout, name, profile, target string) {
	t.Helper()
	if err := state.Save(lay.Home, state.Instance{Name: name, Profile: profile, Target: target, Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
}

func gathered(t *testing.T, lay layout.Layout) inventory {
	t.Helper()
	inv, err := gatherInventory(context.Background(), lay)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func profileIn(t *testing.T, inv inventory, name string) inventoryProfile {
	t.Helper()
	for _, p := range inv.profiles {
		if p.name == name {
			return p
		}
	}
	t.Fatalf("no profile %q in the inventory", name)
	return inventoryProfile{}
}

func TestTheInventoryGroupsSandboxesByProfile(t *testing.T) {
	lay := updateLayout(t) // profile "p", and the bundle's "golden"
	writeFile(t, lay.HomePath("profiles", "empty", "config.yaml"), "name: empty\n")
	record(t, lay, "b", "p", t.TempDir())
	record(t, lay, "a", "p", t.TempDir())
	record(t, lay, "orphan", "deleted", t.TempDir())
	sbxListing(t, "a running\\nb stopped\\norphan stopped\\nhandmade running\\n")

	inv := gathered(t, lay)
	p := profileIn(t, inv, "p")
	if len(p.sandboxes) != 2 || p.sandboxes[0].inst.Name != "a" || p.sandboxes[1].inst.Name != "b" {
		t.Errorf("profile p's sandboxes = %+v, want a then b", p.sandboxes)
	}
	if !p.sandboxes[0].running || p.sandboxes[1].running || !p.sandboxes[0].inSbx {
		t.Errorf("sbx's statuses weren't taken in: %+v", p.sandboxes)
	}
	if !p.exists || p.bundled || p.standing.state != profileCurrent || strings.Join(p.features, ",") != "mine" {
		t.Errorf("profile p = %+v", p)
	}
	if golden := profileIn(t, inv, bundledProfile); !golden.bundled {
		t.Error("the bundle's profile isn't marked as vibe's own")
	}
	if gone := profileIn(t, inv, "deleted"); gone.exists || len(gone.sandboxes) != 1 {
		t.Errorf("a profile only a record names = %+v, want it listed as gone, with its sandbox", gone)
	}
	if empty := profileIn(t, inv, "empty"); len(empty.sandboxes) != 0 || empty.standing.state != profileNotComposed {
		t.Errorf("profile empty = %+v", empty)
	}
	if len(inv.unknown) != 1 || inv.unknown[0].Name != "handmade" {
		t.Errorf("unknown = %+v, want just the sandbox vibe has no record of", inv.unknown)
	}
}

func TestTheInventorySaysWhatsWrongWithEachSandbox(t *testing.T) {
	lay := updateLayout(t)
	gone := filepath.Join(t.TempDir(), "gone")
	record(t, lay, "fine", "p", t.TempDir())
	record(t, lay, "nofolder", "p", gone)
	record(t, lay, "notinsbx", "p", t.TempDir())
	sbxListing(t, "fine running\\nnofolder stopped\\n")

	byName := map[string]inventorySandbox{}
	for _, s := range profileIn(t, gathered(t, lay), "p").sandboxes {
		byName[s.inst.Name] = s
	}
	if s := byName["fine"]; s.likelyTrash() || s.folderGone || !s.inSbx {
		t.Errorf("fine = %+v", s)
	}
	if s := byName["fine"]; !strings.Contains(s.outOfDate, "before vibe kept track") {
		t.Errorf("fine.outOfDate = %q, want it out of date (no fingerprint)", s.outOfDate)
	}
	if s := byName["nofolder"]; !s.folderGone || !s.likelyTrash() {
		t.Errorf("nofolder = %+v, want its folder gone, and trash", s)
	}
	if s := byName["notinsbx"]; s.inSbx || !s.likelyTrash() {
		t.Errorf("notinsbx = %+v, want it gone from sbx, and trash", s)
	}
}

func TestLikelyTrashProfiles(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "unused", "config.yaml"), "name: unused\n")
	writeFile(t, lay.HomePath("profiles", "abandoned", "config.yaml"), "name: abandoned\n")
	record(t, lay, "live", "p", t.TempDir())
	record(t, lay, "old", "abandoned", filepath.Join(t.TempDir(), "gone"))
	sbxListing(t, "live running\\nold stopped\\n")

	inv := gathered(t, lay)
	for name, want := range map[string]bool{"p": false, "unused": true, "abandoned": true, bundledProfile: false} {
		if got := profileIn(t, inv, name).likelyTrash(); got != want {
			t.Errorf("profile %s: likelyTrash = %v, want %v", name, got, want)
		}
	}
}

func TestTheInventoryFailsWhenSbxCantSay(t *testing.T) {
	lay := updateLayout(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sbx"), "#!/bin/sh\necho 'daemon not running' >&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(dir, "sbx"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := gatherInventory(context.Background(), lay); err == nil {
		t.Error("an inventory was taken without knowing what sbx has")
	}
}

// The Profiles tab flags a sandbox sbx has stopped under a vibe as
// sleeping, as 'vibe -l' lists it: still in use, unlike one stopped. A vibe
// only rebuilding it doesn't count.
func TestASandboxStoppedUnderAVibeIsFlaggedSleeping(t *testing.T) {
	asleep := inventorySandbox{inSbx: true, attached: true, vibes: []int{10}}
	rebuilding := inventorySandbox{inSbx: true, attached: true, rebuilding: "rebuilding sandbox 'a'"}
	down := inventorySandbox{inSbx: true}
	for _, c := range []struct {
		s    inventorySandbox
		want string
	}{{asleep, "sleeping"}, {rebuilding, "stopped"}, {down, "stopped"}} {
		if got := sandboxFlags(c.s); len(got) == 0 || got[0] != c.want {
			t.Errorf("sandboxFlags(%+v) = %v, want it first flagged %s", c.s, got, c.want)
		}
	}
}
