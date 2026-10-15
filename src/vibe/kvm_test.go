package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/companion"
)

// noKVM stands in for a machine whose /dev/kvm the user can't use.
func noKVM(t *testing.T) error {
	t.Helper()
	err := errors.New("you can't read and write /dev/kvm, so sbx can't start its VMs.")
	old := vmCheck
	t.Cleanup(func() { vmCheck = old })
	vmCheck = func() error { return err }
	return err
}

func TestWhatStartsAVMIsChecked(t *testing.T) {
	for argv, want := range map[string]bool{
		"":                   true, // create or attach
		"-N":                 true, // creates
		"--headless":         true,
		"--restart":          true,
		"-r":                 true,
		"-R":                 true,
		"-C":                 true,
		"--restore":          true,
		"--update-sandboxes": true,
		"--ssh":              true,
		"--git-token":        true,
		"--stop":             false,
		"--cancel":           false,
		"--info":             false,
		"--delete":           false,
	} {
		args, err := cliargs.Parse(strings.Fields(argv))
		if err != nil {
			t.Fatalf("%q: %v", argv, err)
		}
		if got := startsAVM(args); got != want {
			t.Errorf("%q starts a VM: %v, want %v", argv, got, want)
		}
	}
}

func TestNothingIsAskedOrDoneWhenAVMCantStart(t *testing.T) {
	want := noKVM(t)
	calls := loggingSbx(t, "")
	t.Setenv("VIBE_HOME", t.TempDir())
	err := run([]string{"-f", t.TempDir()})
	if !errors.Is(err, want) {
		t.Errorf("run = %v, want the KVM advice", err)
	}
	data, _ := os.ReadFile(calls)
	for _, call := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if call != "" && call != "version" {
			t.Errorf("sbx was asked to %q", call)
		}
	}
}

func TestThePageStartsNoVMWhenOneCantStart(t *testing.T) {
	want := noKVM(t)
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	calls := loggingSbx(t, "box stopped\\n")

	a := &companionAdmin{lay: lay, name: "box", target: inst.Target}
	for _, kind := range []string{companion.AdminStart, companion.AdminReInit} {
		if err := a.do(context.Background(), companion.AdminAction{Kind: kind}); !errors.Is(err, want) {
			t.Errorf("Admin %s = %v, want the KVM advice", kind, err)
		}
	}
	m := &companionManager{lay: lay}
	s := &inventorySandbox{inst: inst, inSbx: true}
	for _, kind := range []string{companion.ManageStart, companion.ManageRebuild, companion.ManageUpdate} {
		if err := m.do(context.Background(), kind, &inventoryProfile{name: "p"}, s); !errors.Is(err, want) {
			t.Errorf("Profiles %s = %v, want the KVM advice", kind, err)
		}
	}
	if said := keepAwakeFor("box")(); !strings.Contains(said, want.Error()) {
		t.Errorf("keeping awake said %q, want the KVM advice", said)
	}
	data, _ := os.ReadFile(calls)
	for _, call := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(call, "start") || strings.HasPrefix(call, "rm") || strings.HasPrefix(call, "create") {
			t.Errorf("sbx was asked to %q", call)
		}
	}
}
