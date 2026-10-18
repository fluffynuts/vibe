package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/packagereq"
	"vibe/internal/settings"
)

func TestLoadSettingsRefusesAnUnknownPackagesPolicy(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "packages: sometimes\n")
	_, err := loadSettings(lay, "p")
	if err == nil || !strings.Contains(err.Error(), "sometimes") || !strings.Contains(err.Error(), "ask") || !strings.Contains(err.Error(), "allow") {
		t.Errorf("want an error naming the value and the choices, got %v", err)
	}
}

func TestPackagesAreAskedBeforeFetchingUnlessTheUserSaysAllow(t *testing.T) {
	for policy, want := range map[string]string{"": packagereq.PolicyAsk, "ask": packagereq.PolicyAsk, "allow": packagereq.PolicyAllow} {
		if got := packagesPolicy(settings.Settings{Packages: policy}); got != want {
			t.Errorf("packages %q: policy %q, want %q", policy, got, want)
		}
	}
}

// NuGet on the host reads its config from the user's folders, and a
// nuget.config from every folder above the one it restores in: if the
// agent can write any of those, it can redirect the host's fetch.
func TestNoFetchesWhenTheAgentsFolderHoldsWhatNuGetReadsOnTheHost(t *testing.T) {
	home := t.TempDir()
	vibeHome := filepath.Join(home, ".vibe")
	nuget := filepath.Join(home, ".nuget")
	merged := settings.Settings{NugetDir: nuget}
	for name, target := range map[string]string{
		"the user's home":  home,
		"vibe's own":       vibeHome,
		"nuget's":          nuget,
		"above vibe's own": filepath.Dir(home),
	} {
		if why := packagesUnavailable(target, vibeHome, merged); why == "" || !strings.Contains(why, "start vibe in a project folder") {
			t.Errorf("a sandbox in %s: %q, want it refused, saying what to do", name, why)
		}
	}
	project := filepath.Join(home, "code", "project")
	if why := packagesUnavailable(project, vibeHome, merged); why != "" {
		t.Errorf("an ordinary project folder: %q", why)
	}
}

func TestAFolderTheAgentCanWriteIsComparedWithItsLinksFollowed(t *testing.T) {
	home := t.TempDir()
	vibeHome := filepath.Join(home, ".vibe")
	if err := os.MkdirAll(vibeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if why := packagesUnavailable(alias, vibeHome, settings.Settings{}); why == "" {
		t.Error("a link to the folder holding vibe's own passed as a project")
	}
}
