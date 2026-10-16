package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/layout"
)

// TestEnsureProfileExisting leaves a profile that is already there alone.
func TestEnsureProfileExisting(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	if err := ensureProfile(lay, bundledProfile, false, "claude"); err != nil {
		t.Fatalf("ensureProfile on an existing profile: %v", err)
	}
	if _, err := os.Stat(lay.HomePath("profiles")); err == nil {
		t.Error("ensureProfile wrote to the overlay for a profile that already exists")
	}
}

// TestEnsureProfileCreatesBlank covers the two paths that need no prompting:
// an empty bundle (nothing to copy) and --force.
func TestEnsureProfileCreatesBlank(t *testing.T) {
	for _, tc := range []struct {
		name        string
		seed, force bool
	}{
		{"no profiles to copy", false, false},
		{"force", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := layout.New(t.TempDir(), t.TempDir())
			if tc.seed {
				dir := lay.BundlePath("profiles", "yumbi")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("name: yumbi\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := ensureProfile(lay, "foo-browser", tc.force, "claude"); err != nil {
				t.Fatalf("ensureProfile: %v", err)
			}
			// the new profile lands in the overlay, not the bundle
			if got, want := lay.ProfileDir("foo-browser"), lay.HomePath("profiles", "foo-browser"); got != want {
				t.Errorf("created in %s, want %s", got, want)
			}
			// and it loads: a blank profile is enough to build a kit
			if _, _, err := loadKit(lay, "foo-browser"); err != nil {
				t.Fatalf("loadKit on the created profile: %v", err)
			}
		})
	}
}

// TestEnsureProfileRejectsBadName refuses to create a profile whose name
// could escape the profiles directory.
func TestEnsureProfileRejectsBadName(t *testing.T) {
	if err := ensureProfile(layout.New(t.TempDir(), t.TempDir()), "../evil", true, "claude"); err == nil {
		t.Fatal("expected an error for a path-escaping profile name")
	}
}

// TestGuidedProfileChecksDefaultFeatures covers the two ways settings.yaml's
// defaultFeatures list is read before the picker is ever drawn: a name the
// library doesn't have stops everything with the closest real name
// suggested, and a name it does have gets through to the prompt (which there
// is no terminal for here, so it aborts — the point is that it got that far).
func TestGuidedProfileChecksDefaultFeatures(t *testing.T) {
	for _, tc := range []struct{ name, wanted, wantErr string }{
		{"typo", "dashbord", "did you mean 'dashboard'?"},
		{"unknown", "postgres", "the library has: dashboard"},
		{"known", "dashboard", "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := layout.New(t.TempDir(), t.TempDir())
			writeFile(t, lay.BundlePath("library", "dashboard", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
			writeFile(t, lay.HomePath("settings.yaml"), "defaultFeatures:\n  - "+tc.wanted+"\n")

			err := ensureGuidedProfile(lay, "demo", "claude")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// writeClaudeOnly puts a claude-only feature, name, in the bundle's library.
func writeClaudeOnly(t *testing.T, lay layout.Layout, name string) {
	t.Helper()
	writeFile(t, lay.BundlePath("library", name, "install-scripts", "01-install"),
		"#!/bin/sh\n# vibe: description: Install "+name+"\n# vibe: agents: claude\necho hi\n")
}

// A defaultFeatures entry the chosen agent can't use is left out with a
// note saying why, rather than stopping creation as an unknown name does.
func TestGuidedProfileLeavesOutDefaultsTheAgentCantUse(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeClaudeOnly(t, lay, "dashboard")
	writeFile(t, lay.BundlePath("library", "redis", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.HomePath("settings.yaml"), "defaultFeatures:\n  - dashboard\n")

	var err error
	notes := strings.Join(notesOf(func() { err = ensureGuidedProfile(lay, "demo", "codex") }), "\n")
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("error = %v, want it to get as far as asking (and abort, with no terminal)", err)
	}
	if !strings.Contains(notes, "leaving out dashboard: it works only with claude, and this sandbox runs codex") {
		t.Errorf("notes don't say dashboard was left out, and why:\n%s", notes)
	}
}

func TestGuidedProfileSaysSoWhenNoFeatureSuitsTheAgent(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeClaudeOnly(t, lay, "dashboard")

	err := ensureGuidedProfile(lay, "demo", "codex")
	if err == nil || !strings.Contains(err.Error(), "no library features work with codex") {
		t.Errorf("error = %v", err)
	}
}

func TestOfferedFeaturesAreTheAgentsAndWhatTheProfileHas(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeClaudeOnly(t, lay, "dashboard")
	writeClaudeOnly(t, lay, "other")
	writeFile(t, lay.BundlePath("library", "redis", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	all := lay.Features()

	for _, tc := range []struct {
		agent string
		has   []string
		want  string
	}{
		{"claude", nil, "dashboard other redis"},
		{"codex", nil, "redis"},
		{"codex", []string{"dashboard"}, "dashboard redis"},
	} {
		if got := strings.Join(offeredFor(lay, all, tc.agent, tc.has), " "); got != tc.want {
			t.Errorf("offeredFor(%s, has %v) = %q, want %q", tc.agent, tc.has, got, tc.want)
		}
	}
}

func TestChecklistLabelsSayWhichAgentsAFeatureIsFor(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeClaudeOnly(t, lay, "dashboard")
	writeFile(t, lay.BundlePath("library", "redis", "install-scripts", "01-install"),
		"#!/bin/sh\n# vibe: description: Install redis\necho hi\n")

	_, labels, _ := featureChecklist(lay, lay.Features(), nil)
	want := "dashboard: Install dashboard (claude only)|redis: Install redis"
	if got := strings.Join(labels, "|"); got != want {
		t.Errorf("labels = %q, want %q", got, want)
	}
}

// A profile shared with a sandbox of another agent is used as it is, with a
// warning for what this sandbox's agent can't use.
func TestUnsupportedProfileFeaturesAreWarnedAbout(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeClaudeOnly(t, lay, "dashboard")
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "# vibe: features: dashboard\nname: p\n")

	notes := strings.Join(notesOf(func() { warnUnsupportedFeatures(lay, "p", "codex") }), "\n")
	if !strings.Contains(notes, "WARNING: profile 'p' has dashboard: it works only with claude, and this sandbox runs codex") {
		t.Errorf("no warning for dashboard:\n%s", notes)
	}
	if notes := notesOf(func() { warnUnsupportedFeatures(lay, "p", "claude") }); len(notes) != 0 {
		t.Errorf("warned for a feature the agent can use: %v", notes)
	}
}

func TestTheAgentOfferedFirstIsTheProfilesThenTheUsers(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.HomePath("settings.yaml"), "agent: gemini\n")
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "agent: codex\n")

	for profile, want := range map[string]string{"p": "codex", "new-one": "gemini"} {
		got, err := defaultAgentFor(lay, profile)
		if err != nil || got != want {
			t.Errorf("defaultAgentFor(%s) = %q, %v; want %q", profile, got, err, want)
		}
	}
}
