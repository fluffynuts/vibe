package profilegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/layout"
)

// testLayout builds a throwaway overlay + bundle, the bundle holding one
// "yumbi"-shaped profile.
func testLayout(t *testing.T) layout.Layout {
	t.Helper()
	l := layout.New(t.TempDir(), t.TempDir())
	dir := l.BundlePath("profiles", "yumbi")
	if err := os.MkdirAll(filepath.Join(dir, "install-scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "config.yaml"), "# a comment\nname: yumbi\ndisplayName: Yumbi\n", 0o644)
	write(t, filepath.Join(dir, "settings.yaml"), "nugetDir: ~/.nuget\n", 0o644)
	write(t, filepath.Join(dir, "install-scripts", "01-hello"), "#!/bin/sh\necho hi\n", 0o755)
	return l
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestCreateBlankLandsInTheOverlay(t *testing.T) {
	l := testLayout(t)
	dir, err := CreateBlank(l, "phoenix")
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "phoenix"); dir != want {
		t.Errorf("created in %s, want %s", dir, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: phoenix") {
		t.Errorf("blank config.yaml does not name the profile:\n%s", data)
	}
	for _, sub := range []string{"install-scripts", "agent-files"} {
		if info, err := os.Stat(filepath.Join(dir, sub)); err != nil || !info.IsDir() {
			t.Errorf("expected %s/ in the blank profile: %v", sub, err)
		}
	}
	if !l.ProfileExists("phoenix") {
		t.Error("the created profile is not visible through the layout")
	}
}

func TestCopyFromBundleIntoTheOverlay(t *testing.T) {
	l := testLayout(t)
	dir, err := CopyFrom(l, "yumbi", "yumbi-two")
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "yumbi-two"); dir != want {
		t.Errorf("created in %s, want %s", dir, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: yumbi-two") {
		t.Errorf("copied config.yaml was not renamed:\n%s", data)
	}
	if !strings.Contains(string(data), "# a comment") || !strings.Contains(string(data), "displayName: Yumbi") {
		t.Errorf("copy lost comments or other fields:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.yaml")); err != nil {
		t.Errorf("settings.yaml not copied: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "install-scripts", "01-hello"))
	if err != nil {
		t.Fatalf("install script not copied: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("install script lost its executable bit: %v", info.Mode())
	}
	orig, err := os.ReadFile(l.BundlePath("profiles", "yumbi", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(orig), "name: yumbi\n") {
		t.Errorf("the bundled profile was modified:\n%s", orig)
	}
}

func TestImportToHomeKeepsTheName(t *testing.T) {
	l := testLayout(t)
	dir, err := ImportToHome(l, "yumbi")
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "yumbi"); dir != want {
		t.Errorf("imported to %s, want %s", dir, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: yumbi") {
		t.Errorf("import changed the profile:\n%s", data)
	}
	// the overlay copy is now the one that counts
	if got := l.ProfileDir("yumbi"); got != dir {
		t.Errorf("ProfileDir = %s, want the imported %s", got, dir)
	}
}

func TestCreateGuidedComposesTheChosenFeatures(t *testing.T) {
	l := testLayout(t)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho install-mysql\n", 0o644)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "agent-files", ".local", "bin"), "start-mysql"),
		"#!/bin/sh\necho start-mysql\n", 0o644)
	write(t, mkdirAndPath(t, l.BundlePath("library", "rabbitmq", "install-scripts"), "01-install-rabbitmq"),
		"#!/bin/sh\necho install-rabbitmq\n", 0o644)

	dir, err := CreateGuided(l, "phoenix", []string{"mysql", "rabbitmq"})
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "phoenix"); dir != want {
		t.Errorf("created in %s, want %s", dir, want)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "config.yaml")); err != nil {
		t.Errorf("expected a config.yaml naming the profile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "01-install-mysql")); err != nil {
		t.Errorf("expected mysql's install script first: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "02-install-rabbitmq")); err != nil {
		t.Errorf("expected rabbitmq's install script offset after mysql's: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-files", ".local", "bin", "start-mysql")); err != nil {
		t.Errorf("expected start-mysql copied in: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-files", ".local", "bin", "on-start")); err != nil {
		t.Errorf("expected a generated on-start: %v", err)
	}
}

// TestCreateGuidedUsesTheOverlaysOverrideForOneFeatureOnly checks the
// actual requirement: overriding a single feature in the overlay must not
// hide any other feature the bundle ships.
func TestCreateGuidedUsesTheOverlaysOverrideForOneFeatureOnly(t *testing.T) {
	l := testLayout(t)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho bundled-mysql\n", 0o644)
	write(t, mkdirAndPath(t, l.BundlePath("library", "rabbitmq", "install-scripts"), "01-install-rabbitmq"),
		"#!/bin/sh\necho install-rabbitmq\n", 0o644)
	write(t, mkdirAndPath(t, l.HomePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho my-own-mysql\n", 0o644)

	dir, err := CreateGuided(l, "phoenix", []string{"mysql", "rabbitmq"})
	if err != nil {
		t.Fatal(err)
	}
	mysqlScript, err := os.ReadFile(filepath.Join(dir, "install-scripts", "01-install-mysql"))
	if err != nil || string(mysqlScript) != "#!/bin/sh\necho my-own-mysql\n" {
		t.Errorf("expected the overlay's mysql override, got %q, %v", mysqlScript, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "02-install-rabbitmq")); err != nil {
		t.Errorf("expected the bundle's rabbitmq still used, unaffected by mysql's override: %v", err)
	}
}

func mkdirAndPath(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func TestCopyFromUnknownSource(t *testing.T) {
	if _, err := CopyFrom(testLayout(t), "nope", "phoenix"); err == nil {
		t.Fatal("expected an error copying from a missing profile")
	}
}

func TestValidateRejectsPathEscapes(t *testing.T) {
	for _, bad := range []string{"../evil", "a/b", "/abs", "", ".hidden", "Upper"} {
		if err := Validate(bad); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", bad)
		}
	}
	for _, good := range []string{"yumbi", "foo-browser", "a1", "x_y.z"} {
		if err := Validate(good); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", good, err)
		}
	}
}
