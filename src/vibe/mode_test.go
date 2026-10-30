package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/profilegen"
	"vibe/internal/running"
	"vibe/internal/sbxtest"
	"vibe/internal/userprefs"
)

func TestWhatARestartEndedIsTheModeItWasIn(t *testing.T) {
	for name, tc := range map[string]struct {
		claims []running.Claim
		want   runMode
	}{
		"nothing running":      {nil, modeNone},
		"a headless vibe":      {[]running.Claim{{PID: 7, Headless: true}}, modeHeadless},
		"a terminal's vibe":    {[]running.Claim{{PID: 7}}, modeTerminal},
		"both: the terminal's": {[]running.Claim{{PID: 8, Headless: true}, {PID: 9}}, modeTerminal},
	} {
		if got := modeOf(tc.claims); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestRestartSaysWhichModeItEnded(t *testing.T) {
	restartStandIns(t, false, true, true, false)
	for name, tc := range map[string]struct {
		claims []running.Claim
		want   runMode
	}{
		"headless": {[]running.Claim{{PID: 7, Headless: true}}, modeHeadless},
		"terminal": {[]running.Claim{{PID: 7}}, modeTerminal},
		"nothing":  {nil, modeNone},
	} {
		got, err := restart(t.TempDir(), "box", "/work", tc.claims, true)
		if err != nil || got != tc.want {
			t.Errorf("%s: %v, %v; want %v", name, got, err, tc.want)
		}
	}
}

func TestWhichModeToStartIn(t *testing.T) {
	for name, tc := range map[string]struct {
		want     modeWanted
		headless bool
		says     string // part of what it tells the user, "" for nothing
	}{
		"nothing said, nothing kept: a terminal": {modeWanted{}, false, ""},
		"--headless":                             {modeWanted{Headless: true}, true, ""},
		"--terminal over a favourite headless":   {modeWanted{Terminal: true, Favourite: "headless", SandboxExists: true}, false, ""},
		"the favourite, headless":                {modeWanted{Favourite: "headless", SandboxExists: true}, true, ""},
		"the favourite, terminal":                {modeWanted{Favourite: "terminal"}, false, ""},
		"--headless over a favourite terminal":   {modeWanted{Headless: true, Favourite: "terminal"}, true, ""},

		"--restart keeps the headless it ended":            {modeWanted{Restart: true, Ended: modeHeadless}, true, ""},
		"--restart keeps the terminal it ended":            {modeWanted{Restart: true, Ended: modeTerminal}, false, ""},
		"--restart keeps what it ended, not the favourite": {modeWanted{Restart: true, Ended: modeTerminal, Favourite: "headless", SandboxExists: true}, false, ""},
		"--restart with nothing ended: the favourite":      {modeWanted{Restart: true, Ended: modeNone, Favourite: "headless", SandboxExists: true}, true, ""},
		"--restart --terminal takes over a headless":       {modeWanted{Restart: true, Terminal: true, Ended: modeHeadless}, false, ""},
		"--restart --headless takes over a terminal":       {modeWanted{Restart: true, Headless: true, Ended: modeTerminal}, true, ""},

		"--restart --headless after headless: no need to say it": {modeWanted{Restart: true, Headless: true, Ended: modeHeadless}, true, "isn't needed"},
		"--restart --terminal after a terminal: no need either":  {modeWanted{Restart: true, Terminal: true, Ended: modeTerminal}, false, "isn't needed"},

		"a favourite headless, but no sandbox yet: made first, then headless": {modeWanted{Favourite: "headless"}, true, "making it here"},
		"a favourite headless with a rebuild: the rebuild makes it":           {modeWanted{Favourite: "headless", Rebuilds: true}, true, ""},
		"a favourite headless, deleting: not a start at all":                  {modeWanted{Favourite: "headless", Deleting: true}, false, ""},
	} {
		headless, says := tc.want.resolve()
		if headless != tc.headless {
			t.Errorf("%s: headless %v, want %v", name, headless, tc.headless)
		}
		if tc.says == "" && says != "" {
			t.Errorf("%s: said %q", name, says)
		}
		if tc.says != "" && !strings.Contains(says, tc.says) {
			t.Errorf("%s: said %q, want it to say %q", name, says, tc.says)
		}
	}
}

func TestTheNudgeSaysWhatToTypeInstead(t *testing.T) {
	_, says := modeWanted{Restart: true, Headless: true, Ended: modeHeadless}.resolve()
	if !strings.Contains(says, "vibe --restart") || !strings.Contains(says, "headless") {
		t.Errorf("said %q", says)
	}
}

func favouriteHome(t *testing.T, settings string) layout.Layout {
	t.Helper()
	home := t.TempDir()
	if settings != "" {
		if err := os.WriteFile(filepath.Join(home, "settings.yaml"), []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return layout.New(home, t.TempDir())
}

func TestTheFavouriteModeIsReadFromTheUsersSettings(t *testing.T) {
	for settings, want := range map[string]string{"": "", "mode: headless\n": "headless", "mode: terminal\n": "terminal", "memory: 8g\n": ""} {
		got, err := favouriteMode(favouriteHome(t, settings), "p")
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", settings, got, err, want)
		}
	}
}

func TestAFavouriteModeThatIsNeitherIsRefusedWhereToFixIt(t *testing.T) {
	_, err := favouriteMode(favouriteHome(t, "mode: headles\n"), "p")
	if err == nil || !strings.Contains(err.Error(), "settings.yaml") || !strings.Contains(err.Error(), "headless") || !strings.Contains(err.Error(), "terminal") {
		t.Errorf("err = %v", err)
	}
}

func TestAMisspeltModeSettingOnlyStopsAStartThatNeededIt(t *testing.T) {
	lay := favouriteHome(t, "mode: headles\n")
	for name, args := range map[string]cliargs.Args{"--terminal": {Terminal: true}, "--headless": {Headless: true}, "--delete": {Delete: true}} {
		if _, _, err := startMode(lay, args, "box", "/work", modeNone); err != nil {
			t.Errorf("%s was stopped by a setting it doesn't use: %v", name, err)
		}
	}
	if _, _, err := startMode(lay, cliargs.Args{Restart: true}, "box", "/work", modeHeadless); err != nil {
		t.Errorf("--restart that ended a headless vibe was stopped by a setting it doesn't use: %v", err)
	}
	if _, _, err := startMode(lay, cliargs.Args{}, "box", "/work", modeNone); err == nil {
		t.Error("a start that goes by the setting wasn't told it's wrong")
	}
}

func TestTheFavouriteDecidesAStartNothingElseHas(t *testing.T) {
	lay := favouriteHome(t, "mode: headless\n")
	for name, tc := range map[string]struct {
		args  cliargs.Args
		ended runMode
		want  bool
	}{
		"a rebuild: it makes the sandbox, then headless": {cliargs.Args{ReInit: true}, modeNone, true},
		"--terminal beats it":                            {cliargs.Args{Terminal: true}, modeNone, false},
		"--restart of a terminal keeps the terminal":     {cliargs.Args{Restart: true}, modeTerminal, false},
		"--restart of a headless keeps it":               {cliargs.Args{Restart: true}, modeHeadless, true},
	} {
		got, _, err := startMode(lay, tc.args, "box", "/work", tc.ended)
		if err != nil || got != tc.want {
			t.Errorf("%s: %v, %v; want %v", name, got, err, tc.want)
		}
	}
}

func TestTheNudgeIsPrintedForARestartThatNeededNoFlag(t *testing.T) {
	var got bool
	said := notesOf(func() {
		got, _, _ = startMode(favouriteHome(t, ""), cliargs.Args{Restart: true, Headless: true}, "box", "/work", modeHeadless)
	})
	if !got || len(said) != 1 || !strings.Contains(said[0], "vibe --restart") {
		t.Errorf("headless %v, said %q", got, said)
	}
}

// withProfile adds a profile "p" to home whose settings.yaml says settings.
func withProfile(t *testing.T, lay layout.Layout, settings string) {
	withProfileNamed(t, lay, "p", settings)
}

func withProfileNamed(t *testing.T, lay layout.Layout, name, settings string) {
	t.Helper()
	dir := filepath.Join(lay.Home, "profiles", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("name: p\n"), 0o644)
	if settings != "" {
		os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(settings), 0o644)
	}
}

func TestAProfilesModeOverridesTheSettingsAsEveryOtherSettingDoes(t *testing.T) {
	for name, tc := range map[string]struct{ global, profile, want string }{
		"the profile over the global":                        {"mode: headless\n", "mode: terminal\n", "terminal"},
		"the profile over nothing":                           {"", "mode: headless\n", "headless"},
		"the global when the profile's is silent":            {"mode: headless\n", "memory: 8g\n", "headless"},
		"the global when there's no settings in the profile": {"mode: headless\n", "", "headless"},
		"neither": {"", "", ""},
	} {
		lay := favouriteHome(t, tc.global)
		withProfile(t, lay, tc.profile)
		got, err := favouriteMode(lay, "p")
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, tc.want)
		}
	}
}

func TestAProfileThatIsntThereLeavesTheGlobalSettingsMode(t *testing.T) {
	got, err := favouriteMode(favouriteHome(t, "mode: headless\n"), "no-such-profile")
	if err != nil || got != "headless" {
		t.Errorf("%q, %v", got, err)
	}
}

func TestAWrongModeIsRefusedNamingTheFileItIsIn(t *testing.T) {
	lay := favouriteHome(t, "mode: headless\n")
	withProfile(t, lay, "mode: terminl\n")
	_, err := favouriteMode(lay, "p")
	if err == nil || !strings.Contains(err.Error(), filepath.Join("profiles", "p", "settings.yaml")) && !strings.Contains(err.Error(), "p/settings.yaml") {
		t.Errorf("a wrong mode in the profile: %v, want it to name the profile's settings.yaml", err)
	}
	lay = favouriteHome(t, "mode: headles\n")
	withProfile(t, lay, "mode: terminal\n")
	if got, err := favouriteMode(lay, "p"); err != nil || got != "terminal" {
		t.Errorf("a wrong global mode the profile overrides: %q, %v", got, err)
	}
}

func TestWithNoSettingsOfTheUsersTheBundlesAreTheGlobalOnes(t *testing.T) {
	lay := favouriteHome(t, "")
	os.WriteFile(filepath.Join(lay.Bundle, "settings.yaml"), []byte("mode: headless\n"), 0o644)
	if got, err := favouriteMode(lay, "p"); err != nil || got != "headless" {
		t.Errorf("%q, %v", got, err)
	}
}

func TestTheProfileIsFoundTheWayARebuildFindsIt(t *testing.T) {
	lay := favouriteHome(t, "mode: terminal\n")
	withProfile(t, lay, "mode: headless\n")
	// Named by --profile, with a rebuild asked for (which needs no sandbox looked up).
	got, _, err := startMode(lay, cliargs.Args{ReInit: true, Profile: "p"}, "box", "/work", modeNone)
	if err != nil || !got {
		t.Errorf("headless %v, %v; want the profile's headless", got, err)
	}
	// Named by nothing but the folder: the profile's the one derived from it (names are at least two characters).
	if derived := pathresolve.DeriveName("/code/widget"); derived != "widget" {
		t.Fatalf("names are derived differently now: %q", derived)
	}
	withProfileNamed(t, lay, "widget", "mode: headless\n")
	got, _, err = startMode(lay, cliargs.Args{ReInit: true}, "box", "/code/widget", modeNone)
	if err != nil || !got {
		t.Errorf("by the folder's name: headless %v, %v", got, err)
	}
	// Another profile: the global terminal.
	got, _, err = startMode(lay, cliargs.Args{ReInit: true, Profile: "other"}, "box", "/work", modeNone)
	if err != nil || got {
		t.Errorf("another profile: headless %v, %v; want the global terminal", got, err)
	}
}

func TestAProfilesPreferenceForTheModeWinsOverItsSettingsAndTheGlobalOnes(t *testing.T) {
	lay := favouriteHome(t, "mode: terminal\n")
	withProfile(t, lay, "mode: terminal\n")
	if err := userprefs.Update(lay.ProfileDir("p"), func(p *userprefs.Prefs) { p.Mode = "headless" }); err != nil {
		t.Fatal(err)
	}
	got, err := favouriteMode(lay, "p")
	if err != nil || got != "headless" {
		t.Errorf("%q, %v; want the preference's headless", got, err)
	}
}

func TestAWrongModeInThePreferencesIsRefusedNamingThatFile(t *testing.T) {
	lay := favouriteHome(t, "")
	withProfile(t, lay, "")
	if err := userprefs.Update(lay.ProfileDir("p"), func(p *userprefs.Prefs) { p.Mode = "headles" }); err != nil {
		t.Fatal(err)
	}
	_, err := favouriteMode(lay, "p")
	if err == nil || !strings.Contains(err.Error(), "user-preferences.yaml") {
		t.Errorf("err = %v, want it to name user-preferences.yaml", err)
	}
}

func TestAModeInThePreferencesDoesNotMakeAComposedProfileLookEditedByHand(t *testing.T) {
	lay := favouriteHome(t, "")
	withProfile(t, lay, "")
	dir := lay.ProfileDir("p")
	if err := profilegen.RecordHash(dir); err != nil {
		t.Fatal(err)
	}
	before, err := profilegen.ContentHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := userprefs.Update(dir, func(p *userprefs.Prefs) { p.Mode = "headless" }); err != nil {
		t.Fatal(err)
	}
	if after, err := profilegen.ContentHash(dir); err != nil || after != before {
		t.Errorf("the profile's hash changed (%v): a choice made it look edited by hand", err)
	}
}

func TestOnlyAFavouriteHeadlessStartWithNoSandboxMakesItFirst(t *testing.T) {
	for name, tc := range map[string]struct {
		want modeWanted
		make bool
	}{
		"the favourite, no sandbox":              {modeWanted{Favourite: "headless"}, true},
		"the favourite, a sandbox":               {modeWanted{Favourite: "headless", SandboxExists: true}, false},
		"--headless typed, no sandbox: refused":  {modeWanted{Headless: true, Favourite: "headless"}, false},
		"--terminal typed":                       {modeWanted{Terminal: true, Favourite: "headless"}, false},
		"a favourite terminal":                   {modeWanted{Favourite: "terminal"}, false},
		"a rebuild makes it itself":              {modeWanted{Favourite: "headless", Rebuilds: true}, false},
		"--restart of a vibe keeps its own mode": {modeWanted{Favourite: "headless", Restart: true, Ended: modeHeadless}, false},
		"deleting":                               {modeWanted{Favourite: "headless", Deleting: true}, false},
	} {
		if got := tc.want.createsFirst(); got != tc.make {
			t.Errorf("%s: createsFirst = %v, want %v", name, got, tc.make)
		}
	}
}

func TestAFavouriteHeadlessStartInAFolderWithNoSandboxSaysItMakesOneFirst(t *testing.T) {
	sbxtest.OnPath(t, "echo '[]'\n")
	lay := favouriteHome(t, "mode: headless\n")
	var headless, createFirst bool
	said := notesOf(func() {
		var err error
		if headless, createFirst, err = startMode(lay, cliargs.Args{}, "box", "/work", modeNone); err != nil {
			t.Error(err)
		}
	})
	if !headless || !createFirst || len(said) != 1 || !strings.Contains(said[0], "making it here") {
		t.Errorf("headless %v, createFirst %v, said %q", headless, createFirst, said)
	}
}
