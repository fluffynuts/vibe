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
	writeFile(t, lay.BundlePath("library", "dashboard", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.HomePath("profiles", "foo-browser", "config.yaml"),
		"# vibe: features: dashboard, dashbord\nname: foo-browser\n")

	err := doReCompose(lay, cliargs.Args{Force: true, Profile: "foo-browser"}, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "did you mean 'dashboard'?") {
		t.Errorf("error for a vanished feature = %v", err)
	}
}

func TestSplitMissingFeaturesKeepsWhatIsStillThere(t *testing.T) {
	present, missing := splitMissingFeatures(
		[]string{"clipboard", "dashboard", "mysql"}, []string{"dashboard", "mysql", "redis"})
	if got := strings.Join(present, ","); got != "dashboard,mysql" {
		t.Errorf("present = %s", got)
	}
	if got := strings.Join(missing, ","); got != "clipboard" {
		t.Errorf("missing = %s", got)
	}
}

// instructionsFixture is a library with one feature that carries
// instructions, and a guided profile composed from it the way profiles were
// before the kits' instructions had a file of their own: their text in the
// profile's agent-instructions.md, and no kit-instructions.md.
func instructionsFixture(t *testing.T) (lay layout.Layout, profileDir string) {
	t.Helper()
	lay = layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "dashboard", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.BundlePath("library", "dashboard", "agent-instructions.md"), "## Dashboard\n\nreview with it.\n")
	profileDir = lay.HomePath("profiles", "foo-browser")
	writeFile(t, filepath.Join(profileDir, "config.yaml"), "# vibe: features: dashboard\nname: foo-browser\n")
	writeFile(t, filepath.Join(profileDir, "agent-instructions.md"), "## Dashboard\n\nreview with it.\n")
	return lay, profileDir
}

func readFile(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func TestReComposingAnOldProfileGivesTheUserTheirOwnInstructionsFile(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := readFile(t, filepath.Join(dir, "kit-instructions.md")); !strings.Contains(got, "## Dashboard") {
		t.Errorf("kit-instructions.md = %q", got)
	}
	if got, ok := readFile(t, filepath.Join(dir, "agent-instructions.md")); !ok || got != "" {
		t.Errorf("agent-instructions.md = %q, %v; want the user's, blank", got, ok)
	}
	if got, ok := readFile(t, lay.HomePath("agent-instructions.md")); !ok || got != "" {
		t.Errorf("~/.vibe/agent-instructions.md = %q, %v; want a blank one", got, ok)
	}
}

func TestReComposingKeepsTheUsersInstructions(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "agent-instructions.md"), "use British spelling\n")
	for i := 0; i < 2; i++ {
		if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
			t.Fatal(err)
		}
		if got, _ := readFile(t, filepath.Join(dir, "agent-instructions.md")); got != "use British spelling\n" {
			t.Fatalf("after re-compose %d: agent-instructions.md = %q", i+1, got)
		}
		if got, _ := readFile(t, filepath.Join(dir, "kit-instructions.md")); strings.Contains(got, "British") || !strings.Contains(got, "## Dashboard") {
			t.Fatalf("after re-compose %d: kit-instructions.md = %q", i+1, got)
		}
	}
}

func TestRestoringABackupFromBeforeTheMoveStillSortsTheFilesOut(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	// The first re-compose backs up the profile as it was: old-style.
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "agent-instructions.md"), "mine\n")
	if err := doRestore(lay, args, "foo-browser", t.TempDir()); err == nil {
		t.Log("restore went on to rebuild the sandbox")
	}
	// The oldest backup is the old-style profile: restored, then
	// re-composed, its kit text must not land in the user's file.
	if got, _ := readFile(t, filepath.Join(dir, "kit-instructions.md")); !strings.Contains(got, "## Dashboard") {
		t.Errorf("kit-instructions.md = %q", got)
	}
	if got, _ := readFile(t, filepath.Join(dir, "agent-instructions.md")); strings.Contains(got, "## Dashboard") {
		t.Errorf("the kits' text ended up in the user's file: %q", got)
	}
}

func TestABundledProfileIsNotCopiedIntoTheOverlay(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("profiles", "shipped", "config.yaml"), "name: shipped\n")
	if err := prepareInstructions(lay, "shipped"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lay.HomePath("profiles", "shipped")); !os.IsNotExist(err) {
		t.Errorf("a folder was made in ~/.vibe for a bundled profile, which would hide it: %v", err)
	}
	if got, ok := readFile(t, lay.HomePath("agent-instructions.md")); !ok || got != "" {
		t.Errorf("~/.vibe/agent-instructions.md = %q, %v", got, ok)
	}
}

// A profile built before a base feature existed is told which it lacks:
// not always the clipboard.
func TestAProfileMissingABaseFeatureIsToldWhich(t *testing.T) {
	lay := updateLayout(t)
	if err := os.RemoveAll(lay.HomePath("profiles", "p", "agent-files", ".local", "bin", "vibe-review")); err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(notesOf(func() { noteMissingBase(lay, "p") }), "\n")
	if !strings.Contains(notes, "review") || strings.Contains(notes, "clipboard") {
		t.Errorf("notes: %q", notes)
	}
}
