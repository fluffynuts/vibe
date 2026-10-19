package sbxrun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/sbxtest"
)

// TestStartGivesUpOnASandboxItCannotBoot checks Start's failure path: with no
// such sandbox (or no sbx at all) it reports false promptly rather than
// waiting out startTimeout on something that is never going to answer.
func TestStartGivesUpOnASandboxItCannotBoot(t *testing.T) {
	done := make(chan bool, 1)
	go func() { done <- Start(context.Background(), "vibe-test-no-such-sandbox-3f9c1") }()
	select {
	case started := <-done:
		if started {
			t.Error("Start reported a nonexistent sandbox as running")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Start hung on a sandbox that cannot boot")
	}
}

func TestParseAgents(t *testing.T) {
	help := "Omit the path to mount the current directory.\n\n" +
		"Available agents: claude, codex, copilot, cursor, devin, docker-agent, droid, gemini, kiro, opencode, shell\n\n" +
		"With --cloud:\n"
	want := []string{"claude", "codex", "copilot", "cursor", "devin", "docker-agent", "droid", "gemini", "kiro", "opencode", "shell"}
	if got := ParseAgents(help); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAgents = %v, want %v", got, want)
	}
	if got := ParseAgents("no agents here\n"); got != nil {
		t.Errorf("ParseAgents of help without the line = %v", got)
	}
}

func TestStreamHandsOverEachLineAndFeedsStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	var got []string
	cmd := exec.Command("sh", "-c", "read first; echo \"one $first\"; echo two; printf three")
	err := stream(cmd, "typed\n", nil, func(l []byte) { got = append(got, string(l)) })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"one typed", "two", "three"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestStreamReportsWhatAFailedCommandSaid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	var got []string
	cmd := exec.Command("sh", "-c", "echo before; echo oops >&2; exit 3")
	err := stream(cmd, "", nil, func(l []byte) { got = append(got, string(l)) })
	if err == nil || !strings.Contains(err.Error(), "oops") || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("err = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"before"}) {
		t.Errorf("lines = %q", got)
	}
}

func TestStreamDropsWhatIsPastTheLimitButKeepsGoing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	var lens []int
	cmd := exec.Command("sh", "-c", "head -c 1100000 /dev/zero | tr '\\0' a; echo; echo next")
	if err := stream(cmd, "", nil, func(l []byte) { lens = append(lens, len(l)) }); err != nil {
		t.Fatal(err)
	}
	if len(lens) != 2 || lens[0] > maxStreamLine || lens[1] != 4 {
		t.Errorf("line lengths = %v", lens)
	}
}

func TestStreamEndsWhenItsContextIsCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "echo started; sleep 30")
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		done <- stream(cmd, "", nil, func([]byte) { close(started) })
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a killed command reported success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling did not end the stream")
	}
}

func TestTailKeepsTheEnd(t *testing.T) {
	var tl tail
	tl.Write([]byte(strings.Repeat("a", tailSize)))
	tl.Write([]byte("end"))
	if got := tl.String(); len(got) != tailSize || !strings.HasSuffix(got, "aend") {
		t.Errorf("tail = %d bytes ending %q", len(got), got[len(got)-4:])
	}
}

func TestStreamSaysWhenTheCommandHasTakenAllItsInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	// delivered and onLine are called on different goroutines.
	var mu sync.Mutex
	var order []string
	say := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	// The command says nothing until its input ends, which the pipe says
	// only after delivered has been called: so what it says comes after.
	cmd := exec.Command("sh", "-c", `read -r typed; cat >/dev/null; echo "got $typed"`)
	err := stream(cmd, "typed\n", func() { say("delivered") }, func(l []byte) { say(string(l)) })
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"delivered", "got typed"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %q, want %q", order, want)
	}
}

// fakeSbx puts an sbx on PATH that, asked for 'ls --json', prints stdout
// and stderr and exits with code; asked for anything else, it fails.
func fakeSbx(t *testing.T, stdout, stderr string, code int) {
	t.Helper()
	dir := sbxtest.OnPath(t, fmt.Sprintf(
		"[ \"$*\" = \"ls --json\" ] || { echo \"unexpected: sbx $*\" >&2; exit 2; }\n"+
			"cd \"$(dirname \"$0\")\"\ncat stdout\ncat stderr >&2\nexit %d\n", code))
	for name, content := range map[string]string{"stdout": stdout, "stderr": stderr} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// realListing is 'sbx ls --json' from sbx v0.48, cut down to two sandboxes.
const realListing = `[
  {
    "name": "convert-type-imports",
    "id": "1123044f-41e7-4ffb-82b3-57af4ff55f42",
    "agent": "claude",
    "status": "stopped",
    "last_used_at": "2026-10-14T11:23:09.177234797Z",
    "workspaces": [
      "/home/davydm/code/opensource/convert-type-imports",
      "/home/davydm/.vibe/memories/convert-type-imports",
      "/home/davydm/.vibe/clipboard/convert-type-imports"
    ],
    "memory": "12g",
    "memory_mib": 12288,
    "created_at": "2026-10-14T11:22:44Z"
  },
  {
    "name": "vibe",
    "id": "610b4499-468b-4dff-abeb-0ffe9166ba9e",
    "agent": "claude",
    "status": "running",
    "last_used_at": "2026-10-17T08:35:01.140488627Z",
    "ports": [
      {
        "host_ip": "127.0.0.1",
        "host_port": 5405,
        "sandbox_port": 5390,
        "protocol": "tcp4"
      }
    ],
    "workspaces": [
      "/home/davydm/code/opensource/vibe",
      "/home/davydm/.vibe/memories/vibe"
    ],
    "memory": "12g",
    "memory_mib": 12288,
    "created_at": "2026-10-16T19:52:45Z"
  }
]
`

func TestListReadsSbxsJSON(t *testing.T) {
	fakeSbx(t, realListing, "", 0)
	got, err := List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Status{
		{Name: "convert-type-imports", State: "stopped"},
		{Name: "vibe", Running: true, State: "running"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %+v\nwant %+v", got, want)
	}
}

func TestListKeepsAStateItDoesNotKnow(t *testing.T) {
	fakeSbx(t, `[{"name": "odd-one", "status": "Creating"}]`, "", 0)
	got, err := List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []Status{{Name: "odd-one", State: "creating"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

func TestListIsNothingWhenSbxHasNoSandboxes(t *testing.T) {
	for _, out := range []string{"[]\n", "null\n", ""} {
		fakeSbx(t, out, "", 0)
		got, err := List(context.Background())
		if err != nil || len(got) != 0 {
			t.Errorf("List of %q = %+v, %v; want nothing", out, got, err)
		}
	}
}

func TestListIgnoresWhatSbxSaysOnStderr(t *testing.T) {
	fakeSbx(t, realListing, "A new version of sbx is available\n", 0)
	got, err := List(context.Background())
	if err != nil || len(got) != 2 {
		t.Errorf("List = %+v, %v; want the two sandboxes", got, err)
	}
}

func TestListSaysToUpgradeAnSbxWithoutJSON(t *testing.T) {
	fakeSbx(t, "", "Error: unknown flag: --json\n", 1)
	_, err := List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --json") || !strings.Contains(err.Error(), "vibe --install-sbx") {
		t.Errorf("List error = %v, want sbx's reason and how to upgrade it", err)
	}
}

func TestListSaysWhatItCouldNotRead(t *testing.T) {
	fakeSbx(t, "SANDBOX  AGENT  STATUS\n", "", 0)
	_, err := List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sbx ls --json") {
		t.Errorf("List error = %v, want it to name the command it couldn't read", err)
	}
}

func TestExistsLooksInSbxsList(t *testing.T) {
	fakeSbx(t, realListing, "", 0)
	for name, want := range map[string]bool{"vibe": true, "convert-type-imports": true, "vib": false, "nope": false} {
		got, err := Exists(name)
		if err != nil || got != want {
			t.Errorf("Exists(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
}

func TestExistsSaysWhenSbxCannotTell(t *testing.T) {
	fakeSbx(t, "", "daemon not running\n", 1)
	if _, err := Exists("vibe"); err == nil || !strings.Contains(err.Error(), "daemon not running") {
		t.Errorf("Exists error = %v, want sbx's reason", err)
	}
}
