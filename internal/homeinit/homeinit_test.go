package homeinit

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"vibe/internal/layout"
)

func seedBundle(t *testing.T) layout.Layout {
	t.Helper()
	l := layout.New(t.TempDir(), t.TempDir())
	write(t, l.BundlePath("config.yaml"), "name: vibe\n")
	write(t, l.BundlePath("settings.yaml"), "memory: 12g\n")
	write(t, l.BundlePath("profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	write(t, l.BundlePath("profiles", "phoenix", "config.yaml"), "name: phoenix\n")
	write(t, l.BundlePath("defaults", "install-scripts", "01-a"), "echo a\n")
	write(t, l.BundlePath("library", "mysql", "install-scripts", "01-install-mysql"), "echo mysql\n")
	return l
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNeeded(t *testing.T) {
	l := seedBundle(t)
	if !Needed(l) {
		t.Fatal("an empty overlay needs seeding")
	}
	// state from an older vibe doesn't count as configuration
	if err := os.MkdirAll(filepath.Join(l.Home, "instances"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !Needed(l) {
		t.Error("an overlay holding only state still needs seeding")
	}
	write(t, filepath.Join(l.Home, "settings.yaml"), "memory: 24g\n")
	if Needed(l) {
		t.Error("an overlay with its own settings.yaml does not need seeding")
	}
	if err := os.RemoveAll(filepath.Join(l.Home, "settings.yaml")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(l.Home, "defaults", "install-scripts", "01-a"), "echo mine\n")
	if Needed(l) {
		t.Error("an overlay with its own defaults does not need seeding")
	}
	if err := os.RemoveAll(filepath.Join(l.Home, "defaults")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(l.Home, "library", "mysql", "install-scripts", "01-a"), "echo mine\n")
	if Needed(l) {
		t.Error("an overlay with its own library does not need seeding")
	}
	if Needed(layout.New("", l.Bundle)) {
		t.Error("with no overlay there is nothing to seed")
	}
}

func TestRunCopiesBaseFilesAndAcceptedProfiles(t *testing.T) {
	l := seedBundle(t)
	var asked []string
	res, err := Run(l, func(profile string) bool {
		asked = append(asked, profile)
		return profile == "yumbi"
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"phoenix", "yumbi"}; !reflect.DeepEqual(asked, want) {
		t.Errorf("asked about %v, want every bundled profile %v", asked, want)
	}
	if want := []string{"config.yaml", "settings.yaml", "defaults/", "library/"}; !reflect.DeepEqual(res.Files, want) {
		t.Errorf("copied %v, want %v", res.Files, want)
	}
	if _, err := os.Stat(l.HomePath("defaults", "install-scripts", "01-a")); err != nil {
		t.Errorf("defaults not copied into the overlay: %v", err)
	}
	if got, want := l.DefaultsDir(), l.HomePath("defaults"); got != want {
		t.Errorf("DefaultsDir = %s, want the seeded %s", got, want)
	}
	if _, err := os.Stat(l.HomePath("library", "mysql", "install-scripts", "01-install-mysql")); err != nil {
		t.Errorf("library not copied into the overlay: %v", err)
	}
	if got, want := l.LibraryDir(), l.HomePath("library"); got != want {
		t.Errorf("LibraryDir = %s, want the seeded %s", got, want)
	}
	if want := []string{"yumbi"}; !reflect.DeepEqual(res.Profiles, want) {
		t.Errorf("copied profiles %v, want %v", res.Profiles, want)
	}
	if _, err := os.Stat(l.HomePath("profiles", "yumbi", "config.yaml")); err != nil {
		t.Errorf("accepted profile not copied: %v", err)
	}
	if _, err := os.Stat(l.HomePath("profiles", "phoenix")); err == nil {
		t.Error("declined profile was copied anyway")
	}
	// the overlay is now seeded, and the declined profile still resolves to
	// the bundle
	if Needed(l) {
		t.Error("still needs seeding after Run")
	}
	if got, want := l.ProfileDir("phoenix"), l.BundlePath("profiles", "phoenix"); got != want {
		t.Errorf("ProfileDir(phoenix) = %s, want %s", got, want)
	}
}

func TestRunWithoutAsking(t *testing.T) {
	l := seedBundle(t)
	res, err := Run(l, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Profiles) != 0 {
		t.Errorf("copied %v with no way to ask", res.Profiles)
	}
	if info, err := os.Stat(l.HomePath("profiles")); err != nil || !info.IsDir() {
		t.Errorf("expected an empty profiles/ in the overlay: %v", err)
	}
	if Needed(l) {
		t.Error("still needs seeding after Run")
	}
}
