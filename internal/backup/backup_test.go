package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateKeepsOnlyTheNewestThreeAndRestoreReplacesTheProfile(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	os.MkdirAll(filepath.Join(profile, "sub"), 0o755)
	os.WriteFile(filepath.Join(profile, "config.yaml"), []byte("one"), 0o644)
	os.WriteFile(filepath.Join(profile, "sub", "a.txt"), []byte("a"), 0o644)
	dir := filepath.Join(root, "backup")

	base := time.Date(2026, 1, 2, 13, 42, 55, 0, time.Local)
	var first string
	for i := 0; i < 5; i++ {
		p, err := Create(dir, "vibe", profile, base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p
		}
	}
	// another sandbox's backups, and a lookalike, are left alone
	Create(dir, "other", profile, base)
	os.WriteFile(filepath.Join(dir, "vibe-extra-2026-01-02_134255.zip"), nil, 0o644)

	got, err := List(dir, "vibe")
	if err != nil || len(got) != 3 {
		t.Fatalf("want 3 backups, got %v (%v)", got, err)
	}
	if filepath.Base(got[0].Path) != "vibe-2026-01-02_134655.zip" {
		t.Errorf("newest first expected, got %s", got[0].Path)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("oldest should have been pruned")
	}

	os.WriteFile(filepath.Join(profile, "extra.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(profile, "config.yaml"), []byte("two"), 0o644)
	if err := Restore(got[0].Path, profile); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(profile, "config.yaml")); string(b) != "one" {
		t.Errorf("config not restored: %q", b)
	}
	if _, err := os.Stat(filepath.Join(profile, "extra.txt")); !os.IsNotExist(err) {
		t.Errorf("extra file should be gone")
	}
	if _, err := os.Stat(filepath.Join(profile, "sub", "a.txt")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}
}

func TestListWithNoBackupDir(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "nope"), "vibe")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}
