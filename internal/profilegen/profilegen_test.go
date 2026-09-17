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
