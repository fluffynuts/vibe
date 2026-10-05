package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vibe/internal/fscopy"
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
	if got, want := StoreFor("/root", "alpha"), filepath.Join("/root", "alpha"); got != want {
		t.Errorf("StoreFor = %q, want %q", got, want)
	}
}

// fakeSbxEnv, set to "argv" or "joined", makes the test binary stand in for
// sbx (see TestMain and fakeSbx).
const fakeSbxEnv = "VIBE_MEMORY_TEST_FAKE_SBX"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeSbxEnv); mode != "" {
		os.Exit(runFakeSbx(mode == "joined", os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeSbx puts an `sbx` on PATH whose exec runs the command on this machine
// — a copy of this test binary, so it works on Windows too, where the
// commands the package runs in a sandbox are carried out by runFakeSbx.
// joined makes it treat the command as one space-joined line, the way ssh
// does — the behaviour that broke a `sh -c "<script>"` probe.
func fakeSbx(t *testing.T, joined bool) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if joined && strings.Contains(dir, " ") {
		t.Skip("a joined command line can't carry a temp dir with a space in it")
	}
	name := "sbx"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	mode := "argv"
	if joined {
		mode = "joined"
	}
	t.Setenv(fakeSbxEnv, mode)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// runFakeSbx is the fake sbx: `sbx exec <name> -- <command>`, for just the
// commands this package runs, carried out here with the same meaning they
// have in the sandbox. It returns the exit code.
func runFakeSbx(joined bool, args []string) int {
	if len(args) < 4 || args[0] != "exec" || args[2] != "--" {
		return 1
	}
	cmd := args[3:]
	if joined {
		cmd = strings.Fields(strings.Join(cmd, " "))
	}
	// is matches cmd's every word but the last, a path; "" matches any word.
	is := func(want ...string) bool {
		if len(cmd) != len(want)+1 {
			return false
		}
		for i, w := range want {
			if w != "" && cmd[i] != w {
				return false
			}
		}
		return true
	}
	fail := func(err error) int {
		if err != nil {
			return 1
		}
		return 0
	}
	path := cmd[len(cmd)-1]
	switch {
	case len(cmd) == 1 && cmd[0] == "/bin/true":
		return 0
	case is("test", "-d"):
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return 1
		}
		return 0
	case is("test", "-e"):
		_, err := os.Stat(path)
		return fail(err)
	case is("mkdir", "-p"):
		return fail(os.MkdirAll(path, 0o755))
	case is("cp", "-a", ""):
		src := strings.TrimSuffix(cmd[2], ".")
		return fail(fscopy.TreeKeepTimes(filepath.Clean(src), filepath.Clean(path)))
	case len(cmd) == 8 && cmd[0] == "find" && strings.Join(cmd[2:], " ") == "-mindepth 1 -maxdepth 1 -print -quit":
		entries, err := os.ReadDir(cmd[1])
		if err != nil {
			return 1
		}
		if len(entries) > 0 {
			fmt.Println(cmd[1] + "/" + entries[0].Name())
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "fake sbx: can't run %q\n", cmd)
	return 127
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

			// Where the sandbox keeps AgentPath on a filesystem of its own.
			if err := os.MkdirAll(filepath.Join(agent, lostFound), 0o755); err != nil {
				t.Fatal(err)
			}

			store := filepath.Join(t.TempDir(), "store")
			saved, err := Backup("any", store)
			if err != nil || !saved {
				t.Fatalf("Backup = %v, %v; want true, nil", saved, err)
			}
			if _, err := os.Stat(filepath.Join(store, "-proj", "memory", "MEMORY.md")); err != nil {
				t.Errorf("memory not backed up: %v", err)
			}
			if _, err := os.Stat(filepath.Join(store, lostFound)); !os.IsNotExist(err) {
				t.Errorf("lost+found was backed up with the memories")
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

func TestWindowsMountCandidatesCoverTheUsualSpellings(t *testing.T) {
	got := windowsMountCandidates(`C:\Users\me\.vibe\memories\proj`)
	want := []string{
		"C:/Users/me/.vibe/memories/proj",
		"/c/Users/me/.vibe/memories/proj",
		"/mnt/c/Users/me/.vibe/memories/proj",
		"/run/desktop/mnt/host/c/Users/me/.vibe/memories/proj",
		"/host_mnt/c/Users/me/.vibe/memories/proj",
		"/Users/me/.vibe/memories/proj",
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// On Windows the store's path inside the sandbox is found, not assumed:
// only a candidate where the host's marker file shows up is accepted.
func TestSandboxPathOnWindowsFindsTheMountByItsMarker(t *testing.T) {
	fakeSbx(t, false)
	old := hostOS
	hostOS = "windows"
	t.Cleanup(func() { hostOS = old })

	// The fake sbx runs commands on this machine, where the store's own
	// path is where its marker shows up.
	store := t.TempDir()
	got, err := sandboxPath(context.Background(), "any", store)
	if err != nil || filepath.Clean(got) != filepath.Clean(store) {
		t.Errorf("sandboxPath(%q) = %q, %v; want the path itself", store, got, err)
	}
	entries, _ := os.ReadDir(store)
	if len(entries) != 0 {
		t.Errorf("marker left behind in the store: %v", entries)
	}

	// A store the sandbox can't see is an error, not a guess.
	t.Setenv("PATH", t.TempDir())
	if got, err := sandboxPath(context.Background(), "any", store); err == nil {
		t.Errorf("sandboxPath with nothing mounted = %q, want an error", got)
	}
}
