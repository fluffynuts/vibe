package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/state"
)

func TestCompanionMode(t *testing.T) {
	cases := []struct {
		noCompanion bool
		setting     string
		want        string
		wantErr     bool
	}{
		{false, "", "open", false},
		{false, "open", "open", false},
		{false, "serve", "serve", false},
		{false, "off", "off", false},
		{false, " Serve ", "serve", false},
		{true, "", "off", false},
		{true, "open", "off", false},
		{true, "bogus", "off", false},
		{false, "bogus", "", true},
		{false, "true", "", true},
	}
	for _, c := range cases {
		got, err := companionMode(c.noCompanion, c.setting)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("companionMode(%v, %q) = %q, %v; want %q (error: %v)", c.noCompanion, c.setting, got, err, c.want, c.wantErr)
		}
	}
}

func never(int) bool  { return false }
func always(int) bool { return true }

func TestCompanionPortStartsAtTheBase(t *testing.T) {
	port, served, err := chooseCompanionPort(nil, "vibe", never, never)
	if err != nil || served || port != companionBasePort {
		t.Errorf("port = %d, served = %v, err = %v", port, served, err)
	}
}

func TestCompanionPortKeepsTheOneItHad(t *testing.T) {
	instances := []state.Instance{{Name: "vibe", CompanionPort: 10033}}
	port, served, err := chooseCompanionPort(instances, "vibe", never, never)
	if err != nil || served || port != 10033 {
		t.Errorf("port = %d, served = %v, err = %v", port, served, err)
	}
}

func TestCompanionPortHeldByItsOwnPageIsServed(t *testing.T) {
	instances := []state.Instance{{Name: "vibe", CompanionPort: 10033}}
	port, served, err := chooseCompanionPort(instances, "vibe",
		func(p int) bool { return p == 10033 },
		func(p int) bool { return p == 10033 })
	if err != nil || !served || port != 10033 {
		t.Errorf("port = %d, served = %v, err = %v", port, served, err)
	}
}

func TestCompanionPortHeldBySomethingElseMovesOn(t *testing.T) {
	instances := []state.Instance{{Name: "vibe", CompanionPort: 10033}}
	port, served, err := chooseCompanionPort(instances, "vibe",
		func(p int) bool { return p == 10033 }, never)
	if err != nil || served || port == 10033 || port != companionBasePort {
		t.Errorf("port = %d, served = %v, err = %v", port, served, err)
	}
}

func TestCompanionPortAvoidsEveryOtherSandboxs(t *testing.T) {
	instances := []state.Instance{
		{Name: "vibe"},
		{Name: "a", CompanionPort: companionBasePort},
		{Name: "b", CompanionPort: companionBasePort + 1},
		{Name: "c", Publish: []state.PublishRecord{{Name: "x", HostPort: companionBasePort + 2}}},
	}
	port, _, err := chooseCompanionPort(instances, "vibe", never, never)
	if err != nil || port != companionBasePort+3 {
		t.Errorf("port = %d, err = %v", port, err)
	}
}

func TestCompanionPortDoesNotTakeAnotherSandboxsEvenIfRemembered(t *testing.T) {
	instances := []state.Instance{
		{Name: "vibe", CompanionPort: 10001},
		{Name: "other", CompanionPort: 10001},
	}
	port, _, err := chooseCompanionPort(instances, "vibe", never, never)
	if err != nil || port == 10001 {
		t.Errorf("port = %d, err = %v; took the one another sandbox has", port, err)
	}
}

func TestCompanionPortGivesUpWhenThereIsNoRoom(t *testing.T) {
	if _, _, err := chooseCompanionPort(nil, "vibe", always, never); err == nil {
		t.Error("expected an error")
	}
}

func companionFixture(t *testing.T, settingsYAML string) (layout.Layout, string) {
	t.Helper()
	home := t.TempDir()
	profileDir := filepath.Join(home, "profiles", "p")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "config.yaml"), []byte("name: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if settingsYAML != "" {
		if err := os.WriteFile(filepath.Join(profileDir, "settings.yaml"), []byte(settingsYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	name := "companion-test"
	if err := state.Save(home, state.Instance{Name: name, Profile: "p"}); err != nil {
		t.Fatal(err)
	}
	return layout.New(home, t.TempDir()), name
}

func TestStartCompanionServesAndRemembersItsPort(t *testing.T) {
	lay, name := companionFixture(t, "companion: serve\n")
	stop, _, _ := startCompanion(lay, cliargs.Args{}, name)

	inst, _, err := state.Load(lay.Home, name)
	if err != nil {
		t.Fatal(err)
	}
	if inst.CompanionPort == 0 {
		stop()
		t.Fatal("the port was not remembered")
	}
	if !companion.Running(inst.CompanionPort, name) {
		stop()
		t.Fatalf("nothing serves the page on %d", inst.CompanionPort)
	}

	// A second vibe for the same sandbox finds the first one's page and
	// leaves it be.
	second, _, _ := startCompanion(lay, cliargs.Args{}, name)
	second()
	if !companion.Running(inst.CompanionPort, name) {
		t.Error("the second vibe took the first one's page down")
	}

	stop()
	if companion.Running(inst.CompanionPort, name) {
		t.Error("the page is still served after stopping")
	}

	// And the next start has the same port.
	again, _, _ := startCompanion(lay, cliargs.Args{}, name)
	defer again()
	inst2, _, _ := state.Load(lay.Home, name)
	if inst2.CompanionPort != inst.CompanionPort {
		t.Errorf("port changed from %d to %d between starts", inst.CompanionPort, inst2.CompanionPort)
	}
}

func TestStartCompanionDoesNothingWhenOff(t *testing.T) {
	for name, c := range map[string]struct {
		yaml string
		args cliargs.Args
	}{
		"setting": {"companion: off\n", cliargs.Args{}},
		"flag":    {"companion: serve\n", cliargs.Args{NoCompanion: true}},
		"typo":    {"companion: sometimes\n", cliargs.Args{}},
	} {
		lay, sandbox := companionFixture(t, c.yaml)
		stop, _, _ := startCompanion(lay, c.args, sandbox)
		stop()
		inst, _, _ := state.Load(lay.Home, sandbox)
		if inst.CompanionPort != 0 {
			t.Errorf("%s: a page was set up on %d", name, inst.CompanionPort)
		}
		if _, err := os.Stat(filepath.Join(lay.Home, "companion", sandbox+".token")); err == nil {
			t.Errorf("%s: a token was made for a page that isn't served", name)
		}
	}
}

// keepAwakeStandIns stands in for sbx's answer to whether the sandbox is
// running, and for starting it, recording the starts.
func keepAwakeStandIns(t *testing.T, running, startWorks bool) *int {
	t.Helper()
	starts := new(int)
	oldRunning, oldStart := runningNow, startSandboxForPage
	t.Cleanup(func() {
		runningNow, startSandboxForPage = oldRunning, oldStart
		stoppedOnPurpose.Store(false)
		setAttached(false)
	})
	runningNow = func(context.Context, string) bool { return running }
	startSandboxForPage = func(context.Context, string) bool { *starts++; return startWorks }
	return starts
}

func TestKeepingAwakeStartsASandboxSbxStoppedWhileThePageWasAway(t *testing.T) {
	starts := keepAwakeStandIns(t, false, true)
	if said := keepAwakeFor("box")(); *starts != 1 || !strings.Contains(said, "started it again") {
		t.Errorf("starts %d, said %q", *starts, said)
	}
}

func TestKeepingAwakeLeavesASandboxStoppedOnPurpose(t *testing.T) {
	starts := keepAwakeStandIns(t, false, true)
	stoppedOnPurpose.Store(true)
	if said := keepAwakeFor("box")(); *starts != 0 || said != "" {
		t.Errorf("starts %d, said %q; want it left stopped", *starts, said)
	}
}

func TestKeepingAwakeDoesNothingWithATerminalAttached(t *testing.T) {
	starts := keepAwakeStandIns(t, false, true)
	setAttached(true)
	if said := keepAwakeFor("box")(); *starts != 0 || said != "" {
		t.Errorf("starts %d, said %q", *starts, said)
	}
}

func TestKeepingAwakeSaysWhenTheSandboxWontStart(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	if said := keepAwakeFor("box")(); !strings.Contains(said, "couldn't be started again") {
		t.Errorf("said %q", said)
	}
}

// A running sandbox is touched, which is what keeps sbx from stopping it.
func TestKeepingAwakeTouchesARunningSandbox(t *testing.T) {
	calls := loggingSbx(t, "box running\\n")
	keepAwakeStandIns(t, true, true)
	keepAwakeFor("box")()
	if data, _ := os.ReadFile(calls); !strings.Contains(string(data), "exec box -- true") {
		t.Errorf("sbx was asked:\n%s", data)
	}
}

// The page asks for the theme's colour every minute; for a stopped sandbox,
// asking it would start it again, so the profile's record stands in.
func TestTheThemeOfAStoppedSandboxDoesntStartIt(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	calls := loggingSbx(t, "box stopped\\n")
	themeColorFor(lay, "box", state.Instance{Name: "box", Profile: "p"}, "claude")()
	if data, _ := os.ReadFile(calls); strings.Contains(string(data), "exec") {
		t.Errorf("a stopped sandbox was asked for its theme:\n%s", data)
	}
}

func TestARunningSandboxIsReadyForATurnAsItIs(t *testing.T) {
	starts := keepAwakeStandIns(t, true, true)
	var said []string
	if err := readyForTurn("box")(context.Background(), func(s string) { said = append(said, s) }); err != nil {
		t.Fatal(err)
	}
	if *starts != 0 || len(said) != 0 {
		t.Errorf("started %d times, said %v; want neither", *starts, said)
	}
}

// sbx exec starts a stopped sandbox itself, but can give up while it boots
// ("context deadline exceeded"): the turn waits for it to be up instead.
func TestAStoppedSandboxIsStartedForATurnSayingSo(t *testing.T) {
	starts := keepAwakeStandIns(t, false, true)
	stoppedOnPurpose.Store(true)
	var said []string
	if err := readyForTurn("box")(context.Background(), func(s string) { said = append(said, s) }); err != nil {
		t.Fatal(err)
	}
	if *starts != 1 || len(said) != 1 || !strings.Contains(said[0], "starting") {
		t.Errorf("started %d times, said %v", *starts, said)
	}
	if stoppedOnPurpose.Load() {
		t.Error("started for a turn, it's still taken as stopped on purpose, so the page won't keep it awake")
	}
}

func TestATurnSaysWhenTheSandboxWontStart(t *testing.T) {
	keepAwakeStandIns(t, false, false)
	err := readyForTurn("box")(context.Background(), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "sbx ls") {
		t.Errorf("err = %v", err)
	}
}

func TestATurnSaysWhenNoVMCanStart(t *testing.T) {
	keepAwakeStandIns(t, false, true)
	want := noKVM(t)
	if err := readyForTurn("box")(context.Background(), func(string) {}); !errors.Is(err, want) {
		t.Errorf("err = %v, want the KVM advice", err)
	}
}

// A sandbox's folder inside a repository, not at its top, gets a review of
// the whole repository, which the agent's git can't see from the sandbox:
// vibe says so, and where to start it instead.
func TestAFolderBelowItsRepositorysTopIsWarnedAbout(t *testing.T) {
	need(t, "git")
	top := t.TempDir()
	if out, err := exec.Command("git", "-C", top, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	sub := filepath.Join(top, "sub")
	os.MkdirAll(sub, 0o755)
	notes := strings.Join(notesOf(func() { reviewTabFor(t.TempDir(), "box", state.Instance{Target: sub}) }), "\n")
	if !strings.Contains(notes, sub) || !strings.Contains(notes, "start vibe in "+top) {
		t.Errorf("notes: %q", notes)
	}
	if notes := notesOf(func() { reviewTabFor(t.TempDir(), "box", state.Instance{Target: top}) }); len(notes) != 0 {
		t.Errorf("at the top: %q", notes)
	}
}
