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
	if err := ensureProfile(lay, bundledProfile, false); err != nil {
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
			if err := ensureProfile(lay, "foo-browser", tc.force); err != nil {
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
	if err := ensureProfile(layout.New(t.TempDir(), t.TempDir()), "../evil", true); err == nil {
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
		{"typo", "diffty", "did you mean 'diffity'?"},
		{"unknown", "postgres", "the library has: diffity"},
		{"known", "diffity", "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := layout.New(t.TempDir(), t.TempDir())
			writeFile(t, lay.BundlePath("library", "diffity", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
			writeFile(t, lay.HomePath("settings.yaml"), "defaultFeatures:\n  - "+tc.wanted+"\n")

			err := ensureGuidedProfile(lay, "demo")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}
