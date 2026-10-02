package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"vibe"
	"vibe/internal/sbxinstall"
	"vibe/internal/selfupdate"
)

// fakeReleases serves a GitHub releases page whose latest release is tag,
// for as long as the test runs, and returns its URL.
func fakeReleases(t *testing.T, tag string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/latest" {
			http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/releases"
}

// onReleases makes vibe's latest release vibeTag, and sbx's sbxTag.
func onReleases(t *testing.T, vibeTag, sbxTag string) {
	t.Helper()
	oldVibe, oldSbx := selfupdate.Releases, sbxinstall.Releases
	selfupdate.Releases, sbxinstall.Releases = fakeReleases(t, vibeTag), fakeReleases(t, sbxTag)
	t.Cleanup(func() { selfupdate.Releases, sbxinstall.Releases = oldVibe, oldSbx })
}

// asBuild makes this vibe release build of the current version.
func asBuild(t *testing.T, build string) {
	t.Helper()
	old := vibe.Build
	vibe.Build = build
	t.Cleanup(func() { vibe.Build = old })
}

// withSbx puts a stand-in sbx reporting version where --install-sbx
// installs it, in a fresh home, with nothing else on PATH.
func withSbx(t *testing.T, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in sbx is a shell script")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if version == "" {
		return
	}
	writeFile(t, filepath.Join(home, ".docker", "sbx", "bin", "sbx"), "#!/bin/sh\necho 'sbx version: "+version+" abc123'\n")
	os.Chmod(filepath.Join(home, ".docker", "sbx", "bin", "sbx"), 0o755)
}

// captureStdout returns what f prints to stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestUpgradeWhenEverythingIsUpToDate(t *testing.T) {
	asBuild(t, "50")
	onReleases(t, "v"+vibe.Version+".50", "v0.46.0")
	withSbx(t, "v0.46.0")
	var err error
	out := captureStdout(t, func() { err = doUpgrade(false, "") })
	if err != nil || out != "vibe and sbx are up to date\n" {
		t.Errorf("doUpgrade = %v, printing %q", err, out)
	}
}

func TestUpgradeDoesntClaimUpToDateWhenItCouldntCheck(t *testing.T) {
	asBuild(t, "50")
	onReleases(t, "v"+vibe.Version+".50", "v0.46.0")
	withSbx(t, "v0.46.0")
	selfupdate.Releases = "http://127.0.0.1:1/releases" // nothing listens there
	var err error
	out := captureStdout(t, func() { err = doUpgrade(false, "") })
	if err == nil || !strings.Contains(err.Error(), "vibe") || out != "" {
		t.Errorf("doUpgrade = %v, printing %q; want it to say vibe couldn't be checked", err, out)
	}
}

func TestCheckVibeUpgrade(t *testing.T) {
	asBuild(t, "50")
	onReleases(t, "v"+vibe.Version+".51", "v0.46.0")
	u, summary, err := checkVibeUpgrade(false, "")
	if err != nil || u == nil {
		t.Fatalf("checkVibeUpgrade = %v, %q, %v; want an upgrade", u, summary, err)
	}
	if want := "vibe " + vibe.Version + ".50 → " + vibe.Version + ".51"; u.label != want {
		t.Errorf("label = %q, want %q", u.label, want)
	}
	asBuild(t, "51")
	if u, summary, err := checkVibeUpgrade(false, ""); err != nil || u != nil || !strings.Contains(summary, "is the latest") {
		t.Errorf("checkVibeUpgrade when up to date = %v, %q, %v", u, summary, err)
	}
}

func TestCheckSbxUpgrade(t *testing.T) {
	onReleases(t, "v0.1.1", "v0.46.0")
	for _, tt := range []struct {
		installed, label, summary string
	}{
		{"v0.45.1", "Docker SBX v0.45.1 → v0.46.0", "Docker SBX v0.46.0 is available (you have v0.45.1)"},
		{"v0.46.0", "", "Docker SBX v0.46.0 is the latest"},
		{"v0.47.0-rc2", "", "Docker SBX v0.47.0-rc2 is the latest"},
		{"", "", "Docker SBX isn't installed — vibe --install-sbx installs it"},
	} {
		withSbx(t, tt.installed)
		u, summary, err := checkSbxUpgrade()
		if err != nil {
			t.Fatalf("checkSbxUpgrade with %q installed: %v", tt.installed, err)
		}
		label := ""
		if u != nil {
			label = u.label
		}
		if label != tt.label || summary != tt.summary {
			t.Errorf("checkSbxUpgrade with %q installed = %q, %q; want %q, %q", tt.installed, label, summary, tt.label, tt.summary)
		}
	}
}

func TestCheckSbxUpgradeLeavesAnSbxItDidntInstall(t *testing.T) {
	onReleases(t, "v0.1.1", "v0.46.0")
	withSbx(t, "")
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, "sbx"), "#!/bin/sh\necho 'sbx version: v0.45.1'\n")
	os.Chmod(filepath.Join(elsewhere, "sbx"), 0o755)
	t.Setenv("PATH", elsewhere)
	u, summary, err := checkSbxUpgrade()
	if err != nil || u != nil || !strings.Contains(summary, "wasn't installed by vibe --install-sbx") {
		t.Errorf("checkSbxUpgrade = %v, %q, %v", u, summary, err)
	}
}

// withATerminal makes interactive() true for the rest of the test, and
// pickUpgrades answer with pick.
func withATerminal(t *testing.T, pick func(labels []string) ([]int, bool)) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	oldInput, oldPick := openTerminalInput, pickUpgrades
	openTerminalInput = func() (*os.File, func(), bool) { return r, func() {}, true }
	pickUpgrades = pick
	t.Cleanup(func() { openTerminalInput, pickUpgrades = oldInput, oldPick })
}

func TestChooseUpgrades(t *testing.T) {
	found := []pendingUpgrade{{label: "vibe 0.1.1 → 0.1.2"}, {label: "Docker SBX v0.45.1 → v0.46.0"}}
	labels := func(us []pendingUpgrade) []string {
		var out []string
		for _, u := range us {
			out = append(out, u.label)
		}
		return out
	}

	// No terminal: everything, without asking.
	if got, err := chooseUpgrades(found, false); err != nil || len(got) != 2 {
		t.Errorf("chooseUpgrades with no terminal = %v, %v", labels(got), err)
	}

	asked := false
	withATerminal(t, func(l []string) ([]int, bool) {
		asked = true
		if !reflect.DeepEqual(l, labels(found)) {
			t.Errorf("offered %v", l)
		}
		return []int{1}, true
	})
	if got, err := chooseUpgrades(found, true); err != nil || len(got) != 2 || asked {
		t.Errorf("chooseUpgrades with -f = %v, %v, asked = %v; want all, unasked", labels(got), err, asked)
	}
	if got, err := chooseUpgrades(found, false); err != nil || !reflect.DeepEqual(labels(got), []string{found[1].label}) {
		t.Errorf("chooseUpgrades picking the second = %v, %v", labels(got), err)
	}

	pickUpgrades = func([]string) ([]int, bool) { return nil, true }
	if _, err := chooseUpgrades(found, false); err == nil || err.Error() != "nothing selected to update" {
		t.Errorf("chooseUpgrades picking nothing = %v", err)
	}
	pickUpgrades = func([]string) ([]int, bool) { return nil, false }
	if _, err := chooseUpgrades(found, false); err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Errorf("chooseUpgrades when the user quits = %v", err)
	}
}

func TestAvailableUpgrades(t *testing.T) {
	asBuild(t, "50")
	onReleases(t, "v"+vibe.Version+".51", "v0.46.0")
	withSbx(t, "v0.45.1")
	want := []string{"vibe " + vibe.Version + ".50 → " + vibe.Version + ".51", "Docker SBX v0.45.1 → v0.46.0"}
	if got := availableUpgrades(); !reflect.DeepEqual(got, want) {
		t.Errorf("availableUpgrades = %q, want %q", got, want)
	}

	asBuild(t, "51")
	withSbx(t, "v0.46.0")
	if got := availableUpgrades(); len(got) != 0 {
		t.Errorf("availableUpgrades when up to date = %q, want nothing", got)
	}
}

func TestAvailableUpgradesSaysNothingOfFailedChecks(t *testing.T) {
	asBuild(t, "50")
	onReleases(t, "v"+vibe.Version+".51", "v0.46.0")
	selfupdate.Releases = "http://127.0.0.1:1/releases" // nothing listens here
	withSbx(t, "v0.45.1")
	if got, want := availableUpgrades(), []string{"Docker SBX v0.45.1 → v0.46.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("availableUpgrades with vibe's check failing = %q, want %q", got, want)
	}
}

func TestReportUpgrades(t *testing.T) {
	var out strings.Builder
	reportUpgrades(&out, nil)
	if out.String() != "" {
		t.Errorf("reportUpgrades with nothing newer printed %q", out.String())
	}
	reportUpgrades(&out, []string{"vibe 0.1.1 → 0.1.2", "Docker SBX v0.45.1 → v0.46.0"})
	want := "vibe: newer releases are available:\n" +
		"vibe:   vibe 0.1.1 → 0.1.2\n" +
		"vibe:   Docker SBX v0.45.1 → v0.46.0\n" +
		"vibe: run 'vibe --upgrade' to install them\n"
	if out.String() != want {
		t.Errorf("reportUpgrades printed %q, want %q", out.String(), want)
	}
}
