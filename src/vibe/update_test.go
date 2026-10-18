package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"vibe/internal/backup"
	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/profilegen"
	"vibe/internal/prompt"
	"vibe/internal/running"
	"vibe/internal/state"

	"vibe/internal/sbxtest"
)

func fingerprint(t *testing.T, lay layout.Layout, agent string) string {
	t.Helper()
	sum, err := kitFingerprint(lay, bundledProfile, agent)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func TestAKitsFingerprintIsTheSameEachTime(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	if fingerprint(t, lay, "claude") != fingerprint(t, lay, "claude") {
		t.Error("the same kit fingerprinted twice gave different fingerprints")
	}
}

// Sandboxes sharing a profile can differ in theme, and rebuilding one
// records its theme in the profile: were the theme counted, they'd take
// turns being out of date.
func TestTheThemeIsntPartOfAKitsFingerprint(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	before := fingerprint(t, lay, "claude")
	writeFile(t, filepath.Join(root, "profiles", bundledProfile, "claude-theme"), "dark\n")
	if fingerprint(t, lay, "claude") != before {
		t.Error("recording a theme changed the fingerprint")
	}
}

func TestAKitsFingerprintChangesWithWhatTheSandboxIsBuiltFrom(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	before := fingerprint(t, lay, "claude")

	if fingerprint(t, lay, "codex") == before {
		t.Error("a different agent gave the same fingerprint")
	}
	script := filepath.Join(root, "profiles", bundledProfile, "install-scripts", "01-extra")
	writeFile(t, script, "#!/bin/sh\necho extra\n")
	if fingerprint(t, lay, "claude") == before {
		t.Error("a new install script gave the same fingerprint")
	}
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "profiles", bundledProfile, "settings.yaml"), "env:\n  A: b\n")
	if fingerprint(t, lay, "claude") == before {
		t.Error("a new environment variable gave the same fingerprint")
	}
}

// updateLayout is the real bundle, with a feature of the user's own, "mine",
// in ~/.vibe/library, and a guided profile "p" composed from it.
func updateLayout(t *testing.T) layout.Layout {
	t.Helper()
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	writeFile(t, lay.HomePath("library", "mine", "install-scripts", "01-mine"), "#!/bin/sh\necho v1\n")
	if _, err := profilegen.CreateGuided(lay, "p", []string{"mine"}); err != nil {
		t.Fatal(err)
	}
	return lay
}

// mineScript reads the install script "mine" put in profile "p": numbered
// after the base features'.
func mineScript(t *testing.T, lay layout.Layout) string {
	t.Helper()
	found, _ := filepath.Glob(lay.HomePath("profiles", "p", "install-scripts", "*-mine"))
	if len(found) != 1 {
		t.Fatalf("expected one install script from 'mine' in profile 'p', found %v", found)
	}
	data, err := os.ReadFile(found[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// changeLibrary changes the feature profile "p" is composed from.
func changeLibrary(t *testing.T, lay layout.Layout) {
	t.Helper()
	writeFile(t, lay.HomePath("library", "mine", "install-scripts", "01-mine"), "#!/bin/sh\necho v2\n")
}

func updated(t *testing.T, lay layout.Layout) profileOutcome {
	t.Helper()
	outcome, err := updateProfile(lay, "p", "box")
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func backups(t *testing.T, lay layout.Layout) int {
	t.Helper()
	list, err := backup.List(lay.HomePath("backup"), "box")
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

func TestAnUpToDateProfileIsLeftAlone(t *testing.T) {
	lay := updateLayout(t)
	if got := updated(t, lay); got.state != profileCurrent {
		t.Errorf("outcome = %+v, want it up to date", got)
	}
	if backups(t, lay) != 0 {
		t.Error("an up-to-date profile was backed up")
	}
}

func TestAnOutOfDateProfileIsReComposedKeepingTheUsersFiles(t *testing.T) {
	lay := updateLayout(t)
	dir := lay.HomePath("profiles", "p")
	writeFile(t, filepath.Join(dir, "agent-instructions.md"), "be brief\n")
	writeFile(t, filepath.Join(dir, "claude-theme"), "dark\n")
	changeLibrary(t, lay)

	if got := updated(t, lay); got.state != profileUpdated {
		t.Fatalf("outcome = %+v, want it re-composed", got)
	}
	if got := mineScript(t, lay); !strings.Contains(got, "v2") {
		t.Errorf("the profile doesn't have the library's change: %q", got)
	}
	for file, want := range map[string]string{"agent-instructions.md": "be brief\n", "claude-theme": "dark\n"} {
		if data, _ := os.ReadFile(filepath.Join(dir, file)); string(data) != want {
			t.Errorf("%s = %q, want it carried across", file, data)
		}
	}
	if backups(t, lay) != 1 {
		t.Error("the profile wasn't backed up first")
	}
	if got := updated(t, lay); got.state != profileCurrent {
		t.Errorf("after re-composing: outcome = %+v, want it up to date", got)
	}
}

func TestAHandEditedProfileIsLeftForTheUser(t *testing.T) {
	lay := updateLayout(t)
	path := lay.HomePath("profiles", "p", "settings.yaml")
	writeFile(t, path, "memory: 24g\n")
	changeLibrary(t, lay)

	got := updated(t, lay)
	if got.state != profileEdited || !strings.Contains(got.why, "changed by hand") || !strings.Contains(got.why, "vibe -C") {
		t.Errorf("outcome = %+v, want it left for the user, saying why and what to do", got)
	}
	if data, _ := os.ReadFile(path); string(data) != "memory: 24g\n" {
		t.Errorf("the hand edit was lost: %q", data)
	}
}

func TestAProfileWithAFeatureTheLibraryLostIsLeftForTheUser(t *testing.T) {
	lay := updateLayout(t)
	if err := os.RemoveAll(lay.HomePath("library", "mine")); err != nil {
		t.Fatal(err)
	}
	if got := updated(t, lay); got.state != profileMissingFeatures || !strings.Contains(got.why, "mine") {
		t.Errorf("outcome = %+v, want it left for the user, naming the feature", got)
	}
}

func TestProfilesVibeDoesntComposeAreSkipped(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "hand", "config.yaml"), "name: hand\n")
	for _, profile := range []string{"hand", bundledProfile} {
		got, err := updateProfile(lay, profile, "box")
		if err != nil || got.state != profileNotComposed {
			t.Errorf("updateProfile(%s) = %+v, %v; want it skipped", profile, got, err)
		}
	}
}

// One from before hashes were recorded is adopted when it's up to date.
func TestAnUnrecordedProfileThatsUpToDateIsAdopted(t *testing.T) {
	lay := updateLayout(t)
	dir := lay.HomePath("profiles", "p")
	if err := os.Remove(filepath.Join(dir, profilegen.HashFile)); err != nil {
		t.Fatal(err)
	}
	if got := updated(t, lay); got.state != profileCurrent {
		t.Errorf("outcome = %+v, want it up to date", got)
	}
	if profilegen.RecordedHash(dir) == "" {
		t.Error("its hash wasn't recorded")
	}
}

// sandboxFor records a sandbox "box" on profile "p", built from what "p" is
// now when built is set.
func sandboxFor(t *testing.T, lay layout.Layout, built bool) state.Instance {
	t.Helper()
	inst := state.Instance{Name: "box", Profile: "p", Target: t.TempDir(), Agent: "claude"}
	if built {
		var err error
		if inst.KitHash, err = kitFingerprint(lay, "p", "claude"); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

// standIns replaces the user at the countdown with answer, and rebuilding
// with counting the rebuilds.
func standIns(t *testing.T, answer prompt.CountdownAnswer) (rebuilds *int, asked *bool) {
	t.Helper()
	rebuilds, asked = new(int), new(bool)
	oldConfirm, oldRebuild := confirmRebuild, rebuild
	t.Cleanup(func() { confirmRebuild, rebuild = oldConfirm, oldRebuild })
	confirmRebuild = func() prompt.CountdownAnswer {
		*asked = true
		return answer
	}
	rebuild = func(context.Context, layout.Layout, string, string) error {
		*rebuilds++
		return nil
	}
	return rebuilds, asked
}

func TestAnUpToDateSandboxStartsWithoutARebuild(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxFor(t, lay, true)
	rebuilds, asked := standIns(t, prompt.CountdownGo)
	if err := autoUpdate(lay, cliargs.Args{}, "box", inst.Target); err != nil {
		t.Fatal(err)
	}
	if *rebuilds != 0 || *asked {
		t.Errorf("rebuilt %d times, counted down: %v; want neither", *rebuilds, *asked)
	}
}

func TestAnOutOfDateSandboxIsRebuiltAtTheStart(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxFor(t, lay, true)
	changeLibrary(t, lay)
	rebuilds, asked := standIns(t, prompt.CountdownGo)
	var err error
	notes := strings.Join(notesOf(func() { err = autoUpdate(lay, cliargs.Args{}, "box", inst.Target) }), "\n")
	if err != nil {
		t.Fatal(err)
	}
	if *rebuilds != 1 || !*asked {
		t.Errorf("rebuilt %d times, counted down: %v; want once, after counting down", *rebuilds, *asked)
	}
	if !strings.Contains(notes, "sandbox 'box' is out of date: what it's built from has changed") {
		t.Errorf("notes don't say why it was rebuilt:\n%s", notes)
	}
}

func TestASandboxAHeadlessVibeIsUsingIsntRebuiltUnderIt(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxFor(t, lay, true)
	changeLibrary(t, lay)
	rebuilds, asked := standIns(t, prompt.CountdownGo)
	headlessHere = []int{4242}
	t.Cleanup(func() { headlessHere = nil })
	var err error
	notes := strings.Join(notesOf(func() { err = autoUpdate(lay, cliargs.Args{}, "box", inst.Target) }), "\n")
	if err != nil {
		t.Fatal(err)
	}
	if *rebuilds != 0 || *asked {
		t.Errorf("rebuilt %d times, counted down: %v; want neither", *rebuilds, *asked)
	}
	if !strings.Contains(notes, "out of date") || !strings.Contains(notes, "PID 4242") || !strings.Contains(notes, "vibe --restart") {
		t.Errorf("notes don't say why it wasn't rebuilt, or how to:\n%s", notes)
	}
}

func TestASandboxFromBeforeFingerprintsIsOutOfDate(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxFor(t, lay, false)
	rebuilds, _ := standIns(t, prompt.CountdownGo)
	var err error
	notes := strings.Join(notesOf(func() { err = autoUpdate(lay, cliargs.Args{}, "box", inst.Target) }), "\n")
	if err != nil {
		t.Fatal(err)
	}
	if *rebuilds != 1 || !strings.Contains(notes, "before vibe kept track") {
		t.Errorf("rebuilt %d times, saying:\n%s", *rebuilds, notes)
	}
}

func TestTheCountdownCanSkipOrQuit(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxFor(t, lay, false)

	rebuilds, _ := standIns(t, prompt.CountdownSkip)
	if err := autoUpdate(lay, cliargs.Args{}, "box", inst.Target); err != nil || *rebuilds != 0 {
		t.Errorf("skip: %v, rebuilt %d times; want the session to go on, unrebuilt", err, *rebuilds)
	}
	rebuilds, _ = standIns(t, prompt.CountdownQuit)
	err := autoUpdate(lay, cliargs.Args{}, "box", inst.Target)
	if err == nil || !strings.Contains(err.Error(), "--no-auto-update") || *rebuilds != 0 {
		t.Errorf("quit: %v, rebuilt %d times; want an error naming the way out, unrebuilt", err, *rebuilds)
	}
}

func TestAutoUpdateCanBeTurnedOff(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxFor(t, lay, false)
	changeLibrary(t, lay)
	rebuilds, _ := standIns(t, prompt.CountdownGo)

	if err := autoUpdate(lay, cliargs.Args{NoAutoUpdate: true}, "box", inst.Target); err != nil || *rebuilds != 0 {
		t.Errorf("--no-auto-update: %v, rebuilt %d times", err, *rebuilds)
	}
	writeFile(t, lay.HomePath("settings.yaml"), "autoUpdate: false\n")
	if err := autoUpdate(lay, cliargs.Args{}, "box", inst.Target); err != nil || *rebuilds != 0 {
		t.Errorf("autoUpdate: false: %v, rebuilt %d times", err, *rebuilds)
	}
	if strings.Contains(mineScript(t, lay), "v2") {
		t.Error("the profile was re-composed with updating turned off")
	}
}

// sbxListing puts an sbx on PATH whose 'sbx ls' lists rows.
func sbxListing(t *testing.T, rows string) {
	sbxtest.OnPath(t, sbxLsJSON(rows)+"exit 0\n")
}

// sbxLsJSON is a stand-in sbx's answer to 'sbx ls --json': rows, each
// "name state\\n", as the array of objects sbx prints.
func sbxLsJSON(rows string) string {
	return "[ \"$*\" = \"ls --json\" ] && { printf '" + rows + "' | " +
		`awk 'BEGIN { printf "[" } NF { if (n++) printf ","; printf "{\"name\":\"%s\",\"status\":\"%s\"}", $1, $2 } END { print "]" }'; exit 0; }` + "\n"
}

// batchStandIns records what --update-sandboxes rebuilds, starts and stops,
// failing the rebuilds of the sandboxes in failing.
func batchStandIns(t *testing.T, failing ...string) *[]string {
	t.Helper()
	var did []string
	oldRebuild, oldStart, oldStop := rebuild, startRebuilt, stopRebuilt
	t.Cleanup(func() { rebuild, startRebuilt, stopRebuilt = oldRebuild, oldStart, oldStop })
	rebuild = func(_ context.Context, lay layout.Layout, name, _ string) error {
		did = append(did, "rebuild "+name)
		if slices.Contains(failing, name) {
			return errors.New("sbx said no")
		}
		// As a real rebuild does, recording what it was built from.
		inst, _, err := state.Load(lay.Home, name)
		if err != nil {
			return err
		}
		if inst.KitHash, err = kitFingerprint(lay, inst.Profile, inst.Agent); err != nil {
			return err
		}
		return state.Save(lay.Home, inst)
	}
	startRebuilt = func(_ context.Context, name string) bool { did = append(did, "start "+name); return true }
	stopRebuilt = func(_ context.Context, name string) error { did = append(did, "stop "+name); return nil }
	return &did
}

func saveSandbox(t *testing.T, lay layout.Layout, name string, built bool) {
	t.Helper()
	inst := state.Instance{Name: name, Profile: "p", Target: t.TempDir(), Agent: "claude"}
	if built {
		var err error
		if inst.KitHash, err = kitFingerprint(lay, "p", "claude"); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSandboxesBringsEverythingUpToDate(t *testing.T) {
	lay := updateLayout(t)
	saveSandbox(t, lay, "busy", true) // running
	saveSandbox(t, lay, "idle", true) // stopped
	saveSandbox(t, lay, "gone", true) // sbx no longer has it
	changeLibrary(t, lay)
	sbxListing(t, "busy running\\nidle stopped\\n")
	did := batchStandIns(t)

	var err error
	notes := strings.Join(notesOf(func() { err = doUpdateSandboxes(lay, true) }), "\n")
	if err != nil {
		t.Fatalf("doUpdateSandboxes: %v\n%s", err, notes)
	}
	if !strings.Contains(mineScript(t, lay), "v2") {
		t.Error("the profile wasn't re-composed")
	}
	want := []string{"rebuild busy", "start busy", "rebuild idle", "stop idle"}
	if !slices.Equal(*did, want) {
		t.Errorf("did %v, want %v", *did, want)
	}
	for _, line := range []string{"sandbox 'gone' skipped: sbx has no sandbox by that name any more", "updated 3, already up to date 0, skipped 1, failed 0"} {
		if !strings.Contains(notes, line) {
			t.Errorf("notes don't say %q:\n%s", line, notes)
		}
	}

	// Everything is up to date now, so a second run does nothing.
	*did = nil
	notes = strings.Join(notesOf(func() { err = doUpdateSandboxes(lay, true) }), "\n")
	if err != nil || len(*did) != 0 {
		t.Errorf("second run: %v, did %v; want nothing done", err, *did)
	}
	if !strings.Contains(notes, "updated 0, already up to date 3, skipped 1") {
		t.Errorf("second run's summary is wrong:\n%s", notes)
	}
}

func TestUpdateSandboxesCarriesOnPastAFailureAndSaysSo(t *testing.T) {
	lay := updateLayout(t)
	saveSandbox(t, lay, "a", false)
	saveSandbox(t, lay, "b", false)
	sbxListing(t, "a stopped\\nb stopped\\n")
	did := batchStandIns(t, "a")

	err := doUpdateSandboxes(lay, true)
	if err == nil || !strings.Contains(err.Error(), "sandbox 'a'") {
		t.Errorf("doUpdateSandboxes = %v, want it to name the sandbox that failed", err)
	}
	if !slices.Contains(*did, "rebuild b") {
		t.Errorf("did %v: it stopped at the failure", *did)
	}
}

// loggingSbx puts an sbx on PATH that lists rows for 'sbx ls', succeeds at
// everything else, and logs every call; it returns the log's path.
func loggingSbx(t *testing.T, rows string) string {
	// 'sbx ls --quiet' lists just the names, as sbx does.
	dir := sbxtest.OnPath(t, "echo \"$*\" >>\"$(dirname \"$0\")/calls\"\n"+
		"[ \"$1\" = ls ] && [ \"$2\" = --quiet ] && { printf '"+rows+"' | cut -d' ' -f1; exit 0; }\n"+
		sbxLsJSON(rows)+"exit 0\n")
	return filepath.Join(dir, "calls")
}

// A sandbox whose folder is gone can't be built again, so rebuilding it
// must not start by removing it.
func TestARebuildLeavesASandboxWhoseFolderIsGoneAlone(t *testing.T) {
	lay := updateLayout(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p", Target: gone, Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	log := loggingSbx(t, "box stopped\\n")

	err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", gone, true, false)
	if err == nil || !strings.Contains(err.Error(), gone) || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("rebuildSandbox = %v, want it to say the folder is gone", err)
	}
	if calls, _ := os.ReadFile(log); strings.Contains(string(calls), "rm") || strings.Contains(string(calls), "create") {
		t.Errorf("sbx was asked to remove or create the sandbox:\n%s", calls)
	}
}

func TestUpdateSandboxesSkipsASandboxWhoseFolderIsGone(t *testing.T) {
	lay := updateLayout(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p", Target: gone, Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	sbxListing(t, "box stopped\\n")
	did := batchStandIns(t)

	var err error
	notes := strings.Join(notesOf(func() { err = doUpdateSandboxes(lay, true) }), "\n")
	if err != nil {
		t.Errorf("doUpdateSandboxes = %v; a skipped sandbox isn't a failure", err)
	}
	if len(*did) != 0 {
		t.Errorf("did %v to a sandbox whose folder is gone", *did)
	}
	for _, want := range []string{"sandbox 'box' skipped: its folder " + gone + " no longer exists", "vibe --cleanup"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes don't say %q:\n%s", want, notes)
		}
	}
}

// answering stands in for the user at --update-sandboxes' question.
func answering(t *testing.T, yes bool) *int {
	t.Helper()
	asked := new(int)
	old := confirmUpdate
	t.Cleanup(func() { confirmUpdate = old })
	confirmUpdate = func(force bool) bool {
		if !force {
			*asked++
		}
		return force || yes
	}
	return asked
}

// Rebuilding is destructive: without -f, what's out of date is listed and
// asked about once, and saying no changes nothing.
func TestUpdateSandboxesAsksFirstWithoutForce(t *testing.T) {
	lay := updateLayout(t)
	saveSandbox(t, lay, "busy", true)
	changeLibrary(t, lay)
	sbxListing(t, "busy running\\n")
	did := batchStandIns(t)
	asked := answering(t, false)

	var err error
	notes := strings.Join(notesOf(func() { err = doUpdateSandboxes(lay, false) }), "\n")
	if err == nil || !strings.Contains(err.Error(), "nothing changed") {
		t.Errorf("doUpdateSandboxes = %v, want it to say nothing changed", err)
	}
	if *asked != 1 || len(*did) != 0 || strings.Contains(mineScript(t, lay), "v2") {
		t.Errorf("asked %d times, did %v, profile re-composed: %v; want one question and nothing done", *asked, *did, strings.Contains(mineScript(t, lay), "v2"))
	}
	for _, want := range []string{"profile 'p': re-composed", "sandbox 'busy': rebuilt"} {
		if !strings.Contains(notes, want) {
			t.Errorf("the list of what would be done doesn't say %q:\n%s", want, notes)
		}
	}

	*asked = 0
	answering(t, true)
	if err := doUpdateSandboxes(lay, false); err != nil || len(*did) == 0 {
		t.Errorf("after a yes: %v, did %v", err, *did)
	}
}

func TestUpdateSandboxesDoesntAskWhenTheresNothingToDo(t *testing.T) {
	lay := updateLayout(t)
	saveSandbox(t, lay, "busy", true)
	sbxListing(t, "busy running\\n")
	batchStandIns(t)
	asked := answering(t, false)
	if err := doUpdateSandboxes(lay, false); err != nil || *asked != 0 {
		t.Errorf("doUpdateSandboxes = %v, asked %d times; want nothing asked", err, *asked)
	}
}

// The Profiles page runs in a vibe that holds its own folder; rebuilding
// another sandbox from it must claim that sandbox's folder as well, not
// trip over its own claim.
func TestAPagesVibeCanRebuildAnotherSandbox(t *testing.T) {
	lay := updateLayout(t)
	own, err := running.Register(lay.Home, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer own.Release()
	batchStandIns(t)
	inst := sandboxRecord(t, lay, "other", "p").inst
	if err := rebuildAsItWas(context.Background(), lay, inst, false); err != nil {
		t.Errorf("rebuildAsItWas = %v, want the rebuild to go ahead", err)
	}
}

// A profile composed with a feature vibe has since retired is updated
// without it: the record rewritten without it is no hand edit.
func TestAProfileWithARetiredFeatureIsUpdatedWithoutIt(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	writeFile(t, lay.HomePath("library", "mine", "install-scripts", "01-mine"), "#!/bin/sh\necho v1\n")
	writeFile(t, lay.HomePath("library", "diffity", "install-scripts", "01-diffity"), "#!/bin/sh\necho old\n")
	if _, err := profilegen.CreateGuided(lay, "p", []string{"mine", "diffity"}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(lay.HomePath("library", "diffity")); err != nil {
		t.Fatal(err)
	}
	if got := updated(t, lay); got.state != profileUpdated {
		t.Fatalf("outcome = %+v, want it updated", got)
	}
	if found, _ := filepath.Glob(lay.HomePath("profiles", "p", "install-scripts", "*-diffity")); len(found) != 0 {
		t.Errorf("diffity's script is still there: %v", found)
	}
}
