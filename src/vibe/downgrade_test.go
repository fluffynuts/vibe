package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vibe"
	"vibe/internal/running"
	"vibe/internal/sbxinstall"
	"vibe/internal/selfupdate"
)

// fakeReleaseFeed serves a GitHub releases page whose Atom feed lists tags,
// newest first, and whose API listing is empty, for as long as the test
// runs. It returns the releases' URL and the API's.
func fakeReleaseFeed(t *testing.T, tags ...string) (releases, api string) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases.atom":
			fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom">`)
			for _, tag := range tags {
				fmt.Fprintf(w, `<entry><link rel="alternate" type="text/html" href="%s/releases/tag/%s"/></entry>`, srv.URL, tag)
			}
			fmt.Fprint(w, `</feed>`)
		case "/api":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/releases", srv.URL + "/api"
}

// onReleaseFeeds makes vibe's releases vibeTags, and sbx's sbxTags.
func onReleaseFeeds(t *testing.T, vibeTags, sbxTags []string) {
	t.Helper()
	oldVibe, oldVibeAPI, oldSbx, oldSbxAPI := selfupdate.Releases, selfupdate.API, sbxinstall.Releases, sbxinstall.API
	selfupdate.Releases, selfupdate.API = fakeReleaseFeed(t, vibeTags...)
	sbxinstall.Releases, sbxinstall.API = fakeReleaseFeed(t, sbxTags...)
	t.Cleanup(func() {
		selfupdate.Releases, selfupdate.API, sbxinstall.Releases, sbxinstall.API = oldVibe, oldVibeAPI, oldSbx, oldSbxAPI
	})
}

func TestCheckVibeDowngrade(t *testing.T) {
	v := vibe.Version
	onReleaseFeeds(t, []string{"v" + v + ".235", "v" + v + ".228"}, nil)

	asBuild(t, "235")
	d, summary, err := checkVibeDowngrade("")
	if err != nil || d == nil {
		t.Fatalf("checkVibeDowngrade = %v, %q, %v; want a downgrade", d, summary, err)
	}
	if want := "vibe " + v + ".235 → " + v + ".228"; d.label != want {
		t.Errorf("label = %q, want %q", d.label, want)
	}
	if want := "vibe " + v + ".228 is the release before yours (" + v + ".235)"; summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}

	asBuild(t, "228")
	if d, summary, err := checkVibeDowngrade(""); err != nil || d != nil || summary != "vibe "+v+".228 is the oldest release: there is none before it to go back to" {
		t.Errorf("checkVibeDowngrade at the oldest = %v, %q, %v", d, summary, err)
	}
}

// A development build has no build number, so there is no release it came
// after: nothing is offered, and it says why.
func TestCheckVibeDowngradeOffersADevelopmentBuildNothing(t *testing.T) {
	onReleaseFeeds(t, nil, nil)
	selfupdate.Releases = "http://127.0.0.1:1/releases" // never asked
	asBuild(t, "")
	d, summary, err := checkVibeDowngrade("")
	if err != nil || d != nil || summary != "vibe "+vibe.Version+" is a development build, not a release: there is no release before it to go back to" {
		t.Errorf("checkVibeDowngrade from a development build = %v, %q, %v", d, summary, err)
	}
}

func TestCheckSbxDowngrade(t *testing.T) {
	onReleaseFeeds(t, nil, []string{"v0.54.0", "v0.54.0-rc2", "nightly-2026-11-10", "v0.53.0", "v0.52.1"})
	for _, tt := range []struct {
		installed, label, summary string
	}{
		{"v0.54.0", "Docker SBX v0.54.0 → v0.53.0", "Docker SBX v0.53.0 is the stable release before yours (v0.54.0)"},
		{"v0.54.0-rc2", "Docker SBX v0.54.0-rc2 → v0.53.0", "Docker SBX v0.53.0 is the stable release before yours (v0.54.0-rc2)"},
		{"v0.52.1", "", "Docker SBX v0.52.1 is the oldest stable release: there is none before it to go back to"},
		{"", "", "Docker SBX isn't installed — vibe --install-sbx installs it"},
	} {
		withSbx(t, tt.installed)
		d, summary, err := checkSbxDowngrade()
		if err != nil {
			t.Fatalf("checkSbxDowngrade with %q installed: %v", tt.installed, err)
		}
		label := ""
		if d != nil {
			label = d.label
		}
		if label != tt.label || summary != tt.summary {
			t.Errorf("checkSbxDowngrade with %q installed = %q, %q; want %q, %q", tt.installed, label, summary, tt.label, tt.summary)
		}
	}
}

func TestCheckSbxDowngradeLeavesAnSbxItDidntInstall(t *testing.T) {
	onReleaseFeeds(t, nil, []string{"v0.54.0", "v0.53.0"})
	withSbx(t, "")
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, "sbx"), "#!/bin/sh\necho 'sbx version v0.54.0'\n")
	os.Chmod(filepath.Join(elsewhere, "sbx"), 0o755)
	t.Setenv("PATH", elsewhere)
	d, summary, err := checkSbxDowngrade()
	if err != nil || d != nil || !strings.Contains(summary, "wasn't installed by vibe --install-sbx") {
		t.Errorf("checkSbxDowngrade = %v, %q, %v", d, summary, err)
	}
}

// withDowngradePicks makes interactive() true for the rest of the test, and
// pickDowngrades answer with pick.
func withDowngradePicks(t *testing.T, pick func(labels []string, checked []bool) ([]int, bool)) {
	t.Helper()
	withATerminal(t, nil)
	old := pickDowngrades
	pickDowngrades = pick
	t.Cleanup(func() { pickDowngrades = old })
}

func TestChooseDowngradesStartsWithNothingTicked(t *testing.T) {
	found := []pendingUpgrade{{label: "vibe 0.1.235 → 0.1.228"}, {label: "Docker SBX v0.54.0 → v0.53.0"}}
	withDowngradePicks(t, func(labels []string, checked []bool) ([]int, bool) {
		if !reflect.DeepEqual(checked, []bool{false, false}) {
			t.Errorf("offered %v ticked %v; want nothing ticked", labels, checked)
		}
		return []int{1}, true
	})
	got, err := chooseDowngrades(found)
	if err != nil || len(got) != 1 || got[0].label != found[1].label {
		t.Errorf("chooseDowngrades picking the second = %v, %v", got, err)
	}

	pickDowngrades = func([]string, []bool) ([]int, bool) { return nil, true }
	if _, err := chooseDowngrades(found); err == nil || err.Error() != "nothing selected — nothing changed" {
		t.Errorf("chooseDowngrades picking nothing = %v", err)
	}
	pickDowngrades = func([]string, []bool) ([]int, bool) { return nil, false }
	if _, err := chooseDowngrades(found); err == nil || err.Error() != "aborted — nothing changed" {
		t.Errorf("chooseDowngrades when the user quits = %v", err)
	}
}

// With no terminal to choose on, nothing is downgraded: not everything, as
// --upgrade would, and without even asking GitHub.
func TestDowngradeWithNoTerminalChangesNothing(t *testing.T) {
	onReleases(t, "v0.1.1", "v0.46.0")
	selfupdate.Releases = "http://127.0.0.1:1/releases" // never asked
	err := doDowngrade(t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "no terminal") || !strings.Contains(err.Error(), "nothing changed") {
		t.Errorf("doDowngrade with no terminal = %v", err)
	}
}

// heldElsewhere has another vibe's session hold folder in vibeHome, as far
// as --downgrade can tell, for the rest of the test.
func heldElsewhere(t *testing.T, vibeHome, folder string) {
	t.Helper()
	old := otherVibes
	otherVibes = func(home string) []running.FolderClaim {
		if home != vibeHome {
			t.Errorf("asked about vibes running from %s, want %s", home, vibeHome)
		}
		return []running.FolderClaim{{Folder: folder, Claim: running.Claim{PID: 4321}}}
	}
	t.Cleanup(func() { otherVibes = old })
}

func TestDowngradeRefusesWhileAVibeSessionRuns(t *testing.T) {
	onReleaseFeeds(t, nil, nil)
	selfupdate.Releases = "http://127.0.0.1:1/releases" // never asked
	withDowngradePicks(t, func([]string, []bool) ([]int, bool) {
		t.Error("asked what to downgrade while a session runs")
		return nil, false
	})
	vibeHome := t.TempDir()
	heldElsewhere(t, vibeHome, "/work/a")
	err := doDowngrade(vibeHome, "")
	if err == nil || !strings.Contains(err.Error(), "/work/a") || !strings.Contains(err.Error(), "nothing changed") {
		t.Errorf("doDowngrade while a session runs = %v; want a refusal naming the folder", err)
	}
}

func TestApplyDowngradesBacksUpVibesHomeFirst(t *testing.T) {
	root := t.TempDir()
	vibeHome := filepath.Join(root, ".vibe")
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), "memory: 8g\n")
	writeFile(t, filepath.Join(vibeHome, "logs", "x.log"), "noise\n")
	var backups []string
	ran := 0
	chosen := []pendingUpgrade{{label: "a", run: func() error {
		ran++
		backups, _ = filepath.Glob(filepath.Join(root, ".vibe-backup-*.zip"))
		return nil
	}}}
	if err := applyDowngrades(vibeHome, chosen, time.Date(2026, 11, 17, 9, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	if ran != 1 || len(backups) != 1 || filepath.Base(backups[0]) != ".vibe-backup-2026-11-17_090000.zip" {
		t.Errorf("ran %d times, with backups %v beside ~/.vibe when it did; want one, made first", ran, backups)
	}
}

func TestApplyDowngradesStopsWhenTheBackupFails(t *testing.T) {
	root := t.TempDir()
	vibeHome := filepath.Join(root, ".vibe")
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), "memory: 8g\n")
	os.Chmod(root, 0o555) // nowhere to write the backup
	t.Cleanup(func() { os.Chmod(root, 0o755) })
	if f, err := os.Create(filepath.Join(root, "probe")); err == nil {
		f.Close()
		t.Skip("running as a user who can write anywhere")
	}
	chosen := []pendingUpgrade{{label: "a", run: func() error {
		t.Error("downgraded with no backup made")
		return nil
	}}}
	err := applyDowngrades(vibeHome, chosen, time.Now())
	if err == nil || !strings.Contains(err.Error(), "back up") || !strings.Contains(err.Error(), "nothing changed") {
		t.Errorf("applyDowngrades with the backup failing = %v", err)
	}
}

// A session started while the user was choosing still stops it.
func TestApplyDowngradesRefusesASessionStartedSinceAsking(t *testing.T) {
	vibeHome := t.TempDir()
	heldElsewhere(t, vibeHome, "/work/b")
	chosen := []pendingUpgrade{{label: "a", run: func() error {
		t.Error("downgraded while a session runs")
		return nil
	}}}
	if err := applyDowngrades(vibeHome, chosen, time.Now()); err == nil || !strings.Contains(err.Error(), "/work/b") {
		t.Errorf("applyDowngrades while a session runs = %v", err)
	}
}

func TestApplyDowngradesGoesOnPastOneThatFails(t *testing.T) {
	vibeHome := filepath.Join(t.TempDir(), ".vibe")
	ran := 0
	chosen := []pendingUpgrade{
		{label: "a", run: func() error { ran++; return errors.New("a broke") }},
		{label: "b", run: func() error { ran++; return nil }},
	}
	err := applyDowngrades(vibeHome, chosen, time.Now())
	if ran != 2 || err == nil || !strings.Contains(err.Error(), "a broke") {
		t.Errorf("applyDowngrades = %v after running %d; want both run, and a's error", err, ran)
	}
}
