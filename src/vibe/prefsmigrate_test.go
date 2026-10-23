package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/profilegen"
	"vibe/internal/settings"
	"vibe/internal/userprefs"
)

func settingsOf(lay layout.Layout) string { return lay.HomePath("profiles", "p", "settings.yaml") }

// oldVibeSet does to the composed profile's settings.yaml what the vibe that
// kept the model and effort there did: set each key, rewriting the whole file
// in its own layout.
func oldVibeSet(t *testing.T, lay layout.Layout, keyValues ...string) {
	t.Helper()
	data, err := os.ReadFile(settingsOf(lay))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for i := 0; i < len(keyValues); i += 2 {
		if data, err = settings.Set(data, keyValues[i], keyValues[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(settingsOf(lay), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func prefsOf(t *testing.T, lay layout.Layout) userprefs.Prefs {
	t.Helper()
	got, err := userprefs.Load(lay.HomePath("profiles", "p"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAChoiceEarlierVibesWroteInSettingsMovesToThePreferencesAndTheProfileIsNotEditedAfter(t *testing.T) {
	lay := updateLayout(t)
	composed, _ := os.ReadFile(settingsOf(lay))
	oldVibeSet(t, lay, "model", "opus", "effort", "high")
	left, err := migrateChoiceKeys(lay, "p")
	if err != nil || left {
		t.Fatalf("left %v, err %v", left, err)
	}
	if got := prefsOf(t, lay); got.Model != "opus" || got.Effort != "high" {
		t.Errorf("prefs %+v", got)
	}
	if data, _ := os.ReadFile(settingsOf(lay)); string(data) != string(composed) {
		t.Errorf("settings.yaml = %q, want it as composing wrote it: %q", data, composed)
	}
	changeLibrary(t, lay)
	if got := updated(t, lay); got.state != profileUpdated {
		t.Errorf("outcome = %+v: the profile still looks edited by hand", got)
	}
}

func TestOtherSettingsAreKeptAndTheKeysAreLeftWhereTakingThemOutWouldLeaveTheProfileLookingEdited(t *testing.T) {
	lay := updateLayout(t)
	oldVibeSet(t, lay, "memory", "24g", "model", "opus")
	before, _ := os.ReadFile(settingsOf(lay))
	left, err := migrateChoiceKeys(lay, "p")
	if err != nil || !left {
		t.Fatalf("left %v, err %v: the profile was already edited by hand, so removing the key can't restore it", left, err)
	}
	if got := prefsOf(t, lay); got.Model != "opus" {
		t.Errorf("prefs %+v: the value must be where it is read first", got)
	}
	if data, _ := os.ReadFile(settingsOf(lay)); string(data) != string(before) {
		t.Errorf("settings.yaml was changed: %q", data)
	}
}

func TestAProfileVibeDidNotComposeLosesTheKeysAndKeepsTheRest(t *testing.T) {
	home, bundle := t.TempDir(), t.TempDir()
	lay := layout.New(home, bundle)
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	writeFile(t, settingsOf(lay), "# mine\nmemory: 8g\nmodel: opus\neffort: low\n")
	left, err := migrateChoiceKeys(lay, "p")
	if err != nil || left {
		t.Fatalf("left %v, err %v", left, err)
	}
	data, _ := os.ReadFile(settingsOf(lay))
	if !strings.Contains(string(data), "memory: 8g") || !strings.Contains(string(data), "# mine") || strings.Contains(string(data), "model") || strings.Contains(string(data), "effort") {
		t.Errorf("settings.yaml = %q", data)
	}
	if got := prefsOf(t, lay); got.Model != "opus" || got.Effort != "low" {
		t.Errorf("prefs %+v", got)
	}
}

func TestAChoiceMadeSinceWinsOverTheOldKeys(t *testing.T) {
	lay := updateLayout(t)
	dir := lay.HomePath("profiles", "p")
	userprefs.Update(dir, func(p *userprefs.Prefs) { p.Model = "sonnet" })
	oldVibeSet(t, lay, "model", "opus", "effort", "high")
	if _, err := migrateChoiceKeys(lay, "p"); err != nil {
		t.Fatal(err)
	}
	if got := prefsOf(t, lay); got.Model != "sonnet" || got.Effort != "high" {
		t.Errorf("prefs %+v: what the user chose since is the newer word, and the old effort fills the gap", got)
	}
}

func TestMigratingAgainOrWithNothingToMoveChangesNothing(t *testing.T) {
	lay := updateLayout(t)
	if left, err := migrateChoiceKeys(lay, "p"); err != nil || left {
		t.Errorf("left %v, err %v", left, err)
	}
	if _, err := os.Stat(userprefs.Path(lay.HomePath("profiles", "p"))); err == nil {
		t.Error("a preferences file was made for nothing")
	}
	oldVibeSet(t, lay, "model", "opus")
	migrateChoiceKeys(lay, "p")
	before, _ := os.ReadFile(userprefs.Path(lay.HomePath("profiles", "p")))
	migrateChoiceKeys(lay, "p")
	after, _ := os.ReadFile(userprefs.Path(lay.HomePath("profiles", "p")))
	if string(before) != string(after) || len(before) == 0 {
		t.Errorf("before %q, after %q", before, after)
	}
}

func TestASettingsFileThatCannotBeReadIsSaidAndNothingIsChanged(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, settingsOf(lay), "model: [unclosed\n")
	if _, err := migrateChoiceKeys(lay, "p"); err == nil {
		t.Error("no error")
	}
	if data, _ := os.ReadFile(settingsOf(lay)); string(data) != "model: [unclosed\n" {
		t.Errorf("settings.yaml = %q", data)
	}
	if _, err := os.Stat(userprefs.Path(lay.HomePath("profiles", "p"))); err == nil {
		t.Error("preferences were made from a file that couldn't be read")
	}
}

func TestTheBundlesProfileIsNeverWrittenTo(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	bundled := filepath.Join(root, "profiles", bundledProfile, "settings.yaml")
	writeFile(t, bundled, "model: opus\n")
	if left, err := migrateChoiceKeys(lay, bundledProfile); err != nil || left {
		t.Errorf("left %v, err %v", left, err)
	}
	if data, _ := os.ReadFile(bundled); string(data) != "model: opus\n" {
		t.Errorf("the bundle's settings.yaml was changed: %q", data)
	}
	if _, err := os.Stat(userprefs.Path(filepath.Join(root, "profiles", bundledProfile))); err == nil {
		t.Error("preferences were written into the bundle")
	}
}

func TestAGeneratedProfileThatHasNoReadmeIsGivenOneByTheMigration(t *testing.T) {
	lay := updateLayout(t)
	readme := lay.HomePath("profiles", "p", profilegen.ReadmeFile)
	os.Remove(readme)
	migratePreferences(lay, "p")
	data, err := os.ReadFile(readme)
	if err != nil || !strings.Contains(string(data), "generated by vibe") {
		t.Errorf("readme = %q, %v", data, err)
	}
	changeLibrary(t, lay)
	if got := updated(t, lay); got.state != profileUpdated {
		t.Errorf("outcome = %+v: a readme made the profile look edited", got)
	}
}

func TestTheMigrationLeavesAReadmeTheUserWroteAndAProfileNobodyGeneratedAlone(t *testing.T) {
	lay := updateLayout(t)
	readme := lay.HomePath("profiles", "p", profilegen.ReadmeFile)
	writeFile(t, readme, "mine\n")
	migratePreferences(lay, "p")
	if data, _ := os.ReadFile(readme); string(data) != "mine\n" {
		t.Errorf("readme = %q", data)
	}
	other := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, other.HomePath("profiles", "hand", "config.yaml"), "name: hand\n")
	migratePreferences(other, "hand")
	if _, err := os.Stat(other.HomePath("profiles", "hand", profilegen.ReadmeFile)); err == nil {
		t.Error("a profile nobody generated was given a readme")
	}
}

func TestAReComposedProfileHasAReadmeAgain(t *testing.T) {
	lay := updateLayout(t)
	readme := lay.HomePath("profiles", "p", profilegen.ReadmeFile)
	os.Remove(readme)
	if err := replaceProfile(lay, "p", "box", []string{"mine"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(readme); err != nil {
		t.Errorf("no readme after re-composing: %v", err)
	}
}
