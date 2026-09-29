package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// nonexistentSandbox is a name no sandbox on this machine can have, so
// asking about it always fails the same way whether or not sbx is installed.
const nonexistentSandbox = "vibe-test-no-such-sandbox-3f9c1"

func TestSupportedIsClaudeOnly(t *testing.T) {
	if !Supported("claude") {
		t.Error("claude should support memory preservation")
	}
	if Supported("codex") || Supported("") {
		t.Error("only claude has an equivalent memory path")
	}
}

// TestProbeSaysUnknownRatherThanNoneWhenItCannotAsk is the whole point of
// Presence having three states: a sandbox that can't be reached (stopped, or
// no sbx at all) must not be reported as having no memories, or --re-init
// destroys it without ever offering to keep them.
func TestProbeSaysUnknownRatherThanNoneWhenItCannotAsk(t *testing.T) {
	if got := Probe(nonexistentSandbox); got != Unknown {
		t.Errorf("Probe on an unreachable sandbox = %v, want Unknown", got)
	}
}

// TestBackupOfAnUnreachableSandboxReportsNothingSaved makes sure Unknown is
// never mistaken for "backed up" further down the line.
func TestBackupOfAnUnreachableSandboxReportsNothingSaved(t *testing.T) {
	saved, err := Backup(nonexistentSandbox, t.TempDir())
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if saved {
		t.Error("Backup claimed to have saved memories it could not read")
	}
}

func TestStoreForIsPerSandbox(t *testing.T) {
	if got, want := StoreFor("/root", "alpha"), "/root/alpha"; got != want {
		t.Errorf("StoreFor = %q, want %q", got, want)
	}
}

// fakeSbx puts an `sbx` on PATH whose exec runs the command on this machine.
// joined makes it pass the command on as one space-joined line to a shell,
// the way ssh does — the behaviour that broke a `sh -c "<script>"` probe.
func fakeSbx(t *testing.T, joined bool) {
	t.Helper()
	run := `"$@"`
	if joined {
		run = `sh -c "$*"`
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = exec ] || exit 1\nshift 3\n" + run + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// useAgentPath points the package at a stand-in for the sandbox's memories.
func useAgentPath(t *testing.T, path string) {
	t.Helper()
	old := agentPath
	agentPath = path
	t.Cleanup(func() { agentPath = old })
}

func TestProbeAndBackupFindMemoriesHoweverSbxPassesArgs(t *testing.T) {
	for _, joined := range []bool{false, true} {
		name := "argv"
		if joined {
			name = "joined"
		}
		t.Run(name, func(t *testing.T) {
			fakeSbx(t, joined)
			agent := filepath.Join(t.TempDir(), "projects")
			useAgentPath(t, agent)

			if got := Probe("any"); got != None {
				t.Errorf("Probe with no memory dir = %v, want None", got)
			}
			if err := os.MkdirAll(agent, 0o755); err != nil {
				t.Fatal(err)
			}
			if got := Probe("any"); got != None {
				t.Errorf("Probe with empty memory dir = %v, want None", got)
			}

			mem := filepath.Join(agent, "-proj", "memory", "MEMORY.md")
			if err := os.MkdirAll(filepath.Dir(mem), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(mem, []byte("- a memory\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := Probe("any"); got != Some {
				t.Fatalf("Probe with memories = %v, want Some", got)
			}

			store := filepath.Join(t.TempDir(), "store")
			saved, err := Backup("any", store)
			if err != nil || !saved {
				t.Fatalf("Backup = %v, %v; want true, nil", saved, err)
			}
			if _, err := os.Stat(filepath.Join(store, "-proj", "memory", "MEMORY.md")); err != nil {
				t.Errorf("memory not backed up: %v", err)
			}

			if err := os.RemoveAll(agent); err != nil {
				t.Fatal(err)
			}
			if err := Restore("any", store); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if _, err := os.Stat(mem); err != nil {
				t.Errorf("memory not restored: %v", err)
			}
		})
	}
}
