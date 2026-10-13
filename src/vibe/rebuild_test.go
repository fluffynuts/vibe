package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
)

// TestDoReCreateDeletesTheOverlayProfileThenRebuilds checks the part of
// --re-create that doesn't need a real sbx binary: it deletes the overlay
// copy of the matched profile, then (via doReInit, with no profile left)
// creates a fresh blank one in its place before going on to attempt the
// sandbox rebuild, which fails harmlessly here since sbx isn't installed.
func TestDoReCreateDeletesTheOverlayProfileThenRebuilds(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	dir := lay.HomePath("profiles", "foo-browser")
	writeFile(t, filepath.Join(dir, "config.yaml"), "name: foo-browser\n")
	writeFile(t, filepath.Join(dir, "install-scripts", "01-mine"), "echo mine\n")

	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	if err := doReCreate(lay, args, "foo-browser", t.TempDir()); err == nil {
		t.Fatal("expected an error from the sandbox rebuild step (sbx is not installed here)")
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "01-mine")); err == nil {
		t.Error("the old profile's install script is still there — the overlay copy was not deleted")
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("expected a fresh blank profile in its place: %v", err)
	}
	if !strings.Contains(string(data), "name: foo-browser") {
		t.Errorf("unexpected config.yaml after re-create:\n%s", data)
	}
}

// TestReComposeNeedsAProfileThatRecordsItsFeatures covers the two ways
// --re-compose refuses rather than guessing: no profile at all, and a
// profile that was not composed from the library (hand-written, copied or
// blank), which has nothing to re-compose from.
func TestReComposeNeedsAProfileThatRecordsItsFeatures(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	args := cliargs.Args{Force: true, Profile: "foo-browser"}

	err := doReCompose(lay, args, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no profile 'foo-browser' to re-compose") {
		t.Errorf("error for a missing profile = %v", err)
	}

	writeFile(t, lay.HomePath("profiles", "foo-browser", "config.yaml"), "name: foo-browser\n")
	err = doReCompose(lay, args, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "does not record which features") {
		t.Errorf("error for a profile with no record = %v", err)
	}
}

// TestReComposeRejectsAFeatureThatIsGoneWhenNotAsking keeps a renamed or
// removed feature from silently dropping out of a rebuilt profile when there
// is no one to ask (--force).
func TestReComposeRejectsAFeatureThatIsGoneWhenNotAsking(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "diffity", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.HomePath("profiles", "foo-browser", "config.yaml"),
		"# vibe: features: diffity, diffty\nname: foo-browser\n")

	err := doReCompose(lay, cliargs.Args{Force: true, Profile: "foo-browser"}, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "did you mean 'diffity'?") {
		t.Errorf("error for a vanished feature = %v", err)
	}
}

func TestSplitMissingFeaturesKeepsWhatIsStillThere(t *testing.T) {
	present, missing := splitMissingFeatures(
		[]string{"clipboard", "diffity", "mysql"}, []string{"diffity", "mysql", "redis"})
	if got := strings.Join(present, ","); got != "diffity,mysql" {
		t.Errorf("present = %s", got)
	}
	if got := strings.Join(missing, ","); got != "clipboard" {
		t.Errorf("missing = %s", got)
	}
}
