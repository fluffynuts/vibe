package layout

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFilePrefersTheOverlay(t *testing.T) {
	l := New(t.TempDir(), t.TempDir())
	write(t, l.BundlePath("settings.yaml"), "memory: 12g\n")
	write(t, l.BundlePath("config.yaml"), "name: vibe\n")
	write(t, l.HomePath("settings.yaml"), "memory: 24g\n")

	if got, want := l.File("settings.yaml"), l.HomePath("settings.yaml"); got != want {
		t.Errorf("File(settings.yaml) = %s, want the overlay's %s", got, want)
	}
	if got, want := l.File("config.yaml"), l.BundlePath("config.yaml"); got != want {
		t.Errorf("File(config.yaml) = %s, want the bundle's %s", got, want)
	}
	// a file in neither layer resolves to the bundle, for callers that treat
	// a missing file as "not configured"
	if got, want := l.File("nope.yaml"), l.BundlePath("nope.yaml"); got != want {
		t.Errorf("File(nope.yaml) = %s, want %s", got, want)
	}
}

func TestDefaultsDirIsAWholeDirectoryOverride(t *testing.T) {
	l := New(t.TempDir(), t.TempDir())
	write(t, l.BundlePath("defaults", "install-scripts", "01-a"), "echo a\n")
	write(t, l.BundlePath("defaults", "install-scripts", "02-b"), "echo b\n")

	if got, want := l.DefaultsDir(), l.BundlePath("defaults"); got != want {
		t.Errorf("DefaultsDir = %s, want %s", got, want)
	}

	// once the overlay has a defaults directory it wins whole: the bundle's
	// 02-b must not show through
	write(t, l.HomePath("defaults", "install-scripts", "01-a"), "echo mine\n")
	if got, want := l.DefaultsDir(), l.HomePath("defaults"); got != want {
		t.Errorf("DefaultsDir = %s, want the overlay's %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(l.DefaultsDir(), "install-scripts", "02-b")); err == nil {
		t.Error("the bundle's 02-b is still visible — the override is not whole")
	}
}

// TestFeatureDirOverridesOnePerFeature checks that a library feature
// overrides individually, unlike defaults: the overlay having its own
// mysql must not hide the bundle's rabbitmq.
func TestFeatureDirOverridesOnePerFeature(t *testing.T) {
	l := New(t.TempDir(), t.TempDir())
	write(t, l.BundlePath("library", "mysql", "install-scripts", "01-a"), "echo a\n")
	write(t, l.BundlePath("library", "rabbitmq", "install-scripts", "01-b"), "echo b\n")

	if got, want := l.FeatureDir("mysql"), l.BundlePath("library", "mysql"); got != want {
		t.Errorf("FeatureDir(mysql) = %s, want the bundle's %s", got, want)
	}

	// the overlay overriding mysql alone must not hide the bundle's rabbitmq
	write(t, l.HomePath("library", "mysql", "install-scripts", "01-a"), "echo mine\n")
	if got, want := l.FeatureDir("mysql"), l.HomePath("library", "mysql"); got != want {
		t.Errorf("FeatureDir(mysql) = %s, want the overlay's %s", got, want)
	}
	if got, want := l.FeatureDir("rabbitmq"), l.BundlePath("library", "rabbitmq"); got != want {
		t.Errorf("FeatureDir(rabbitmq) = %s, want the bundle's %s — one feature's override must not hide another", got, want)
	}
}

// TestFeaturesUnionsBothLayers checks Features() lists every feature from
// either layer, deduplicated, without requiring the overlay to carry a
// full mirror of the bundle's library.
func TestFeaturesUnionsBothLayers(t *testing.T) {
	l := New(t.TempDir(), t.TempDir())
	write(t, l.BundlePath("library", "mysql", "install-scripts", "01-a"), "echo a\n")
	write(t, l.BundlePath("library", "rabbitmq", "install-scripts", "01-b"), "echo b\n")
	write(t, l.HomePath("library", "mysql", "install-scripts", "01-a"), "echo mine\n")
	write(t, l.HomePath("library", "postgres", "install-scripts", "01-c"), "echo c\n")

	got := l.Features()
	want := []string{"mysql", "postgres", "rabbitmq"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Features = %v, want %v", got, want)
	}
}

func TestProfileDirIsAWholeDirectoryOverride(t *testing.T) {
	l := New(t.TempDir(), t.TempDir())
	write(t, l.BundlePath("profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	write(t, l.BundlePath("profiles", "yumbi", "settings.yaml"), "nugetDir: ~/.nuget\n")

	if got, want := l.ProfileDir("yumbi"), l.BundlePath("profiles", "yumbi"); got != want {
		t.Errorf("ProfileDir = %s, want %s", got, want)
	}

	// once the overlay has a directory of that name it wins whole: the
	// bundle's settings.yaml must not leak through
	write(t, l.HomePath("profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	if got, want := l.ProfileDir("yumbi"), l.HomePath("profiles", "yumbi"); got != want {
		t.Errorf("ProfileDir = %s, want the overlay's %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(l.ProfileDir("yumbi"), "settings.yaml")); err == nil {
		t.Error("the bundle's settings.yaml is still visible — the override is not whole")
	}
}

func TestProfilesUnionAndExistence(t *testing.T) {
	l := New(t.TempDir(), t.TempDir())
	write(t, l.BundlePath("profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	write(t, l.HomePath("profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	write(t, l.HomePath("profiles", "phoenix", "config.yaml"), "name: phoenix\n")
	// a directory with no config.yaml is not a profile
	if err := os.MkdirAll(l.HomePath("profiles", "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}

	want := []string{"phoenix", "yumbi"}
	if got := l.Profiles(); !reflect.DeepEqual(got, want) {
		t.Errorf("Profiles = %v, want %v", got, want)
	}
	if got, want := l.BundleProfiles(), []string{"yumbi"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BundleProfiles = %v, want %v", got, want)
	}
	if !l.ProfileExists("phoenix") || l.ProfileExists("scratch") || l.ProfileExists("nope") {
		t.Error("ProfileExists disagrees with Profiles")
	}
}

func TestNoOverlay(t *testing.T) {
	l := New("", t.TempDir())
	write(t, l.BundlePath("settings.yaml"), "memory: 12g\n")
	write(t, l.BundlePath("profiles", "yumbi", "config.yaml"), "name: yumbi\n")

	if got, want := l.File("settings.yaml"), l.BundlePath("settings.yaml"); got != want {
		t.Errorf("File = %s, want %s", got, want)
	}
	if got, want := l.NewProfileDir("x"), l.BundlePath("profiles", "x"); got != want {
		t.Errorf("NewProfileDir = %s, want the bundle's %s", got, want)
	}
	if got, want := l.DefaultsDir(), l.BundlePath("defaults"); got != want {
		t.Errorf("DefaultsDir = %s, want the bundle's %s", got, want)
	}
	if got, want := l.Profiles(), []string{"yumbi"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Profiles = %v, want %v", got, want)
	}
}
