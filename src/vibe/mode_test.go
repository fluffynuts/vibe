package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/running"
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

		"a favourite headless, but no sandbox yet: a terminal to make it": {modeWanted{Favourite: "headless"}, false, "no sandbox"},
		"a favourite headless with a rebuild: the rebuild makes it":       {modeWanted{Favourite: "headless", Rebuilds: true}, true, ""},
		"a favourite headless, deleting: not a start at all":              {modeWanted{Favourite: "headless", Deleting: true}, false, ""},
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
		got, err := favouriteMode(favouriteHome(t, settings))
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", settings, got, err, want)
		}
	}
}

func TestAFavouriteModeThatIsNeitherIsRefusedWhereToFixIt(t *testing.T) {
	_, err := favouriteMode(favouriteHome(t, "mode: headles\n"))
	if err == nil || !strings.Contains(err.Error(), "settings.yaml") || !strings.Contains(err.Error(), "headless") || !strings.Contains(err.Error(), "terminal") {
		t.Errorf("err = %v", err)
	}
}

func TestAMisspeltModeSettingOnlyStopsAStartThatNeededIt(t *testing.T) {
	lay := favouriteHome(t, "mode: headles\n")
	for name, args := range map[string]cliargs.Args{"--terminal": {Terminal: true}, "--headless": {Headless: true}, "--delete": {Delete: true}} {
		if _, err := startMode(lay, args, "box", modeNone); err != nil {
			t.Errorf("%s was stopped by a setting it doesn't use: %v", name, err)
		}
	}
	if _, err := startMode(lay, cliargs.Args{Restart: true}, "box", modeHeadless); err != nil {
		t.Errorf("--restart that ended a headless vibe was stopped by a setting it doesn't use: %v", err)
	}
	if _, err := startMode(lay, cliargs.Args{}, "box", modeNone); err == nil {
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
		got, err := startMode(lay, tc.args, "box", tc.ended)
		if err != nil || got != tc.want {
			t.Errorf("%s: %v, %v; want %v", name, got, err, tc.want)
		}
	}
}

func TestTheNudgeIsPrintedForARestartThatNeededNoFlag(t *testing.T) {
	var got bool
	said := notesOf(func() {
		got, _ = startMode(favouriteHome(t, ""), cliargs.Args{Restart: true, Headless: true}, "box", modeHeadless)
	})
	if !got || len(said) != 1 || !strings.Contains(said[0], "vibe --restart") {
		t.Errorf("headless %v, said %q", got, said)
	}
}
