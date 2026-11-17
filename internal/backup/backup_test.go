package backup

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

// A backup is a zip on disk: one with an entry reaching out of the profile
// ("../") is refused, and nothing is written outside it.
func TestRestoreRefusesAnEntryOutsideTheProfile(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	os.MkdirAll(profile, 0o755)
	zipPath := filepath.Join(root, "bad.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"config.yaml", "../escaped.txt"} {
		w, _ := zw.Create(name)
		w.Write([]byte("x"))
	}
	zw.Close()
	f.Close()

	if err := Restore(zipPath, profile); err == nil {
		t.Error("a backup reaching out of the profile was restored")
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.txt")); err == nil {
		t.Error("an entry was written outside the profile")
	}
}

func TestCreateLeavingOutSkipsThoseTopLevelFoldersOnly(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, ".vibe")
	for _, f := range []string{"settings.yaml", "profiles/a/config.yaml", "logs/a.log", "running/1.lock", "library/logs/kept.txt"} {
		os.MkdirAll(filepath.Dir(filepath.Join(home, f)), 0o755)
		os.WriteFile(filepath.Join(home, f), []byte(f), 0o644)
	}
	p, err := CreateLeavingOut(root, ".vibe-backup", home, time.Date(2026, 11, 17, 9, 5, 0, 0, time.Local), []string{"logs", "running"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ".vibe-backup-2026-11-17_090500.zip"); p != want {
		t.Errorf("backup at %s, want %s", p, want)
	}
	r, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var got []string
	for _, f := range r.File {
		got = append(got, f.Name)
	}
	want := []string{"library/logs/kept.txt", "profiles/a/config.yaml", "settings.yaml"}
	if !reflect.DeepEqual(sorted(got), want) {
		t.Errorf("backup holds %v, want %v", got, want)
	}
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}
