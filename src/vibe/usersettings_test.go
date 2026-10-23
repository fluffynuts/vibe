package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/hostmem"
	"vibe/internal/settings"
)

func TestPickCPUsWithoutAsking(t *testing.T) {
	// The tests' machine has 16 CPUs: 1 to 15 can be given, from 8 up.
	for want, got := range map[int]int{0: 8, 1: 1, 4: 4, 15: 15, 16: 15, 99: 15, -1: 8} {
		if c := pickCPUs(want, false, ""); c != got {
			t.Errorf("pickCPUs(%d) on 16 CPUs = %d, want %d", want, c, got)
		}
	}
}

// With nobody to answer (the tests have no terminal), asking keeps where the
// choice starts, as it does for memory.
func TestPickCPUsAskingWithNoTerminalKeepsTheStart(t *testing.T) {
	for want, got := range map[int]int{0: 8, 3: 3, 99: 15} {
		if c := pickCPUs(want, true, "how many?"); c != got {
			t.Errorf("pickCPUs(%d) with no terminal to ask on = %d, want %d", want, c, got)
		}
	}
}

func TestPickMemoryWithoutAsking(t *testing.T) {
	onHost(t, 32)
	for want, got := range map[string]string{"12g": "12g", "6g": "6g", "24g": "16g", "lots": "16g"} {
		if m := pickMemory(want, false, ""); m != got {
			t.Errorf("pickMemory(%q) on 32 GB = %q, want %q", want, m, got)
		}
	}
	hostmem.Total = func() (uint64, error) { return 0, errors.New("no idea") }
	if m := pickMemory("24g", false, ""); m != "24g" {
		t.Errorf("pickMemory on an unknown machine = %q, want it kept", m)
	}
}

// The name last set for a repository's default branch is kept in the
// user's settings, to suggest next time, with the rest of the file left as
// it was.
func TestTheDefaultBranchSetIsRememberedWithTheRestOfTheSettingsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	writeFile(t, path, "# mine\nmemory: 8g\n")
	if got := preferredDefaultBranch(path)(); got != "" {
		t.Errorf("nothing set yet, but %q is suggested", got)
	}
	for _, name := range []string{"master", "main"} {
		if err := rememberDefaultBranch(path)(name); err != nil {
			t.Fatal(err)
		}
		if got := preferredDefaultBranch(path)(); got != name {
			t.Errorf("after setting %s, %q is suggested", name, got)
		}
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# mine") || !strings.Contains(string(data), "memory: 8g") {
		t.Errorf("the rest of the file wasn't kept:\n%s", data)
	}
}

func TestTheDefaultBranchIsRememberedWithNoSettingsFileYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := rememberDefaultBranch(path)("master"); err != nil {
		t.Fatal(err)
	}
	if got := preferredDefaultBranch(path)(); got != "master" {
		t.Errorf("suggested %q", got)
	}
}

func answeringMode(t *testing.T, pick func(labels []string, def int) (int, bool)) (asked *[]string, restore func()) {
	t.Helper()
	asked = new([]string)
	was := chooseMode
	chooseMode = func(question string, labels []string, def int) (int, bool) {
		*asked = append(*asked, question)
		return pick(labels, def)
	}
	return asked, func() { chooseMode = was }
}

func TestThePickedModeIsTerminalOrHeadless(t *testing.T) {
	for want, i := range map[string]int{"terminal": 0, "headless": 1} {
		_, restore := answeringMode(t, func([]string, int) (int, bool) { return i, true })
		got := pickMode("", "how?")
		restore()
		if got != want {
			t.Errorf("picked entry %d: %q, want %q", i, got, want)
		}
	}
}

func TestTheModeQuestionStartsOnWhatIsSetAndOnTerminalWhenNothingIs(t *testing.T) {
	for current, want := range map[string]int{"": 0, "terminal": 0, "headless": 1, "headles": 0} {
		var started int
		_, restore := answeringMode(t, func(labels []string, def int) (int, bool) {
			started = def
			if len(labels) != 2 || !strings.HasPrefix(labels[0], "terminal") || !strings.HasPrefix(labels[1], "headless") {
				t.Errorf("labels %q", labels)
			}
			return def, true
		})
		pickMode(current, "how?")
		restore()
		if started != want {
			t.Errorf("with %q set it started on %d, want %d", current, started, want)
		}
	}
}

func TestQuittingTheModeQuestionKeepsWhatWasSet(t *testing.T) {
	for current, want := range map[string]string{"": "terminal", "headless": "headless"} {
		_, restore := answeringMode(t, func([]string, int) (int, bool) { return 0, false })
		got := pickMode(current, "how?")
		restore()
		if got != want {
			t.Errorf("quitting with %q set kept %q, want %q", current, got, want)
		}
	}
}

func TestTheModeSetupWritesIsTheOneTheFavouriteModeReads(t *testing.T) {
	lay := favouriteHome(t, "memory: 8g\n")
	path := lay.HomePath("settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if data, err = settings.Set(data, "mode", "headless"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := favouriteMode(lay, "p"); err != nil || got != "headless" {
		t.Errorf("%q, %v", got, err)
	}
	if !strings.Contains(string(data), "memory: 8g") {
		t.Errorf("the rest of the file was lost:\n%s", data)
	}
}
