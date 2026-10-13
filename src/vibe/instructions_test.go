package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

func instructionsAdmin(t *testing.T) (*companionAdmin, layout.Layout) {
	t.Helper()
	lay := layout.New(t.TempDir(), t.TempDir())
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	return &companionAdmin{lay: lay, name: "box", target: t.TempDir()}, lay
}

func TestTheTabShowsBothFilesAndWhatTheAgentDoesWithThem(t *testing.T) {
	a, lay := instructionsAdmin(t)
	writeFile(t, lay.HomePath("agent-instructions.md"), "British spelling.\n")
	got := a.Instructions()
	if len(got.Files) != 2 || got.Files[0].Which != "universal" || got.Files[0].Text != "British spelling.\n" {
		t.Fatalf("files = %+v", got.Files)
	}
	if !got.Files[1].Editable || got.Files[1].Label != "For profile 'p'" {
		t.Errorf("project file = %+v", got.Files[1])
	}
	if !got.Applies || !got.CanOptimise || got.Agent != "claude" {
		t.Errorf("claude: %+v", got)
	}
}

func TestSavingAFileNobodyElseChangedJustSavesIt(t *testing.T) {
	a, lay := instructionsAdmin(t)
	a.listSandboxes = func(context.Context) ([]sbxrun.Status, error) {
		return []sbxrun.Status{{Name: "box", Running: false}}, nil
	}
	path := lay.HomePath("profiles", "p", "agent-instructions.md")
	base := a.Instructions().Files[1]
	saved, err := a.SaveInstructions(companion.AdminSave{Which: "project", Text: "Use Go.\n", Base: base.Text, Version: base.Version})
	if err != nil || !saved.Saved || saved.Conflict {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	if got := readText(path); got != "Use Go.\n" {
		t.Errorf("on disk: %q", got)
	}
	// Stopped: it gets them when it next starts.
	if strings.Join(saved.NotRunning, ",") != "box" || len(saved.Failed) != 0 {
		t.Errorf("not running = %v, failed = %v", saved.NotRunning, saved.Failed)
	}
}

func TestASaveSaysSoWhenSbxCantSayWhatIsRunning(t *testing.T) {
	a, lay := instructionsAdmin(t)
	a.listSandboxes = func(context.Context) ([]sbxrun.Status, error) {
		return nil, errors.New("sbx is wedged")
	}
	base := a.Instructions().Files[0]
	saved, err := a.SaveInstructions(companion.AdminSave{Which: "universal", Text: "x\n", Base: base.Text, Version: base.Version})
	if err != nil || !saved.Saved {
		t.Fatalf("the save itself: %+v, %v", saved, err)
	}
	if got := readText(lay.HomePath("agent-instructions.md")); got != "x\n" {
		t.Errorf("on disk: %q", got)
	}
	// Not "not running": vibe doesn't know, and says why.
	if len(saved.NotRunning) != 0 || !strings.Contains(saved.Failed["box"], "sbx is wedged") {
		t.Errorf("not running = %v, failed = %v", saved.NotRunning, saved.Failed)
	}
}

func TestAFileChangedOnDiskIsNotOverwrittenUnasked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("merging needs git")
	}
	a, lay := instructionsAdmin(t)
	path := lay.HomePath("agent-instructions.md")
	writeFile(t, path, "one\ntwo\nthree\nfour\nfive\n")
	base := a.Instructions().Files[0]
	// Someone edits it in their editor; the page edits another part.
	writeFile(t, path, "ONE\ntwo\nthree\nfour\nfive\n")
	mine := "one\ntwo\nthree\nfour\nFIVE (page)\n"

	saved, err := a.SaveInstructions(companion.AdminSave{Which: "universal", Text: mine, Base: base.Text, Version: base.Version})
	if err != nil || saved.Saved || !saved.Conflict || !saved.CanMerge || saved.DiskText != "ONE\ntwo\nthree\nfour\nfive\n" {
		t.Fatalf("unasked: %+v, %v", saved, err)
	}
	if got := readText(path); got != "ONE\ntwo\nthree\nfour\nfive\n" {
		t.Errorf("the file was written anyway: %q", got)
	}

	saved, err = a.SaveInstructions(companion.AdminSave{Which: "universal", Text: mine, Base: base.Text, Version: base.Version, Resolve: "merge"})
	if err != nil || !saved.Saved || readText(path) != "ONE\ntwo\nthree\nfour\nFIVE (page)\n" {
		t.Errorf("merge: %+v, %v, on disk %q", saved, err, readText(path))
	}

	stale := a.Instructions().Files[0]
	writeFile(t, path, "someone else's\n")
	saved, err = a.SaveInstructions(companion.AdminSave{Which: "universal", Text: "mine\n", Base: stale.Text, Version: stale.Version, Resolve: "overwrite"})
	if err != nil || !saved.Saved || readText(path) != "mine\n" {
		t.Errorf("overwrite: %+v, %v, on disk %q", saved, err, readText(path))
	}
}

func TestABundledProfilesFileCantBeSavedFromTheTab(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	state.Save(lay.Home, state.Instance{Name: "box", Profile: "shipped", Agent: "claude"})
	writeFile(t, lay.BundlePath("profiles", "shipped", "config.yaml"), "name: shipped\n")
	a := &companionAdmin{lay: lay, name: "box", target: t.TempDir()}
	project := a.Instructions().Files[1]
	if project.Editable || project.Note == "" {
		t.Errorf("project = %+v", project)
	}
	if _, err := a.SaveInstructions(companion.AdminSave{Which: "project", Text: "x", Version: project.Version}); err == nil {
		t.Error("a save was let through")
	}
	if _, err := os.Stat(lay.HomePath("profiles", "shipped")); !os.IsNotExist(err) {
		t.Errorf("a folder was made in ~/.vibe for the bundled profile: %v", err)
	}
}

func TestWhatVibeSaysDuringASessionWaitsForItToEnd(t *testing.T) {
	defer setAttached(false)
	setAttached(true)
	if !holdNote("one") || !holdNote("two") {
		t.Fatal("not held while attached")
	}
	attachedMu.Lock()
	held := strings.Join(heldNotes, ",")
	attachedMu.Unlock()
	if held != "one,two" {
		t.Errorf("held = %s", held)
	}
	setAttached(false)
	if holdNote("three") {
		t.Error("held while nothing is attached")
	}
	attachedMu.Lock()
	defer attachedMu.Unlock()
	if len(heldNotes) != 0 {
		t.Errorf("not printed and cleared on detaching: %v", heldNotes)
	}
}
