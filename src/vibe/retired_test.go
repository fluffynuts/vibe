package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/profilegen"
)

// A defaultFeatures naming a feature vibe no longer has leaves it out,
// saying why, rather than refusing to make a guided profile at all.
func TestADefaultFeatureVibeNoLongerHasIsLeftOut(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "dotnet", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.HomePath("settings.yaml"), "defaultFeatures:\n  - diffity\n  - dotnet\n")
	var err error
	notes := strings.Join(notesOf(func() { err = ensureGuidedProfile(lay, "demo", "claude") }), "\n")
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("error = %v, want it to reach the picker (aborted, with no terminal)", err)
	}
	if !strings.Contains(notes, "diffity") || !strings.Contains(notes, "Review tab") {
		t.Errorf("notes don't say diffity is gone, and for what:\n%s", notes)
	}
}

// A profile composed with a feature vibe no longer has is settled without
// it, saying so: re-composing it then just leaves it out.
func TestAProfileComposedWithARetiredFeatureIsSettledWithoutIt(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	dir := lay.HomePath("profiles", "old")
	writeFile(t, filepath.Join(dir, "config.yaml"), "# vibe: features: diffity, dotnet\nname: old\n")
	var features []string
	notes := strings.Join(notesOf(func() { features = settleFeatures(lay, "old") }), "\n")
	if want := []string{"dotnet"}; !reflect.DeepEqual(features, want) || !reflect.DeepEqual(profilegen.ComposedFrom(dir), want) {
		t.Errorf("settled %v, recorded %v, want %v", features, profilegen.ComposedFrom(dir), want)
	}
	if !strings.Contains(notes, "'old'") || !strings.Contains(notes, "diffity") {
		t.Errorf("notes don't say what was left out of which profile:\n%s", notes)
	}
	// Settled once, it's not said again.
	if again := notesOf(func() { settleFeatures(lay, "old") }); len(again) != 0 {
		t.Errorf("said again: %q", again)
	}
}
