package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/layout"
)

// choiceHome is a ~/.vibe with a profile "p" in it, and a bundle with none.
func choiceHome(t *testing.T, profileSettings string) (profileChoice, string) {
	t.Helper()
	home, bundle := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, "profiles", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("name: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if profileSettings != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(profileSettings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return profileChoice{lay: layout.New(home, bundle), profile: "p"}, filepath.Join(dir, "settings.yaml")
}

func TestNothingChosenIsTheAgentsOwnDefault(t *testing.T) {
	c, _ := choiceHome(t, "")
	got, err := c.get()
	if err != nil || got != (turnChoice{}) {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestAChoiceIsKeptInTheProfilesSettingsWithTheRestOfThemLeftAlone(t *testing.T) {
	c, path := choiceHome(t, "# mine\nmemory: 8g\n")
	if err := c.set("opus", "high"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "opus", Effort: "high"}) {
		t.Errorf("got %+v", got)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# mine") || !strings.Contains(string(data), "memory: 8g") {
		t.Errorf("the rest of the file was lost:\n%s", data)
	}
}

func TestChoosingTheDefaultTakesTheKeysOutRatherThanLeavingEmptyOnes(t *testing.T) {
	c, path := choiceHome(t, "memory: 8g\n")
	if err := c.set("opus", "high"); err != nil {
		t.Fatal(err)
	}
	if err := c.set("", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "model") || strings.Contains(string(data), "effort") {
		t.Errorf("keys left behind:\n%s", data)
	}
	if got, _ := c.get(); got != (turnChoice{}) {
		t.Errorf("got %+v", got)
	}
}

func TestAProfileOverridesWhatTheGlobalSettingsChose(t *testing.T) {
	c, _ := choiceHome(t, "model: haiku\n")
	if err := os.WriteFile(filepath.Join(c.lay.Home, "settings.yaml"), []byte("model: opus\neffort: low\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "haiku", Effort: "low"}) {
		t.Errorf("got %+v", got)
	}
}

func TestAChoiceIsNotWrittenForAProfileThatIsNotThere(t *testing.T) {
	c, _ := choiceHome(t, "")
	c.profile = "nobody"
	err := c.set("opus", "")
	if err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Errorf("err = %v, want it to name the profile", err)
	}
	if _, statErr := os.Stat(filepath.Join(c.lay.Home, "profiles", "nobody")); statErr == nil {
		t.Error("a profile directory was made, which would hide the real one")
	}
}

func TestATurnRefusesAChoiceThatWasWrittenByHandAndIsntSafe(t *testing.T) {
	for _, settings := range []string{"model: \"x; rm -rf /\"\n", "effort: \"$(x)\"\n"} {
		c, _ := choiceHome(t, settings)
		if _, err := c.forTurn(); err == nil {
			t.Errorf("%q: no error", settings)
		} else if !strings.Contains(err.Error(), "settings.yaml") {
			t.Errorf("%q: %v doesn't say where to fix it", settings, err)
		}
	}
}
