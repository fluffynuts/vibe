package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"vibe/internal/pathresolve"
	"vibe/internal/settings"
	"vibe/internal/state"
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

func packageFolders(t *testing.T, s settings.Settings) (inbox, view folder, found bool) {
	t.Helper()
	home := t.TempDir()
	mailbox := state.PackagesPath(home, "box")
	folders := sandboxFolders(home, "box", t.TempDir(), s, "claude", "")
	inbox, hasInbox := folderAt(folders, filepath.Join(mailbox, "inbox"))
	view, hasView := folderAt(folders, filepath.Join(mailbox, "view"))
	return inbox, view, hasInbox && hasView
}

// The agent asks for what it can't get by dropping a request in the inbox
// and reading the answer in the view, which it can't write: both folders are
// the host's, which acts on what's in them.
func TestNugetByDefaultGivesTheSandboxAPlaceToAskForPackages(t *testing.T) {
	inbox, view, found := packageFolders(t, settings.Settings{NugetDir: hostNuget(t)})
	if !found {
		t.Fatal("the package inbox and view aren't mounted")
	}
	if inbox.readOnly || !inbox.made {
		t.Errorf("the inbox is the agent's to write, and vibe makes it: %+v", inbox)
	}
	if !view.readOnly || !view.made {
		t.Errorf("the view must be read-only: %+v", view)
	}
	want := "VIBE_PACKAGES_DIR=" + filepath.Dir(inbox.path)
	if !slices.Contains(inbox.env, want) {
		t.Errorf("env %v lacks %s", inbox.env, want)
	}
}

func TestNoPackageRequestsWhereThereIsNothingToAskForOrSharingIsTotal(t *testing.T) {
	cases := map[string]settings.Settings{
		"shared":        {NugetDir: hostNuget(t), Nuget: "shared"},
		"no nuget":      {},
		"no host cache": {NugetDir: filepath.Join(t.TempDir(), "nope")},
	}
	for name, s := range cases {
		if _, _, found := packageFolders(t, s); found {
			t.Errorf("%s: package request folders mounted", name)
		}
	}
}

// NuGet reads a nuget.config from every folder above the one it restores
// in, so no folder the agent can write may be, or be above, the folder
// vibe fetches in: a config planted there would redirect the host's fetch.
func TestNoFolderTheAgentWritesIsAtOrAboveWhereVibeFetches(t *testing.T) {
	home := t.TempDir()
	s := settings.Settings{NugetDir: hostNuget(t)}
	fetchRoot := state.NugetFetchPath(home)
	for _, f := range sandboxFolders(home, "box", t.TempDir(), s, "claude", "") {
		if f.readOnly {
			continue
		}
		if pathresolve.Within(f.path, fetchRoot) {
			t.Errorf("%s is writable in the sandbox, and is at or above %s", f.path, fetchRoot)
		}
	}
}
