package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// sandboxRecord saves a record of sandbox name on profile, with a folder
// that exists, and returns how the inventory would see it, in sbx.
func sandboxRecord(t *testing.T, lay layout.Layout, name, profile string, recs ...state.PublishRecord) inventorySandbox {
	t.Helper()
	inst := state.Instance{Name: name, Profile: profile, Target: t.TempDir(), Publish: recs}
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	return inventorySandbox{inst: inst, inSbx: true}
}

// remover stands in for sbx rm, failing for the names in failing, and
// recording what it was asked to remove.
func remover(t *testing.T, removed *[]string, failing ...string) func(string, bool) error {
	return func(name string, force bool) error {
		if !force {
			t.Errorf("%s should be force-removed: it was picked knowing whether it was running", name)
		}
		*removed = append(*removed, name)
		if slices.Contains(failing, name) {
			return fmt.Errorf("container is wedged")
		}
		return nil
	}
}

func TestCleanupRowsTickTheLikelyTrash(t *testing.T) {
	gone := inventorySandbox{inst: state.Instance{Name: "old", Profile: "trash", Target: "/gone"}, folderGone: true, inSbx: true}
	live := inventorySandbox{inst: state.Instance{Name: "live", Profile: "keep", Target: "/live"}, inSbx: true}
	inv := inventory{
		profiles: []inventoryProfile{
			{name: "keep", exists: true, sandboxes: []inventorySandbox{live}},
			{name: "trash", exists: true, sandboxes: []inventorySandbox{gone}},
			{name: "unused", exists: true},
			{name: "golden", exists: true, bundled: true, sandboxes: []inventorySandbox{{inst: state.Instance{Name: "g", Profile: "golden"}, inSbx: false}}},
		},
		unknown: []sbxrun.Status{{Name: "handmade", Running: true}},
	}
	rows, kept := cleanupRows(inv)
	got := map[string]bool{}
	for _, r := range rows {
		got[r.id] = r.ticked
	}
	want := map[string]bool{
		"profile:keep": false, "sandbox:live": false,
		"profile:trash": true, "sandbox:old": false, // ticked through its profile
		"profile:unused":   true,
		"sandbox:g":        true, // the bundled profile isn't offered; its trash sandbox is
		"unknown:handmade": false,
	}
	for id, ticked := range want {
		if have, ok := got[id]; !ok || have != ticked {
			t.Errorf("row %s: offered %v, ticked %v; want offered, ticked %v", id, ok, have, ticked)
		}
	}
	if _, ok := got["profile:golden"]; ok || len(rows) != len(want) || len(kept) != 0 {
		t.Errorf("rows = %+v, kept = %v", rows, kept)
	}
}

func TestWhatCantBeDeletedIsntOfferedAndSaysWhy(t *testing.T) {
	attached := inventorySandbox{inst: state.Instance{Name: "busy", Profile: "p", Target: "/work/busy"}, inSbx: true, attached: true}
	inv := inventory{profiles: []inventoryProfile{{name: "p", exists: true, sandboxes: []inventorySandbox{attached}}}}
	rows, kept := cleanupRows(inv)
	if len(rows) != 0 {
		t.Errorf("rows = %+v, want nothing offered", rows)
	}
	joined := strings.Join(kept, "\n")
	for _, want := range []string{"profile 'p': its sandbox 'busy' can't be deleted", "sandbox 'busy': a vibe is attached to it", "vibe --stop /work/busy"} {
		if !strings.Contains(joined, want) {
			t.Errorf("kept doesn't say %q:\n%s", want, joined)
		}
	}
}

func TestPlanningARemovalRefusesWhatCantGo(t *testing.T) {
	own := inventorySandbox{inst: state.Instance{Name: "mine", Profile: "p"}, inSbx: true}
	other := inventorySandbox{inst: state.Instance{Name: "other", Profile: "p"}, inSbx: true}
	inv := inventory{profiles: []inventoryProfile{
		{name: "p", exists: true, sandboxes: []inventorySandbox{own, other}},
		{name: "golden", exists: true, bundled: true},
	}}
	for ids, want := range map[string]string{
		"profile:golden": "ships with vibe",
		"sandbox:mine":   "the sandbox this page is for",
		"profile:p":      "its sandbox 'mine' can't be deleted",
		"profile:nope":   "no profile 'nope'",
		"sandbox:nope":   "no record of a sandbox 'nope'",
		"unknown:nope":   "no unrecorded sandbox 'nope'",
		"other:x":        "isn't something that can be picked",
	} {
		if _, err := planRemoval(inv, []string{ids}, "mine"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("planRemoval(%s) = %v, want an error saying %q", ids, err, want)
		}
	}
	if d, err := planRemoval(inv, []string{"sandbox:other"}, "mine"); err != nil || len(d.sandboxes) != 1 {
		t.Errorf("planRemoval(sandbox:other) = %+v, %v", d, err)
	}
}

func TestASandboxPickedWithItsProfileGoesOnce(t *testing.T) {
	s := inventorySandbox{inst: state.Instance{Name: "a", Profile: "p"}, inSbx: true}
	inv := inventory{profiles: []inventoryProfile{{name: "p", exists: true, sandboxes: []inventorySandbox{s}}}}
	d, err := planRemoval(inv, []string{"sandbox:a", "profile:p", "profile:p"}, "")
	if err != nil || len(d.profiles) != 1 || len(d.sandboxes) != 0 {
		t.Errorf("planRemoval = %+v, %v; want the profile once, and the sandbox only through it", d, err)
	}
}

// Deleting a profile kills all of it: the profile, its sandboxes, and
// everything vibe keeps for them. Another profile is left alone.
func TestDeletingAProfileDeletesEverythingOfIt(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	writeFile(t, lay.HomePath("profiles", "keep", "config.yaml"), "name: keep\n")
	s := sandboxRecord(t, lay, "a", "p")
	keep := sandboxRecord(t, lay, "k", "keep")
	for _, name := range []string{"a", "k"} {
		writeFile(t, filepath.Join(lay.HomePath("memories"), name, "MEMORY.md"), "remember\n")
		writeFile(t, filepath.Join(state.ClipboardPath(lay.Home, name), "1.txt"), "pasted\n")
		writeFile(t, state.ConversationPath(lay.Home, name), "[]")
	}
	writeFile(t, lay.HomePath("backup", "a-2026-10-01_120000.zip"), "zip")
	writeFile(t, lay.HomePath("backup", "p-2026-10-02_120000.zip"), "zip")
	writeFile(t, lay.HomePath("backup", "k-2026-10-01_120000.zip"), "zip")

	p := inventoryProfile{name: "p", exists: true, sandboxes: []inventorySandbox{s}}
	d := removal{profiles: []inventoryProfile{p}}
	described := strings.Join(d.describe(lay), "\n")
	for _, want := range []string{"profile 'p'", "2 backup(s)", "sandbox 'a'", "agent memories", "clipboard history", "conversation"} {
		if !strings.Contains(described, want) {
			t.Errorf("the description doesn't mention %q:\n%s", want, described)
		}
	}

	var removed []string
	if err := d.run(lay, remover(t, &removed)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(removed, ",") != "a" {
		t.Errorf("sbx was asked to remove %v, want [a]", removed)
	}
	for _, path := range []string{
		lay.HomePath("profiles", "p"), filepath.Join(lay.HomePath("memories"), "a"), state.ClipboardPath(lay.Home, "a"),
		state.ConversationPath(lay.Home, "a"), lay.HomePath("backup", "a-2026-10-01_120000.zip"), lay.HomePath("backup", "p-2026-10-02_120000.zip"),
	} {
		if fileExists(path) {
			t.Errorf("%s is still there", path)
		}
	}
	if _, found, _ := state.Load(lay.Home, "a"); found {
		t.Error("sandbox a's record is still there")
	}
	for _, path := range []string{
		lay.HomePath("profiles", "keep"), filepath.Join(lay.HomePath("memories"), "k"), state.ClipboardPath(lay.Home, "k"),
		lay.HomePath("backup", "k-2026-10-01_120000.zip"),
	} {
		if !fileExists(path) {
			t.Errorf("%s, of another profile, was deleted", path)
		}
	}
	if _, found, _ := state.Load(lay.Home, keep.inst.Name); !found {
		t.Error("another profile's sandbox record was deleted")
	}
}

// A record outliving its sandbox would hold the sandbox's published ports
// against every sandbox made after it.
func TestDeletingASandboxFreesItsPublishedPorts(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	base := freeBase(t, 4)
	s := sandboxRecord(t, lay, "old", "p", state.PublishRecord{Name: "diffity", ContainerPort: base, HostPort: base})
	var removed []string
	if err := (removal{sandboxes: []inventorySandbox{s}}).run(lay, remover(t, &removed)); err != nil {
		t.Fatal(err)
	}
	mappings, err := allocatePublish(lay.Home, "new", publishOne(base))
	if err != nil {
		t.Fatal(err)
	}
	if got := hostPorts(mappings); len(got) != 1 || got[0] != base {
		t.Errorf("host ports = %v, want [%d] — the deleted sandbox is still holding its port", got, base)
	}
}

// One wedged sandbox doesn't strand the rest of what was confirmed, and
// what couldn't go is named. Its profile stays, so deleting can be tried
// again; so does its record, as the sandbox is still there.
func TestARemovalCarriesOnPastAFailure(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	stuck := sandboxRecord(t, lay, "stuck", "p")
	first := sandboxRecord(t, lay, "first", "q")
	last := sandboxRecord(t, lay, "last", "q")
	d := removal{
		profiles:  []inventoryProfile{{name: "p", exists: true, sandboxes: []inventorySandbox{stuck}}},
		sandboxes: []inventorySandbox{first, last},
		unknown:   []string{"orphan"},
	}
	var removed []string
	err := d.run(lay, remover(t, &removed, "stuck"))
	if err == nil || !strings.Contains(err.Error(), "sandbox 'stuck'") || !strings.Contains(err.Error(), "profile 'p'") {
		t.Errorf("error = %v, want it to name the sandbox and profile left over", err)
	}
	if strings.Join(removed, ",") != "stuck,first,last,orphan" {
		t.Errorf("removed = %v, want every one tried", removed)
	}
	if !fileExists(lay.HomePath("profiles", "p")) {
		t.Error("profile p was deleted, though its sandbox couldn't be")
	}
	if _, found, _ := state.Load(lay.Home, "stuck"); !found {
		t.Error("'stuck' wasn't removed, so its record should remain")
	}
	for _, name := range []string{"first", "last"} {
		if _, found, _ := state.Load(lay.Home, name); found {
			t.Errorf("'%s' was removed but kept its record", name)
		}
	}
}

// A sandbox sbx no longer has is only a record: sbx isn't asked to remove
// it.
func TestARecordWhoseSandboxIsGoneIsJustForgotten(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	s := sandboxRecord(t, lay, "ghost", "p")
	s.inSbx = false
	var removed []string
	if err := (removal{sandboxes: []inventorySandbox{s}}).run(lay, remover(t, &removed)); err != nil || len(removed) != 0 {
		t.Errorf("run = %v, removed %v; want the record forgotten, sbx not asked", err, removed)
	}
	if _, found, _ := state.Load(lay.Home, "ghost"); found {
		t.Error("the record is still there")
	}
}

// A memory store that isn't a folder of the sandbox's own is never deleted.
func TestAMemoryStoreSharedWithOthersIsntDeleted(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	shared := filepath.Join(t.TempDir(), "everything")
	writeFile(t, filepath.Join(shared, "precious"), "x")
	s := sandboxRecord(t, lay, "a", "p")
	s.inst.MemoryStore = shared
	var removed []string
	if err := (removal{sandboxes: []inventorySandbox{s}}).run(lay, remover(t, &removed)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(shared, "precious")); err != nil {
		t.Error("a memory store that isn't the sandbox's own was deleted")
	}
}
