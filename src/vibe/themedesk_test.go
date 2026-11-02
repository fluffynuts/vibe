package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"vibe/internal/claudetheme"
	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/state"
)

// themeHome is a vibe home with two themes in its defaults, and a profile "p" in it.
func themeHome(t *testing.T) (layout.Layout, string) {
	t.Helper()
	home := t.TempDir()
	themes := filepath.Join(home, "defaults", "agent-files", ".claude", "themes")
	os.MkdirAll(themes, 0o755)
	os.WriteFile(filepath.Join(themes, "amber.json"), []byte(`{"overrides":{"promptBorder":"#ffaa00"}}`), 0o644)
	os.WriteFile(filepath.Join(themes, "teal.json"), []byte(`{"overrides":{"promptBorder":"#00aaaa"}}`), 0o644)
	profile := filepath.Join(home, "profiles", "p")
	os.MkdirAll(profile, 0o755)
	return layout.New(home, t.TempDir()), profile
}

func TestTheThemeDeskOffersTheDefaultAndTheKitsThemesAndSaysWhichIsRecorded(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, profile := themeHome(t)
	claudetheme.SaveToProfile(profile, "teal")
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}}
	got := desk.Themes(context.Background())
	if got.Current != "teal" || got.Live || len(got.Options) != 3 || got.Options[0] != "default" {
		t.Errorf("themes: %+v", got)
	}
}

func TestChoosingAThemeForASandboxThatIsntRunningIsRecordedForItsNextBuild(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, profile := themeHome(t)
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}}
	set, err := desk.SetTheme(context.Background(), "amber")
	if err != nil {
		t.Fatal(err)
	}
	if set.Live || set.Color != "#ffaa00" {
		t.Errorf("set: %+v", set)
	}
	if got, ok := claudetheme.FromProfile(profile); !ok || got != "amber" {
		t.Errorf("the profile has %q, %v", got, ok)
	}
}

func TestAThemeNotOnOfferIsRefusedAndNothingIsRecorded(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, profile := themeHome(t)
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}}
	for _, bad := range []string{"nope", "dark", "a b; rm -rf /"} {
		if _, err := desk.SetTheme(context.Background(), bad); !errors.Is(err, companion.ErrNoSuchTheme) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if got, ok := claudetheme.FromProfile(profile); ok {
		t.Errorf("something was recorded: %q", got)
	}
}

func TestChoosingTheDefaultSetsNoColour(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, _ := themeHome(t)
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}}
	set, err := desk.SetTheme(context.Background(), "default")
	if err != nil || set.Color != "" {
		t.Errorf("set: %+v, %v", set, err)
	}
}

// A theme Claude has built in (the user ran /theme) isn't one the kit offers, but it is the one in force: the list
// has it too, or the drop-down would show nothing chosen.
func TestABuiltInThemeInForceIsInTheListOfThemes(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, profile := themeHome(t)
	claudetheme.SaveToProfile(profile, "light")
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}}
	got := desk.Themes(context.Background())
	if got.Current != "light" {
		t.Fatalf("current: %q", got.Current)
	}
	found := false
	for _, o := range got.Options {
		found = found || o == "light"
	}
	if !found {
		t.Errorf("the current theme isn't among the options: %v", got.Options)
	}
	if got.Options[0] != "default" || len(got.Options) != 4 {
		t.Errorf("options: %v", got.Options)
	}
}

func TestAThemeTheKitDoesntHaveIsNotMadeUpInTheList(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, profile := themeHome(t)
	claudetheme.SaveToProfile(profile, "gone")
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}}
	got := desk.Themes(context.Background())
	if got.Current != "default" || len(got.Options) != 3 {
		t.Errorf("themes: %+v", got)
	}
}

// An agent with no themes of its own (cursor, so far) still has the page's accent: the choice is recorded in its
// profile and read from there, and nothing is written into the sandbox.
func TestAnAgentWithoutThemesOfItsOwnHasTheChoiceRecordedAndNothingWrittenIntoItsSandbox(t *testing.T) {
	keepAwakeStandIns(t, true, false) // running: a Claude sandbox would be written into, and no sbx is here
	lay, profile := themeHome(t)
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}, agent: "cursor"}
	set, err := desk.SetTheme(context.Background(), "amber")
	if err != nil {
		t.Fatalf("it went to the sandbox, which it should not for this agent: %v", err)
	}
	if set.AgentThemes || set.Live || set.Color != "#ffaa00" {
		t.Errorf("set: %+v", set)
	}
	if got, ok := claudetheme.FromProfile(profile); !ok || got != "amber" {
		t.Errorf("the profile has %q, %v", got, ok)
	}
	themes := desk.Themes(context.Background())
	if themes.Current != "amber" || themes.AgentThemes || themes.Live {
		t.Errorf("themes: %+v", themes)
	}
}

func TestClaudeHasThemesOfItsOwnAndCursorDoesNot(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, _ := themeHome(t)
	for agent, want := range map[string]bool{"": true, "claude": true, "cursor": false, "opencode": false} {
		desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}, agent: agent}
		if got := desk.Themes(context.Background()).AgentThemes; got != want {
			t.Errorf("agent %q: AgentThemes = %v, want %v", agent, got, want)
		}
	}
}

func TestThePagesColourForAnAgentWithoutThemesComesFromTheProfileAlone(t *testing.T) {
	keepAwakeStandIns(t, true, false) // running, and no sbx here: the sandbox must not be asked
	lay, profile := themeHome(t)
	inst := state.Instance{Profile: "p"}
	if got := themeColorFor(lay, "box", inst, "cursor")(); got != "" {
		t.Errorf("no choice recorded, and the colour is %q", got)
	}
	claudetheme.SaveToProfile(profile, "teal")
	if got := themeColorFor(lay, "box", inst, "cursor")(); got != "#00aaaa" {
		t.Errorf("the colour is %q", got)
	}
}
