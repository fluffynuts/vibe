//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A turn runs the command conversationCommand builds in a shell, so what the
// page chose has to survive that shell's quoting and reach claude as separate
// arguments: this runs the real command against a claude that records them.
func TestATurnAfterAChoiceCarriesItToClaudeAndOneBeforeDoesNot(t *testing.T) {
	bin, work := t.TempDir(), t.TempDir()
	record := filepath.Join(work, "args")
	standIn := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + record + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(standIn), 0o755); err != nil {
		t.Fatal(err)
	}
	c, _ := choiceHome(t, "")
	turn := func() []string {
		t.Helper()
		choice, err := c.forTurn()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", "-c", conversationCommand(work, "abc-123", filepath.Join(work, "t.pid"), choice)[4])
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the turn failed: %v\n%s", err, out)
		}
		data, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}

	before := turn()
	for _, flag := range []string{"--model", "--effort"} {
		if slices.Contains(before, flag) {
			t.Errorf("before any choice, claude was given %s: %q", flag, before)
		}
	}

	if err := c.set("sonnet[1m]", "high"); err != nil {
		t.Fatal(err)
	}
	after := turn()
	for _, pair := range [][2]string{{"--model", "sonnet[1m]"}, {"--effort", "high"}, {"--resume", "abc-123"}} {
		i := slices.Index(after, pair[0])
		if i < 0 || i+1 >= len(after) || after[i+1] != pair[1] {
			t.Errorf("claude wasn't given %s %s as separate arguments: %q", pair[0], pair[1], after)
		}
	}

	if err := c.set("", ""); err != nil {
		t.Fatal(err)
	}
	if again := turn(); slices.Contains(again, "--model") || slices.Contains(again, "--effort") {
		t.Errorf("after choosing Default again, claude was still given a choice: %q", again)
	}
}
