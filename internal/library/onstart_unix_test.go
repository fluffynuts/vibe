//go:build !windows

package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The generated on-start is run as it is, with start scripts of the test's own, and its log, lock and scripts'
// folder moved by the overrides it has for being tried. Processes the tests start are ended by the PIDs they were
// given, never by name.

type onStartEnv struct {
	dir, bin, log, lock, out string
	path                     string // PATH for the script: only the tools it needs, so a real sudo can't take it over
	extra                    []string
}

func newOnStartEnv(t *testing.T, scripts map[string]string, tools ...string) *onStartEnv {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the script is for a Linux sandbox")
	}
	e := &onStartEnv{dir: t.TempDir()}
	e.bin = filepath.Join(e.dir, "bin")
	e.log, e.lock, e.out = filepath.Join(e.dir, "log"), filepath.Join(e.dir, "lock"), filepath.Join(e.dir, "out")
	tooldir := filepath.Join(e.dir, "tools")
	for _, d := range []string{e.bin, tooldir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range append([]string{"date", "id", "sleep", "cat", "setsid", "nohup"}, tools...) {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on this machine", tool)
		}
		os.Symlink(real, filepath.Join(tooldir, tool))
	}
	e.path = tooldir
	var names []string
	for name, body := range scripts {
		names = append(names, name)
		if err := os.WriteFile(filepath.Join(e.bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Alphabetical, so the order is the same each time.
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	profile := filepath.Join(e.dir, "profile")
	if err := writeOnStart(profile, names); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(profile, "agent-files", ".local", "bin", "on-start")
	// Deployed it is executable; flock runs it again, so here it has to be.
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	e.extra = []string{script}
	return e
}

func (e *onStartEnv) command(env ...string) *exec.Cmd {
	cmd := exec.Command("/bin/sh", e.extra[0])
	cmd.Env = append([]string{"PATH=" + e.path, "VIBE_ON_START_LOG=" + e.log, "VIBE_ON_START_BIN=" + e.bin,
		"VIBE_ON_START_LOCK=" + e.lock, "OUT=" + e.out}, env...)
	return cmd
}

func (e *onStartEnv) logText() string {
	got, _ := os.ReadFile(e.log)
	return string(got)
}

func (e *onStartEnv) outLines() []string {
	got, _ := os.ReadFile(e.out)
	return strings.Fields(string(got))
}

func TestOnStartRunsEachScriptInOrderAndSaysHowLongEachTook(t *testing.T) {
	e := newOnStartEnv(t, map[string]string{"start-a": "echo a >>\"$OUT\"\n", "start-b": "echo b >>\"$OUT\"\n"})
	if out, err := e.command().CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := strings.Join(e.outLines(), ","); got != "a,b" {
		t.Errorf("the scripts ran as %q, want a,b", got)
	}
	log := e.logText()
	for _, want := range []string{"--- start-a", "start-a took ", "--- start-b", "start-b took ", "=== done at ", "=== took "} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
}

func TestAScriptThatFailsIsSaidAndTheRestStillRun(t *testing.T) {
	e := newOnStartEnv(t, map[string]string{"start-a": "exit 3\n", "start-b": "echo b >>\"$OUT\"\n"})
	if out, err := e.command().CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(e.logText(), "WARN: start-a exited 3") || strings.Join(e.outLines(), ",") != "b" {
		t.Errorf("a failing script stopped the rest, or wasn't said:\n%s\nout: %v", e.logText(), e.outLines())
	}
}

// Boot and vibe's nudge can overlap: the second must wait for the first, not run its scripts among the first's.
func TestTwoOnStartsAtOnceTakeTurns(t *testing.T) {
	e := newOnStartEnv(t, map[string]string{"start-slow": "echo start >>\"$OUT\"\nsleep 1\necho end >>\"$OUT\"\n"}, "flock")
	first, second := e.command(), e.command()
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Errorf("the first: %v", err)
	}
	if err := second.Wait(); err != nil {
		t.Errorf("the second: %v", err)
	}
	if got := strings.Join(e.outLines(), ","); got != "start,end,start,end" {
		t.Errorf("the runs overlapped: %q, want start,end,start,end", got)
	}
	if !strings.Contains(e.logText(), "s for another on-start to finish") {
		t.Errorf("the one that waited didn't say so:\n%s", e.logText())
	}
}

// The daemons a start script launches must not hold the lock after the run is over, or every later run would wait
// for them: that is what flock -o is for.
func TestADaemonAStartScriptLeavesRunningDoesNotKeepTheLock(t *testing.T) {
	e := newOnStartEnv(t, map[string]string{"start-daemon": "setsid nohup sleep 60 >/dev/null 2>&1 &\necho $! >\"$OUT\"\n"}, "flock")
	if out, err := e.command().CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if pids := e.outLines(); len(pids) == 1 {
		if pid, err := strconv.Atoi(pids[0]); err == nil {
			t.Cleanup(func() {
				if p, err := os.FindProcess(pid); err == nil {
					p.Kill()
				}
			})
		}
	}
	probe := exec.Command("flock", "-n", e.lock, "true")
	if err := probe.Run(); err != nil {
		t.Errorf("the lock is still held after the run ended: a daemon it started has it: %v", err)
	}
}

func holdLock(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
}

func TestAnOnStartThatCannotGetTheLockInTimeGivesUpAndRunsNothing(t *testing.T) {
	e := newOnStartEnv(t, map[string]string{"start-a": "echo a >>\"$OUT\"\n"}, "flock")
	holdLock(t, e.lock)
	began := time.Now()
	if out, err := e.command("VIBE_ON_START_LOCK_WAIT=1").CombinedOutput(); err != nil {
		t.Fatalf("giving up must not be a failure: %v\n%s", err, out)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("it waited %v for a wait of 1 second", took)
	}
	if !strings.Contains(e.logText(), "not starting a second") || len(e.outLines()) != 0 {
		t.Errorf("it ran the scripts anyway, or didn't say it gave up:\n%s\nout: %v", e.logText(), e.outLines())
	}
}

// A lock that can't be had is no reason to leave the services down.
func TestAnOnStartWithALockItCannotOpenRunsWithoutIt(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can open any file")
	}
	e := newOnStartEnv(t, map[string]string{"start-a": "echo a >>\"$OUT\"\n"}, "flock")
	if err := os.WriteFile(e.lock, nil, 0o000); err != nil {
		t.Fatal(err)
	}
	if out, err := e.command().CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(e.logText(), "couldn't open its lock") || strings.Join(e.outLines(), ",") != "a" {
		t.Errorf("the scripts didn't run without the lock, or it wasn't said:\n%s\nout: %v", e.logText(), e.outLines())
	}
}

func TestAnOnStartRunsWithoutAnyLockWhenTheImageHasNoFlock(t *testing.T) {
	e := newOnStartEnv(t, map[string]string{"start-a": "echo a >>\"$OUT\"\n"}) // no flock on its PATH
	if out, err := e.command().CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Join(e.outLines(), ",") != "a" {
		t.Errorf("the scripts didn't run without flock: %v\n%s", e.outLines(), e.logText())
	}
}

// sbx validates every brace-form substitution in a kit's file content against its own placeholder list.
func TestTheGeneratedOnStartHasNoBracedExpansions(t *testing.T) {
	e := newOnStartEnv(t, map[string]string{"start-a": "true\n"})
	got, err := os.ReadFile(e.extra[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "${") {
		t.Errorf("on-start has a braced expansion:\n%s", got)
	}
}
