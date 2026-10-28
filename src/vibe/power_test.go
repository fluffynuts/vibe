package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/running"
	"vibe/internal/state"
)

// powerStandIns makes the desk's view of the folder's vibes claims, and what the agent is doing answering.
func powerStandIns(t *testing.T, claims []running.Claim, answering, known bool) (*powerDesk, layout.Layout, string) {
	t.Helper()
	lay := updateLayout(t)
	rec := sandboxRecord(t, lay, "box", "p")
	old := agentAnswering
	t.Cleanup(func() { agentAnswering = old })
	agentAnswering = func(string, string) (bool, bool) { return answering, known }
	return &powerDesk{lay: lay, claims: func(string, string) []running.Claim { return claims }, outOfDate: func(state.Instance) string { return "" }}, lay, rec.inst.Target
}

func TestAnIdleHeadlessVibeCanBeStoppedOrRestartedUnasked(t *testing.T) {
	d, _, target := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, false, true)
	check := d.Check("box")
	if check.Error != "" || check.Folder == "" || !strings.Contains(check.Folder, filepath.Base(target)) {
		t.Fatalf("%+v", check)
	}
	for _, a := range []string{companion.PowerStop, companion.PowerRestart} {
		if got := check.Actions[a]; !got.OK || len(got.Warns) != 0 {
			t.Errorf("%s: %+v", a, got)
		}
	}
	if got := check.Actions[companion.PowerStart]; got.OK || got.Why == "" {
		t.Errorf("a vibe is running, so it can't be started: %+v", got)
	}
}

func TestAnAgentPartWayThroughATurnIsWhatStoppingWouldCutOff(t *testing.T) {
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, true, true)
	for _, a := range []string{companion.PowerStop, companion.PowerRestart} {
		got := d.Check("box").Actions[a]
		if !got.OK || len(got.Warns) != 1 || !strings.Contains(got.Warns[0], "part-way through a turn") {
			t.Errorf("%s: %+v", a, got)
		}
	}
}

func TestAVibeThatCouldntBeAskedIsSaidSoRatherThanTakenForIdle(t *testing.T) {
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, false, false)
	if got := d.Check("box").Actions[companion.PowerStop]; len(got.Warns) != 1 || !strings.Contains(got.Warns[0], "couldn't be asked") {
		t.Errorf("%+v", got)
	}
}

func TestATerminalSessionCanBeStoppedWithAWarningButNotRestartedFromThePage(t *testing.T) {
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7}}, false, true)
	check := d.Check("box")
	if got := check.Actions[companion.PowerStop]; !got.OK || len(got.Warns) != 1 || !strings.Contains(got.Warns[0], "terminal session") {
		t.Errorf("stop: %+v", got)
	}
	if got := check.Actions[companion.PowerRestart]; got.OK || !strings.Contains(got.Why, "terminal") {
		t.Errorf("restart: %+v", got)
	}
}

func TestStartingOrRestartingAnOutOfDateSandboxFromThePageSaysItWontBeUpdated(t *testing.T) {
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, false, true)
	d.outOfDate = func(state.Instance) string { return "what it's built from has changed" }
	check := d.Check("box")
	if got := check.Actions[companion.PowerStop]; len(got.Warns) != 0 {
		t.Errorf("stop: %+v", got)
	}
	got := check.Actions[companion.PowerRestart]
	if !got.OK || len(got.Warns) != 1 || !strings.Contains(got.Warns[0], "out of date") || !strings.Contains(got.Warns[0], "terminal") {
		t.Errorf("restart: %+v", got)
	}
	d.claims = func(string, string) []running.Claim { return nil }
	if got := d.Check("box").Actions[companion.PowerStart]; !got.OK || len(got.Warns) != 1 || !strings.Contains(got.Warns[0], "out of date") {
		t.Errorf("start: %+v", got)
	}
}

func TestAFolderHeldForARebuildCanNeitherBeStoppedNorRestartedAndSaysWhatIsGoingOn(t *testing.T) {
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}, {PID: 8, Work: "rebuilding sandbox 'box'"}}, false, true)
	for _, a := range []string{companion.PowerStop, companion.PowerRestart, companion.PowerStart} {
		got := d.Check("box").Actions[a]
		if got.OK || !strings.Contains(got.Why, "rebuilding sandbox 'box'") {
			t.Errorf("%s: %+v", a, got)
		}
	}
}

func TestWithNoVibeASandboxCanBeStartedAndNotRestarted(t *testing.T) {
	d, _, _ := powerStandIns(t, nil, false, true)
	check := d.Check("box")
	if got := check.Actions[companion.PowerStart]; !got.OK {
		t.Errorf("start: %+v", got)
	}
	if got := check.Actions[companion.PowerRestart]; got.OK || got.Why == "" {
		t.Errorf("restart: %+v", got)
	}
}

func TestASandboxWhoseFolderIsGoneCantBeStartedAndSaysSo(t *testing.T) {
	d, _, target := powerStandIns(t, nil, false, true)
	os.RemoveAll(target)
	if got := d.Check("box").Actions[companion.PowerStart]; got.OK || !strings.Contains(got.Why, "folder") {
		t.Errorf("%+v", got)
	}
}

func TestASandboxVibeHasNoRecordOfIsNotKnown(t *testing.T) {
	d, _, _ := powerStandIns(t, nil, false, true)
	if got := d.Check("nobody"); got.Error == "" {
		t.Errorf("%+v", got)
	}
}

// fakeVibe is a stand-in for the vibe the desk runs apart from this one: a script that says what it was
// given and goes on for a moment.
func fakeVibe(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	path := filepath.Join(t.TempDir(), "vibe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := powerExe
	t.Cleanup(func() { powerExe = old })
	powerExe = func() (string, error) { return path, nil }
	return path
}

func TestEachActionIsTheCLICommandItStandsForRunForTheSandboxAndForced(t *testing.T) {
	d, _, target := powerStandIns(t, nil, false, true)
	args := filepath.Join(t.TempDir(), "args")
	fakeVibe(t, `printf '%s\n' "$@" > `+args)
	for action, want := range map[string][]string{
		companion.PowerStop:    {"--stop", "--force", "--name", "box", "--", target},
		companion.PowerRestart: {"--restart", "--force", "--no-auto-update", "--name", "box", "--", target},
		companion.PowerStart:   {"--headless", "--no-auto-update", "--name", "box", "--", target},
	} {
		os.Remove(args)
		d.claims = func(string, string) []running.Claim { return nil }
		if action != companion.PowerStart {
			d.claims = func(string, string) []running.Claim { return []running.Claim{{PID: 7, Headless: true}} }
		}
		if err := d.Do("box", action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		waitFor(t, func() bool { _, err := os.Stat(args); return err == nil })
		time.Sleep(50 * time.Millisecond)
		data, _ := os.ReadFile(args)
		if got := strings.Fields(string(data)); !slices.Equal(got, want) {
			t.Errorf("%s ran vibe %v, want %v", action, got, want)
		}
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("it never happened")
}

func TestWhatTheActionSaysIsKeptInALogThePageReadsFromWhereItGotToWhileItRuns(t *testing.T) {
	d, _, _ := powerStandIns(t, nil, false, true)
	fakeVibe(t, `echo "stopping sandbox"; echo "a problem" >&2; sleep 0.4; echo "stopped"`)
	if err := d.Do("box", companion.PowerStop); err != nil {
		t.Fatal(err)
	}
	var text string
	var next int64
	waitFor(t, func() bool {
		more, n, running := d.Log("box", next)
		text += more
		next = n
		return running && strings.Contains(text, "a problem")
	})
	waitFor(t, func() bool {
		more, n, running := d.Log("box", next)
		text += more
		next = n
		return !running && strings.Contains(text, "stopped")
	})
	for _, want := range []string{"vibe --stop", "stopping sandbox", "a problem", "stopped"} {
		if !strings.Contains(text, want) {
			t.Errorf("the log leaves out %q:\n%s", want, text)
		}
	}
	if more, _, _ := d.Log("box", next); more != "" {
		t.Errorf("from the end, more: %q", more)
	}
}

func TestAnActionThatCantBeDoneIsNotRunAtAll(t *testing.T) {
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, false, true)
	ran := filepath.Join(t.TempDir(), "ran")
	fakeVibe(t, "touch "+ran)
	if err := d.Do("box", companion.PowerStart); err == nil {
		t.Error("a vibe is running, and one was started")
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(ran); err == nil {
		t.Error("vibe was run")
	}
}

func TestALogOfASandboxThatNeverHadAnActionIsEmptyAndNotRunning(t *testing.T) {
	d, _, _ := powerStandIns(t, nil, false, true)
	if text, next, running := d.Log("box", 0); text != "" || next != 0 || running {
		t.Errorf("%q %d %v", text, next, running)
	}
}

func TestEachCommandTheDeskRunsIsOneTheCLIParsesAsWhatItStandsFor(t *testing.T) {
	for action, check := range map[string]func(cliargs.Args) bool{
		companion.PowerStop:    func(a cliargs.Args) bool { return a.Stop && a.Force },
		companion.PowerRestart: func(a cliargs.Args) bool { return a.Restart && a.Force && a.NoAutoUpdate },
		companion.PowerStart:   func(a cliargs.Args) bool { return a.Headless && a.NoAutoUpdate },
	} {
		argv, err := powerCommand(action, "box", "/work/box")
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := cliargs.Parse(argv)
		if err != nil {
			t.Errorf("%s: %v does not parse: %v", action, argv, err)
			continue
		}
		if !check(parsed) || parsed.Name != "box" || parsed.Path != "/work/box" {
			t.Errorf("%s: %v parses as %+v", action, argv, parsed)
		}
	}
}
