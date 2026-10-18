package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"vibe/internal/settings"
)

func foldersFor(t *testing.T, s settings.Settings) []folder {
	t.Helper()
	return sandboxFolders(t.TempDir(), "box", t.TempDir(), s, "claude", "")
}

func folderAt(folders []folder, path string) (folder, bool) {
	for _, f := range folders {
		if f.path == path {
			return f, true
		}
	}
	return folder{}, false
}

func hostNuget(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// By default only the host's package cache goes in, read-only: the rest of
// ~/.nuget holds feeds' credentials and plugins NuGet runs.
func TestNugetByDefaultGivesTheHostsPackagesReadOnlyAndNothingElse(t *testing.T) {
	nuget := hostNuget(t)
	folders := foldersFor(t, settings.Settings{NugetDir: nuget})
	packages := filepath.Join(nuget, "packages")
	f, ok := folderAt(folders, packages)
	if !ok || !f.readOnly || f.mount() != packages+":ro" {
		t.Fatalf("packages should be mounted read-only: %+v", folders)
	}
	if !slices.Contains(f.env, "SBX_HOST_NUGET_PACKAGES="+packages) {
		t.Errorf("the sandbox isn't told where the cache is: %v", f.env)
	}
	if _, whole := folderAt(folders, nuget); whole {
		t.Error("the whole nuget folder is mounted")
	}
	for _, f := range folders {
		for _, e := range f.env {
			if strings.HasPrefix(e, "SBX_HOST_NUGET=") {
				t.Errorf("%s would have the sandbox link all of ~/.nuget", e)
			}
		}
	}
}

func TestNugetSharedGivesTheWholeFolderReadWrite(t *testing.T) {
	nuget := hostNuget(t)
	folders := foldersFor(t, settings.Settings{NugetDir: nuget, Nuget: "shared"})
	f, ok := folderAt(folders, nuget)
	if !ok || f.readOnly {
		t.Fatalf("shared should mount the whole folder read-write: %+v", folders)
	}
	if !slices.Contains(f.env, "SBX_HOST_NUGET="+nuget) {
		t.Errorf("env = %v", f.env)
	}
	if !strings.Contains(f.note, "plant code") || !strings.Contains(f.note, "credentials") {
		t.Errorf("creating a shared sandbox should say what that lets the agent do: %q", f.note)
	}
}

func TestNugetMountsNothingWhenTheHostHasNoPackages(t *testing.T) {
	empty := t.TempDir()
	for _, mode := range []string{"", "fallback", "shared"} {
		s := settings.Settings{NugetDir: filepath.Join(empty, "nope"), Nuget: mode}
		for _, f := range foldersFor(t, s) {
			if strings.Contains(f.path, "nope") {
				t.Errorf("mode %q mounted %s, which isn't there", mode, f.path)
			}
		}
	}
}

func TestLoadSettingsRefusesAnUnknownNugetMode(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "nuget: everything\n")
	_, err := loadSettings(lay, "p")
	if err == nil || !strings.Contains(err.Error(), "everything") || !strings.Contains(err.Error(), "shared") {
		t.Errorf("want an error naming the value and the choices, got %v", err)
	}
}
