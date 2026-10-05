package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A backup interrupted part-way leaves some memories overwritten and some
// new ones added; restoring puts the store back exactly as it was.
func TestSnapshotRestoreUndoesAPartialBackup(t *testing.T) {
	store := filepath.Join(t.TempDir(), "proj")
	writeFile(t, filepath.Join(store, "-a", "memory", "MEMORY.md"), "original")
	writeFile(t, filepath.Join(store, "-a", "memory", "kept.md"), "kept")
	then := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(store, "-a", "memory", "kept.md"), then, then); err != nil {
		t.Fatal(err)
	}

	snap, err := TakeSnapshot(store)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	writeFile(t, filepath.Join(store, "-a", "memory", "MEMORY.md"), "half-copied")
	writeFile(t, filepath.Join(store, "-b", "memory", "new.md"), "new")
	os.Remove(filepath.Join(store, "-a", "memory", "kept.md"))

	if err := snap.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := readFile(t, filepath.Join(store, "-a", "memory", "MEMORY.md")); got != "original" {
		t.Errorf("MEMORY.md = %q, want the original", got)
	}
	if got := readFile(t, filepath.Join(store, "-a", "memory", "kept.md")); got != "kept" {
		t.Errorf("kept.md = %q, want it back", got)
	}
	// Back as it was means when it was last changed, too.
	if info, err := os.Stat(filepath.Join(store, "-a", "memory", "kept.md")); err != nil || !info.ModTime().Equal(then) {
		t.Errorf("kept.md restored as modified %v (%v), want %s", info.ModTime(), err, then)
	}
	if _, err := os.Stat(filepath.Join(store, "-b")); !os.IsNotExist(err) {
		t.Errorf("memories added by the interrupted backup survived the restore")
	}
	assertOnlyStoreLeft(t, store)
}

// A store that didn't exist before the backup is restored to empty — but
// still there, since the sandbox has it mounted.
func TestSnapshotOfAMissingStoreRestoresToEmpty(t *testing.T) {
	store := filepath.Join(t.TempDir(), "proj")
	snap, err := TakeSnapshot(store)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	writeFile(t, filepath.Join(store, "-a", "memory", "MEMORY.md"), "half-copied")

	if err := snap.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	entries, err := os.ReadDir(store)
	if err != nil {
		t.Fatalf("store gone after restore: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("store = %v, want it empty", entries)
	}
	assertOnlyStoreLeft(t, store)
}

func TestSnapshotDiscardLeavesTheStoreAlone(t *testing.T) {
	store := filepath.Join(t.TempDir(), "proj")
	writeFile(t, filepath.Join(store, "MEMORY.md"), "original")
	snap, err := TakeSnapshot(store)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	writeFile(t, filepath.Join(store, "MEMORY.md"), "backed up")
	snap.Discard()
	if got := readFile(t, filepath.Join(store, "MEMORY.md")); got != "backed up" {
		t.Errorf("MEMORY.md = %q, want the backed-up copy", got)
	}
	assertOnlyStoreLeft(t, store)
}

// assertOnlyStoreLeft checks the snapshot's copy has been cleaned away.
func assertOnlyStoreLeft(t *testing.T, store string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(store))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(store) {
			t.Errorf("left behind beside the store: %s", e.Name())
		}
	}
}

func TestBackupContextStopsWhenCancelled(t *testing.T) {
	fakeSbx(t, false)
	agent := filepath.Join(t.TempDir(), "projects")
	useAgentPath(t, agent)
	writeFile(t, filepath.Join(agent, "-proj", "memory", "MEMORY.md"), "a memory")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := filepath.Join(t.TempDir(), "store")
	saved, err := BackupContext(ctx, "any", store)
	if saved || !errors.Is(err, context.Canceled) {
		t.Errorf("BackupContext after cancel = %v, %v; want false, context.Canceled", saved, err)
	}
	if _, err := os.Stat(filepath.Join(store, "-proj")); !os.IsNotExist(err) {
		t.Errorf("a cancelled backup still copied memories")
	}
}
