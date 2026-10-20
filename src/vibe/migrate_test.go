package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/running"
	"vibe/internal/state"
)

func TestAProjectsFolderIsNamedAsTheAgentNamesIt(t *testing.T) {
	for path, want := range map[string]string{
		"/home/user/code/opensource/vibe": "-home-user-code-opensource-vibe",
		"/home/me/code/my.app_2":          "-home-me-code-my-app-2",
		"/tmp":                            "-tmp",
	} {
		if got := projectFolder(path); got != want {
			t.Errorf("projectFolder(%q) = %q, want %q", path, got, want)
		}
	}
}

// tgzOf is a gzipped tar holding files, by name.
func tgzOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func namesIn(t *testing.T, tgz []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		out[h.Name] = string(body)
	}
}

func TestMigratingRecordsTakesTheAgentsMemoriesAndConversationAlong(t *testing.T) {
	home := t.TempDir()
	from, to := "/code/old-place", "/work/new-place"
	oldSlug, newSlug := projectFolder(from), projectFolder(to)
	store := filepath.Join(home, "memories", "old-place")
	if err := state.Save(home, state.Instance{Name: "old-place", Target: from, MemoryStore: store}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(store, oldSlug, "memory", "MEMORY.md"), "remember this")
	writeFile(t, filepath.Join(store, "-some-other-project", "memory", "MEMORY.md"), "not this one's")
	writeFile(t, headlessLog(home, "old-place"), "a log")
	writeFile(t, state.TranscriptPath(home, "old-place"), string(tgzOf(t, map[string]string{
		"projects/" + oldSlug + "/abc.jsonl": "the conversation",
	})))

	if err := migrateRecords(home, "old-place", "new-place", from, to); err != nil {
		t.Fatal(err)
	}

	inst, found, err := state.Load(home, "new-place")
	if err != nil || !found {
		t.Fatalf("no record for the new name: %v", err)
	}
	newStore := filepath.Join(home, "memories", "new-place")
	if inst.Target != to || inst.MemoryStore != newStore {
		t.Errorf("record: target %q, memory store %q", inst.Target, inst.MemoryStore)
	}
	if data, err := os.ReadFile(filepath.Join(newStore, newSlug, "memory", "MEMORY.md")); err != nil || string(data) != "remember this" {
		t.Errorf("the project's memories weren't filed under the new folder's name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newStore, "-some-other-project")); err != nil {
		t.Error("another project's memories were lost")
	}
	if _, err := os.Stat(headlessLog(home, "new-place")); err != nil {
		t.Error("the log wasn't moved")
	}
	tgz, err := os.ReadFile(state.TranscriptPath(home, "new-place"))
	if err != nil {
		t.Fatal(err)
	}
	if got := namesIn(t, tgz); got["projects/"+newSlug+"/abc.jsonl"] != "the conversation" {
		t.Errorf("the transcript holds %v, want it under the new folder's name", got)
	}
}

func TestMigratingToAFolderOfTheSameNameKeepsTheName(t *testing.T) {
	home := t.TempDir()
	from, to := "/code/app", "/work/app"
	store := filepath.Join(home, "memories", "app")
	if err := state.Save(home, state.Instance{Name: "app", Target: from, MemoryStore: store}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(store, projectFolder(from), "memory", "MEMORY.md"), "remember this")
	if err := migrateRecords(home, "app", "app", from, to); err != nil {
		t.Fatal(err)
	}
	if inst, _, _ := state.Load(home, "app"); inst.Target != to || inst.MemoryStore != store {
		t.Errorf("record: %+v", inst)
	}
	if _, err := os.Stat(filepath.Join(store, projectFolder(to), "memory", "MEMORY.md")); err != nil {
		t.Error("the memories weren't filed under the new folder's name")
	}
}

func TestAMigrationIsRefusedBeforeAnythingChanges(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	from := filepath.Join(root, "app")
	os.MkdirAll(from, 0o755)
	os.MkdirAll(filepath.Join(root, "taken"), 0o755)
	if err := state.Save(home, state.Instance{Name: "app", Target: from}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(home, state.Instance{Name: "other", Target: "/somewhere/other"}); err != nil {
		t.Fatal(err)
	}
	noSbx := func(string) (bool, error) { return false, nil }
	for _, tc := range []struct {
		why, name, to string
		sbxHas        func(string) (bool, error)
		want          string
	}{
		{"no record", "nope", filepath.Join(root, "x"), noSbx, "no sandbox for"},
		{"already there", "app", filepath.Join(root, "taken"), noSbx, "already exists"},
		{"no parent", "app", filepath.Join(root, "missing", "app"), noSbx, "doesn't exist"},
		{"into itself", "app", filepath.Join(from, "sub"), noSbx, "inside it"},
		{"into itself, under a name starting ..", "app", filepath.Join(from, "..sub"), noSbx, "inside it"},
		{"name taken by a record", "app", filepath.Join(root, "other"), noSbx, "'other'"},
		{"name taken in sbx", "app", filepath.Join(root, "made-by-hand"), func(n string) (bool, error) { return n == "made-by-hand", nil }, "'made-by-hand'"},
		{"sbx can't tell", "app", filepath.Join(root, "new-app"), func(string) (bool, error) { return false, errors.New("sbx ls --json: daemon not running") }, "daemon not running"},
	} {
		_, err := checkMigration(home, tc.name, from, tc.to, tc.sbxHas)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want it to say %q", tc.why, err, tc.want)
		}
	}
	if name, err := checkMigration(home, "app", from, filepath.Join(root, "new-app"), noSbx); err != nil || name != "new-app" {
		t.Errorf("a good move: %q, %v", name, err)
	}
}

// migrateStandIns stand in for the user's answer, for sbx (with no
// sandboxes of its own) and for the rebuild, which runs the relocation as
// the real one does, and records what it was asked to rebuild and what it
// was relocated to.
func migrateStandIns(t *testing.T, answer bool) (rebuilt *[]string) {
	t.Helper()
	sbxListing(t, "")
	rebuilt = &[]string{}
	oldConfirm, oldRebuild := confirmMigration, migrateRebuild
	t.Cleanup(func() { confirmMigration, migrateRebuild = oldConfirm, oldRebuild })
	confirmMigration = func(bool, []string) bool { return answer }
	migrateRebuild = func(_ context.Context, _ layout.Layout, _ cliargs.Args, name, target string, _, _ bool, relocate relocation) error {
		newName, newTarget, err := relocate()
		*rebuilt = append(*rebuilt, name+" "+target, newName+" "+newTarget)
		return err
	}
	return rebuilt
}

func TestMigratingMovesTheFolderAndRebuildsItsSandboxThere(t *testing.T) {
	lay := updateLayout(t)
	root := t.TempDir()
	from, to := filepath.Join(root, "app"), filepath.Join(root, "moved-app")
	writeFile(t, filepath.Join(from, "main.go"), "package main")
	if err := state.Save(lay.Home, state.Instance{Name: "app", Profile: "p", Target: from}); err != nil {
		t.Fatal(err)
	}
	rebuilt := migrateStandIns(t, true)

	if err := doMigrate(lay, cliargs.Args{}, "app", from, to); err != nil {
		t.Fatal(err)
	}
	if want := []string{"app " + from, "moved-app " + to}; strings.Join(*rebuilt, "|") != strings.Join(want, "|") {
		t.Errorf("rebuilt %v, want %v", *rebuilt, want)
	}
	if _, err := os.Stat(filepath.Join(to, "main.go")); err != nil {
		t.Error("the folder wasn't moved")
	}
	if _, err := os.Stat(from); err == nil {
		t.Error("the old folder is still there")
	}
	if inst, found, _ := state.Load(lay.Home, "moved-app"); !found || inst.Target != to {
		t.Errorf("the record wasn't moved: %+v", inst)
	}
}

func TestAMigrationNotAgreedToChangesNothing(t *testing.T) {
	lay := updateLayout(t)
	root := t.TempDir()
	from, to := filepath.Join(root, "app"), filepath.Join(root, "moved-app")
	writeFile(t, filepath.Join(from, "main.go"), "package main")
	if err := state.Save(lay.Home, state.Instance{Name: "app", Profile: "p", Target: from}); err != nil {
		t.Fatal(err)
	}
	rebuilt := migrateStandIns(t, false)
	if err := doMigrate(lay, cliargs.Args{}, "app", from, to); err == nil || !strings.Contains(err.Error(), "nothing moved") {
		t.Errorf("not agreed to: %v", err)
	}
	if len(*rebuilt) != 0 {
		t.Errorf("rebuilt anyway: %v", *rebuilt)
	}
	if _, err := os.Stat(filepath.Join(from, "main.go")); err != nil {
		t.Error("the folder was moved anyway")
	}
}

func TestAFolderAVibeIsRunningInIsntMigrated(t *testing.T) {
	lay := updateLayout(t)
	root := t.TempDir()
	from, to := filepath.Join(root, "app"), filepath.Join(root, "moved-app")
	writeFile(t, filepath.Join(from, "main.go"), "package main")
	if err := state.Save(lay.Home, state.Instance{Name: "app", Profile: "p", Target: from}); err != nil {
		t.Fatal(err)
	}
	rebuilt := migrateStandIns(t, true)
	old := folderClaims
	t.Cleanup(func() { folderClaims = old })
	folderClaims = func(string, string) []running.Claim { return []running.Claim{{PID: 4242}} }
	if err := doMigrate(lay, cliargs.Args{}, "app", from, to); err == nil || !strings.Contains(err.Error(), "4242") {
		t.Errorf("with a vibe running there: %v", err)
	}
	if len(*rebuilt) != 0 {
		t.Errorf("rebuilt anyway: %v", *rebuilt)
	}
}

func TestAMoveToAnotherDiskIsRefused(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	from := filepath.Join(root, "app")
	os.MkdirAll(from, 0o755)
	if err := state.Save(home, state.Instance{Name: "app", Target: from}); err != nil {
		t.Fatal(err)
	}
	old := sameFilesystem
	t.Cleanup(func() { sameFilesystem = old })
	sameFilesystem = func(string, string) bool { return false }
	if _, err := checkMigration(home, "app", from, filepath.Join(root, "new-app"), func(string) (bool, error) { return false, nil }); err == nil || !strings.Contains(err.Error(), "another disk") {
		t.Errorf("across disks: %v", err)
	}
}

// What's asked says everything vibe keeps for the sandbox goes along.
func TestAMigrationSaysItsReviewsGoAlong(t *testing.T) {
	lay := updateLayout(t)
	root := t.TempDir()
	from, to := filepath.Join(root, "app"), filepath.Join(root, "moved-app")
	writeFile(t, filepath.Join(from, "main.go"), "package main")
	if err := state.Save(lay.Home, state.Instance{Name: "app", Profile: "p", Target: from}); err != nil {
		t.Fatal(err)
	}
	migrateStandIns(t, false)
	var asked []string
	confirmMigration = func(_ bool, steps []string) bool { asked = steps; return false }
	doMigrate(lay, cliargs.Args{}, "app", from, to)
	if said := strings.Join(asked, "\n"); !strings.Contains(said, "reviews") {
		t.Errorf("asked:\n%s", said)
	}
}
