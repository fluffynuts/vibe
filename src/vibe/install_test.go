package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"vibe/internal/fscopy"
	"vibe/internal/hostmem"
	"vibe/internal/layout"
)

// TestDoInstallCopiesEverythingAndLeavesCustomizationsAlone checks the
// two --install requirements: it merges config.yaml/settings.yaml,
// defaults/, profiles/ and library/ from the bundle into ~/.vibe (without
// touching anything already customized there), and it copies the running
// binary into ~/.local/bin.
func TestDoInstallCopiesEverythingAndLeavesCustomizationsAlone(t *testing.T) {
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home)        // installBinary resolves ~/.local/bin from this
	t.Setenv("USERPROFILE", home) // ...or, on Windows, this

	writeFile(t, filepath.Join(bundle, "config.yaml"), "name: vibe\n")
	writeFile(t, filepath.Join(bundle, "settings.yaml"), "memory: 12g\n")
	writeFile(t, filepath.Join(bundle, "defaults", "install-scripts", "01-a"), "echo a\n")
	writeFile(t, filepath.Join(bundle, "profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	writeFile(t, filepath.Join(bundle, "library", "mysql", "install-scripts", "01-a"), "echo mysql\n")

	vibeHome := filepath.Join(home, ".vibe")
	// a pre-existing customization that must survive the install
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), "memory: 24g\n")

	// It differs from the package's, and with no record of which release it
	// came from (so no merge) and -f (so no asking), it's left alone — and
	// the install says so, failing, rather than calling itself done.
	lay := layout.New(vibeHome, bundle)
	err := doInstall(lay, true, "")
	if err == nil || !strings.Contains(err.Error(), "left unmerged in 1 file") {
		t.Errorf("doInstall = %v, want it to report the one unsettled file", err)
	}

	if data, err := os.ReadFile(filepath.Join(vibeHome, "settings.yaml")); err != nil || !strings.Contains(string(data), "24g") {
		t.Errorf("existing settings.yaml was overwritten: %q, %v", data, err)
	}
	if _, err := os.ReadFile(filepath.Join(vibeHome, "config.yaml")); err != nil {
		t.Errorf("expected config.yaml copied in: %v", err)
	}
	for _, p := range []string{
		filepath.Join(vibeHome, "defaults", "install-scripts", "01-a"),
		filepath.Join(vibeHome, "profiles", "yumbi", "config.yaml"),
		filepath.Join(vibeHome, "library", "mysql", "install-scripts", "01-a"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s copied in: %v", p, err)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dest := filepath.Join(home, ".local", "bin", filepath.Base(exe))
	if info, err := os.Stat(dest); err != nil {
		t.Errorf("expected the binary copied to %s: %v", dest, err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("expected the copied binary to be executable, got %v", info.Mode())
	}
}

// TestDoInstallIsSafeToRunTwice re-running --install (e.g. after fetching a
// newer bundle release) must not clobber a file the user edited in ~/.vibe
// after the first install.
func TestDoInstallIsSafeToRunTwice(t *testing.T) {
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(bundle, "config.yaml"), "name: vibe\n")

	lay := layout.New(filepath.Join(home, ".vibe"), bundle)
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	universal := filepath.Join(home, ".vibe", "agent-instructions.md")
	if data, err := os.ReadFile(universal); err != nil || len(data) != 0 {
		t.Errorf("the first install didn't make a blank agent-instructions.md: %q, %v", data, err)
	}
	writeFile(t, filepath.Join(home, ".vibe", "config.yaml"), "name: customized\n")
	writeFile(t, universal, "use British spelling\n")
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".vibe", "config.yaml"))
	if err != nil || !strings.Contains(string(data), "customized") {
		t.Errorf("second install overwrote the customized config.yaml: %q, %v", data, err)
	}
	if data, _ := os.ReadFile(universal); string(data) != "use British spelling\n" {
		t.Errorf("second install touched the user's agent-instructions.md: %q", data)
	}
}

// TestDoInstallUpgradesFromOneReleaseToTheNext installs one release, edits a
// file, then installs the next: files the user never touched follow the
// package, a file changed on both sides merges, and one the user deleted
// stays deleted.
func TestDoInstallUpgradesFromOneReleaseToTheNext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("merging needs git")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	vibeHome := filepath.Join(home, ".vibe")
	lines := "a\nb\nc\nd\ne\nf\ng\n"

	v1 := t.TempDir()
	writeFile(t, filepath.Join(v1, "settings.yaml"), lines)
	writeFile(t, filepath.Join(v1, "defaults", "install-scripts", "01-a"), "echo v1\n")
	writeFile(t, filepath.Join(v1, "defaults", "install-scripts", "02-b"), "echo b\n")
	if err := doInstall(layout.New(vibeHome, v1), true, ""); err != nil {
		t.Fatal(err)
	}

	// The user edits the end of settings.yaml and deletes a default script.
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), strings.Replace(lines, "g\n", "G (mine)\n", 1))
	os.Remove(filepath.Join(vibeHome, "defaults", "install-scripts", "02-b"))

	// The next release changes the start of settings.yaml and 01-a, and adds a file.
	v2 := t.TempDir()
	writeFile(t, filepath.Join(v2, "settings.yaml"), strings.Replace(lines, "a\n", "A (package)\n", 1))
	writeFile(t, filepath.Join(v2, "defaults", "install-scripts", "01-a"), "echo v2\n")
	writeFile(t, filepath.Join(v2, "defaults", "install-scripts", "02-b"), "echo b\n")
	writeFile(t, filepath.Join(v2, "defaults", "install-scripts", "03-new"), "echo new\n")
	if err := doInstall(layout.New(vibeHome, v2), true, "merge,keep"); err != nil {
		t.Fatal(err)
	}

	read := func(rel ...string) string {
		data, _ := os.ReadFile(filepath.Join(append([]string{vibeHome}, rel...)...))
		return string(data)
	}
	if got := read("settings.yaml"); !strings.Contains(got, "A (package)") || !strings.Contains(got, "G (mine)") {
		t.Errorf("settings.yaml wasn't merged: %q", got)
	}
	if got := read("defaults", "install-scripts", "01-a"); got != "echo v2\n" {
		t.Errorf("the untouched 01-a wasn't updated: %q", got)
	}
	if got := read("defaults", "install-scripts", "03-new"); got != "echo new\n" {
		t.Errorf("the new file wasn't copied: %q", got)
	}
	if _, err := os.Stat(filepath.Join(vibeHome, "defaults", "install-scripts", "02-b")); err == nil {
		t.Error("the deleted 02-b came back")
	}
}

func TestInstallUpdateStrategyIsCheckedFirst(t *testing.T) {
	err := doInstall(layout.New(t.TempDir(), t.TempDir()), true, "update,merge")
	if err == nil || !strings.Contains(err.Error(), "unknown --update-strategy") {
		t.Errorf("doInstall with a bad strategy = %v", err)
	}
}

func TestOfferSbxInstallOnlyHintsWithForce(t *testing.T) {
	old := sbxOnPath
	defer func() { sbxOnPath = old }()
	for _, onPath := range []bool{true, false} {
		sbxOnPath = func() bool { return onPath }
		// With -f nothing is asked and nothing is downloaded: a missing sbx
		// only gets a hint.
		if err := offerSbxInstall(true); err != nil {
			t.Errorf("offerSbxInstall(force) with sbx on PATH=%v = %v", onPath, err)
		}
	}
}

func TestWarnIfNotOnPathNeverFails(t *testing.T) {
	old := addToUserPath
	addToUserPath = func(string) (bool, error) { return false, errors.New("no registry here") }
	t.Cleanup(func() { addToUserPath = old })
	if err := warnIfNotOnPath(t.TempDir(), "vibe"); err != nil {
		t.Errorf("warnIfNotOnPath must never fail: %v", err)
	}
}

// An upgrade replaces the installed vibe even while another session is
// running it — which a plain copy over it can't do ("text file busy").
func TestReplaceFileWhileTheTargetIsRunning(t *testing.T) {
	// The running target is a copy of this test binary, which stays up
	// when told to (see TestMain) whatever it's called.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "vibe")
	if runtime.GOOS == "windows" {
		dest += ".exe" // Windows only runs what's named like a program
	}
	if err := fscopy.File(self, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	running := exec.Command(dest)
	running.Env = append(os.Environ(), "VIBE_TEST_STAY_RUNNING=1")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { running.Process.Kill(); running.Wait() })
	time.Sleep(200 * time.Millisecond)
	if err := running.Process.Signal(syscall.Signal(0)); err != nil && runtime.GOOS != "windows" {
		t.Fatalf("the stand-in exited before the test could replace it: %v", err)
	}
	// Writing straight into it is what the plain copy used to do, and fails.
	if f, err := os.OpenFile(dest, os.O_WRONLY|os.O_TRUNC, 0); err == nil {
		f.Close()
		t.Log("this platform lets a running binary be overwritten in place; the swap is still exercised")
	}

	src := filepath.Join(dir, "new-vibe")
	writeFile(t, src, "#!/bin/sh\necho new\n")
	if err := replaceFile(src, dest); err != nil {
		t.Fatalf("replaceFile while the target runs: %v", err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "#!/bin/sh\necho new\n" {
		t.Errorf("dest wasn't replaced: %q", data)
	}
	if info, _ := os.Stat(dest); runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the replaced binary isn't executable: %v", info.Mode())
	}
	leftovers := []string{dest + ".new"}
	if runtime.GOOS != "windows" { // Windows can't delete the running old one yet
		leftovers = append(leftovers, dest+".old")
	}
	for _, leftover := range leftovers {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("left behind %s", leftover)
		}
	}
}

// onHost stands in a machine with gib GB installed — reporting a little
// under that, as an OS does — for the rest of the test.
func onHost(t *testing.T, gib uint64) {
	t.Helper()
	old := hostmem.Total
	hostmem.Total = func() (uint64, error) { return gib*hostmem.GiB - 400<<20, nil }
	t.Cleanup(func() { hostmem.Total = old })
}

const shippedSettings = `memory: 12g
agent: claude
# ticked by default when a guided profile is created
defaultFeatures:
  - dashboard
`

// TestDoInstallFirstTimeSetsMemoryForThisMachine: with nobody to ask, a
// first install keeps the package's settings, but with no more memory than
// half of this machine's.
func TestDoInstallFirstTimeSetsMemoryForThisMachine(t *testing.T) {
	onHost(t, 16)
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(bundle, "settings.yaml"), shippedSettings)

	vibeHome := filepath.Join(home, ".vibe")
	if err := doInstall(layout.New(vibeHome, bundle), true, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(vibeHome, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(shippedSettings, "memory: 12g", "memory: 8g", 1); string(data) != want {
		t.Errorf("settings.yaml after a first install on 16 GB:\n%s\nwant:\n%s", data, want)
	}
}

// TestDoInstallLowersAMemorySettingThisMachineCantGive: a later install
// leaves the user's settings alone, but for a memory setting over half
// this machine's.
func TestDoInstallLowersAMemorySettingThisMachineCantGive(t *testing.T) {
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(bundle, "settings.yaml"), shippedSettings)
	vibeHome := filepath.Join(home, ".vibe")
	lay := layout.New(vibeHome, bundle)
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(shippedSettings, "agent: claude", "agent: codex", 1)
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), mine)

	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(vibeHome, "settings.yaml")); string(data) != mine {
		t.Errorf("a later install on a big machine changed settings.yaml:\n%s", data)
	}

	onHost(t, 16)
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(vibeHome, "settings.yaml"))
	if want := strings.Replace(mine, "memory: 12g", "memory: 8g", 1); string(data) != want {
		t.Errorf("settings.yaml after a later install on 16 GB:\n%s\nwant:\n%s", data, want)
	}
}

func TestAFeatureThatIsABaseOneNowIsMovedAside(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "base", "clipboard-bridge", "config.yaml"), "new\n")
	writeFile(t, lay.HomePath("library", "clipboard-bridge", "config.yaml"), "old, maybe edited\n")
	writeFile(t, lay.HomePath("library", "mysql", "config.yaml"), "mine\n")
	now := time.Date(2026, 10, 12, 13, 0, 0, 0, time.UTC)

	retireShadowedFeatures(lay, now, nil)
	if _, err := os.Stat(lay.HomePath("library", "clipboard-bridge")); !os.IsNotExist(err) {
		t.Errorf("the old copy is still in the library: %v", err)
	}
	if b, err := os.ReadFile(lay.HomePath("retired", "library", "clipboard-bridge", "config.yaml")); err != nil || string(b) != "old, maybe edited\n" {
		t.Errorf("retired copy: %q, %v", b, err)
	}
	if _, err := os.Stat(lay.HomePath("library", "mysql", "config.yaml")); err != nil {
		t.Errorf("an ordinary feature was moved: %v", err)
	}

	// Another, later: the first one moved aside is kept.
	writeFile(t, lay.HomePath("library", "clipboard-bridge", "config.yaml"), "again\n")
	retireShadowedFeatures(lay, now, nil)
	if b, _ := os.ReadFile(lay.HomePath("retired", "library", "clipboard-bridge-2026-10-12_130000", "config.yaml")); string(b) != "again\n" {
		t.Errorf("the second copy moved aside: %q", b)
	}
}

func TestAFeatureThatIsABaseOneNowCanBeRemoved(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "base", "clipboard-bridge", "config.yaml"), "new\n")
	writeFile(t, lay.HomePath("library", "clipboard-bridge", "config.yaml"), "old\n")
	var asked string
	retireShadowedFeatures(lay, time.Now(), func(name, dir string) bool { asked = name; return true })
	if asked != "clipboard-bridge" {
		t.Errorf("asked about %q", asked)
	}
	for _, dir := range []string{lay.HomePath("library", "clipboard-bridge"), lay.HomePath("retired")} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s is there: %v", dir, err)
		}
	}
}

// installedBinaryOf puts stand-in content at the ~/.local/bin copy of the
// running binary, in a fresh home, returning its path.
func installedBinaryOf(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".local", "bin", filepath.Base(exe))
	writeFile(t, dest, content)
	return dest
}

// A build from source replacing a released vibe would lose that release,
// and leave 'vibe --upgrade' offering it again.
func TestADevelopmentBuildLeavesAnInstalledVibeAlone(t *testing.T) {
	asBuild(t, "")
	dest := installedBinaryOf(t, "the release\n")

	var err error
	notes := strings.Join(notesOf(func() { err = installBinary() }), "\n")
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "the release\n" {
		t.Errorf("the installed vibe was replaced by a development build")
	}
	if !strings.Contains(notes, "development build") || !strings.Contains(notes, "leaving the vibe at "+dest) {
		t.Errorf("notes don't say why the installed vibe was left alone:\n%s", notes)
	}
}

func TestAReleaseBuildReplacesAnInstalledVibe(t *testing.T) {
	asBuild(t, "50")
	dest := installedBinaryOf(t, "an older release\n")

	if err := installBinary(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dest); string(data) == "an older release\n" {
		t.Errorf("a release build didn't replace the installed vibe")
	}
}
