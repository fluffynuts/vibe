package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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
	desk := newThemeDesk(t, lay)
	got := desk.Themes(context.Background())
	if got.Current != "teal" || got.Live || len(got.Options) != 3 || got.Options[0] != "default" {
		t.Errorf("themes: %+v", got)
	}
}

func TestChoosingAThemeForASandboxThatIsntRunningIsRecordedForItsNextBuild(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, profile := themeHome(t)
	desk := newThemeDesk(t, lay)
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
	desk := newThemeDesk(t, lay)
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
	desk := newThemeDesk(t, lay)
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
	desk := newThemeDesk(t, lay)
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
	desk := newThemeDesk(t, lay)
	got := desk.Themes(context.Background())
	if got.Current != "default" || len(got.Options) != 3 {
		t.Errorf("themes: %+v", got)
	}
}

// sandboxTheme stands in for reading the theme from the sandbox: each read counts in reads, and waits for release when it is
// set.
func sandboxTheme(t *testing.T, choice string, release chan struct{}) *atomic.Int32 {
	t.Helper()
	reads := new(atomic.Int32)
	old := themeFromSandbox
	t.Cleanup(func() { themeFromSandbox = old })
	themeFromSandbox = func(ctx context.Context, name string) (string, bool) {
		reads.Add(1)
		if release != nil {
			<-release
		}
		return choice, true
	}
	return reads
}

// newThemeDesk is a desk for sandbox "box" of profile "p", whose checks of the sandbox are over before the test's stand-ins
// (made before it) are put back.
func newThemeDesk(t *testing.T, lay layout.Layout) *themeDesk {
	t.Helper()
	desk := &themeDesk{lay: lay, name: "box", inst: state.Instance{Profile: "p"}}
	t.Cleanup(func() {
		for desk.isChecking() {
			time.Sleep(time.Millisecond)
		}
	})
	return desk
}

// checked waits for the desk's check of the sandbox to have been made.
func checked(t *testing.T, desk *themeDesk) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !desk.hasChecked() {
		if time.Now().After(deadline) {
			t.Fatal("the sandbox's theme was never checked")
		}
		time.Sleep(time.Millisecond)
	}
}

// Asking the sandbox can be slow: the drop-down is given the themes and the one last known at once, and corrected once the
// sandbox has said.
func TestTheThemeDeskAnswersAtOnceAndChecksTheSandboxMeanwhile(t *testing.T) {
	keepAwakeStandIns(t, true, true)
	lay, profile := themeHome(t)
	claudetheme.SaveToProfile(profile, "teal")
	release := make(chan struct{})
	sandboxTheme(t, "amber", release)
	desk := newThemeDesk(t, lay)
	began := time.Now()
	got := desk.Themes(context.Background())
	if took := time.Since(began); took > time.Second {
		t.Errorf("the themes waited %s for the sandbox", took)
	}
	if got.Current != "teal" || !got.Checking || len(got.Options) != 3 {
		t.Errorf("before the sandbox has said: %+v, want the profile's teal, said to be being checked", got)
	}
	close(release)
	checked(t, desk)
	got = desk.Themes(context.Background())
	if got.Current != "amber" || !got.Live || got.Checking {
		t.Errorf("once the sandbox has said: %+v, want amber, live", got)
	}
}

func TestTheThemeDeskAsksTheSandboxOnceForManyLooksAndForThePagesColourToo(t *testing.T) {
	keepAwakeStandIns(t, true, true)
	lay, _ := themeHome(t)
	reads := sandboxTheme(t, "amber", nil)
	desk := newThemeDesk(t, lay)
	for i := 0; i < 5; i++ {
		desk.Themes(context.Background())
		desk.color()
	}
	checked(t, desk)
	for i := 0; i < 5; i++ {
		desk.Themes(context.Background())
		desk.color()
	}
	if n := reads.Load(); n != 1 {
		t.Errorf("the sandbox was asked %d times, want 1", n)
	}
	if c := desk.color(); c != "#ffaa00" {
		t.Errorf("the page's colour = %q, want amber's", c)
	}
}

func TestAThemeJustChosenIsTheOneShownWithoutAskingTheSandbox(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	lay, _ := themeHome(t)
	desk := newThemeDesk(t, lay)
	if _, err := desk.SetTheme(context.Background(), "amber"); err != nil {
		t.Fatal(err)
	}
	if got := desk.Themes(context.Background()); got.Current != "amber" || got.Live || got.Checking {
		t.Errorf("after choosing amber in a stopped sandbox: %+v", got)
	}
}
