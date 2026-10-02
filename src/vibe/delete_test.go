package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vibe/internal/state"
)

func TestDeletionOptions(t *testing.T) {
	for _, tt := range []struct {
		name    string
		d       deletion
		labels  []string
		checked []bool
	}{
		{
			"both",
			deletion{name: "proj", sandbox: true, profile: "yumbi", profileDir: "/p"},
			[]string{"remove the sandbox proj", "remove the profile yumbi"},
			[]bool{true, false},
		},
		{
			"a shared profile",
			deletion{name: "proj", sandbox: true, profile: "yumbi", profileDir: "/p", sharedWith: []string{"a", "b"}},
			[]string{"remove the sandbox proj", "remove the profile yumbi (also used by a, b)"},
			[]bool{true, false},
		},
		{
			"no sandbox",
			deletion{name: "proj", profile: "yumbi", profileDir: "/p"},
			[]string{"remove the profile yumbi"},
			[]bool{false},
		},
		{
			"a profile only the bundle has",
			deletion{name: "proj", sandbox: true, profile: "yumbi"},
			[]string{"remove the sandbox proj"},
			[]bool{true},
		},
		{"nothing", deletion{name: "proj", profile: "yumbi"}, nil, nil},
	} {
		labels, checked, _ := tt.d.options()
		if !reflect.DeepEqual(labels, tt.labels) || !reflect.DeepEqual(checked, tt.checked) {
			t.Errorf("%s: options = %q, %v; want %q, %v", tt.name, labels, checked, tt.labels, tt.checked)
		}
	}
}

func TestDeletionChoose(t *testing.T) {
	d := deletion{name: "proj", sandbox: true, profile: "yumbi", profileDir: "/p"}

	if sandbox, profile, err := d.choose(true); err != nil || !sandbox || !profile {
		t.Errorf("choose with -f = %v, %v, %v; want both", sandbox, profile, err)
	}
	if sandbox, profile, err := (deletion{name: "proj", sandbox: true, profile: "yumbi"}).choose(true); err != nil || !sandbox || profile {
		t.Errorf("choose with -f and no overlay profile = %v, %v, %v; want the sandbox only", sandbox, profile, err)
	}
	if _, _, err := d.choose(false); err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("choose with no terminal = %v", err)
	}

	withATerminal(t, nil)
	var offered []bool
	pick := func(answer []int, ok bool) {
		old := pickDeletions
		pickDeletions = func(_ []string, checked []bool) ([]int, bool) {
			offered = checked
			return answer, ok
		}
		t.Cleanup(func() { pickDeletions = old })
	}
	pick([]int{0}, true)
	if sandbox, profile, err := d.choose(false); err != nil || !sandbox || profile {
		t.Errorf("choose ticking the sandbox = %v, %v, %v", sandbox, profile, err)
	}
	if !reflect.DeepEqual(offered, []bool{true, false}) {
		t.Errorf("offered ticked %v, want the sandbox only", offered)
	}
	pick([]int{1}, true)
	if sandbox, profile, err := d.choose(false); err != nil || sandbox || !profile {
		t.Errorf("choose ticking the profile = %v, %v, %v", sandbox, profile, err)
	}
	pick(nil, true)
	if sandbox, profile, err := d.choose(false); err != nil || sandbox || profile {
		t.Errorf("choose ticking nothing = %v, %v, %v", sandbox, profile, err)
	}
	pick(nil, false)
	if _, _, err := d.choose(false); err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Errorf("choose when the user quits = %v", err)
	}
}

func TestDeletionRun(t *testing.T) {
	setUp := func(t *testing.T) (string, deletion) {
		home := t.TempDir()
		dir := filepath.Join(home, "profiles", "yumbi")
		writeFile(t, filepath.Join(dir, "kit.yaml"), "x")
		if err := state.Save(home, state.Instance{Name: "proj", Profile: "yumbi", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		return home, deletion{name: "proj", sandbox: true, profile: "yumbi", profileDir: dir}
	}
	var removed []string
	remove := func(name string, force bool) error {
		removed = append(removed, name)
		return nil
	}

	home, d := setUp(t)
	if err := d.run(home, true, false, remove); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{"proj"}) {
		t.Errorf("removed %v, want proj", removed)
	}
	if _, found, _ := state.Load(home, "proj"); found {
		t.Error("the sandbox's instance record outlived it")
	}
	if _, err := os.Stat(d.profileDir); err != nil {
		t.Errorf("the profile went, unticked: %v", err)
	}

	removed = nil
	home, d = setUp(t)
	if err := d.run(home, false, true, remove); err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed %v, unticked", removed)
	}
	if _, err := os.Stat(d.profileDir); !os.IsNotExist(err) {
		t.Errorf("the profile is still there: %v", err)
	}
	if _, found, _ := state.Load(home, "proj"); !found {
		t.Error("the instance record went with the profile, though the sandbox stayed")
	}

	home, d = setUp(t)
	failing := func(string, bool) error { return errors.New("sbx said no") }
	if err := d.run(home, true, true, failing); err == nil || !strings.Contains(err.Error(), "sbx said no") {
		t.Errorf("run with sbx failing = %v", err)
	}
	if _, err := os.Stat(d.profileDir); err != nil {
		t.Errorf("the profile went though the sandbox couldn't be removed: %v", err)
	}
}
