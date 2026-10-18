package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/hostmem"
)

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
