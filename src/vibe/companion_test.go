package main

import (
	"os"
	"path/filepath"
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
	stop := startCompanion(lay, cliargs.Args{}, name)

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
	second := startCompanion(lay, cliargs.Args{}, name)
	second()
	if !companion.Running(inst.CompanionPort, name) {
		t.Error("the second vibe took the first one's page down")
	}

	stop()
	if companion.Running(inst.CompanionPort, name) {
		t.Error("the page is still served after stopping")
	}

	// And the next start has the same port.
	again := startCompanion(lay, cliargs.Args{}, name)
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
		stop := startCompanion(lay, c.args, sandbox)
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
