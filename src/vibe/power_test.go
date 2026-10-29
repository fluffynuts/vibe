package main

import (
	"fmt"
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
		if a != companion.PowerRestart && strings.Contains(got.Why, "restart") {
			t.Errorf("%s is told to restart: %q", a, got.Why)
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
	// Written whole and moved into place, so that the file being there means it is all there. And it lingers
	// after, as a slow machine's exiting process does: an action isn't over when it has said what it was asked.
	fakeVibe(t, `printf '%s\n' "$@" > `+args+`.tmp && mv `+args+`.tmp `+args+`; sleep 0.3`)
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
		data, _ := os.ReadFile(args)
		if got := strings.Fields(string(data)); !slices.Equal(got, want) {
			t.Errorf("%s ran vibe %v, want %v", action, got, want)
		}
		// The next action waits for this one to end, as the desk makes it.
		waitFor(t, func() bool { return !d.actionRunning("box") })
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

// vibeAsked counts how often the desk asked which vibe to run. It asks as it is about to run one and for no other
// reason, so a desk that refused hasn't: said by the count, which needs no waiting to see that nothing started.
func vibeAsked(t *testing.T) *int {
	t.Helper()
	asked := 0
	inner := powerExe
	t.Cleanup(func() { powerExe = inner })
	powerExe = func() (string, error) { asked++; return inner() }
	return &asked
}

func TestAnActionThatCantBeDoneIsNotRunAtAll(t *testing.T) {
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, false, true)
	fakeVibe(t, "true")
	asked := vibeAsked(t)
	if err := d.Do("box", companion.PowerStart); err == nil {
		t.Error("a vibe is running, and one was started")
	}
	if *asked != 0 {
		t.Errorf("vibe was asked for %d times to be run", *asked)
	}
	// The same count sees one that is run, so that a count of none says something.
	d.claims = func(string, string) []running.Claim { return nil }
	if err := d.Do("box", companion.PowerStart); err != nil {
		t.Fatal(err)
	}
	if *asked != 1 {
		t.Errorf("a start that was done asked for vibe %d times, not once", *asked)
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

func TestADeskNeverTakesANameForAPlaceAndRunsNothingForOne(t *testing.T) {
	d, lay, _ := powerStandIns(t, nil, false, true)
	// A record planted where a name with .. would find it: one for a folder of someone else's.
	planted := filepath.Join(filepath.Dir(lay.Home), "evil")
	if err := os.WriteFile(planted+".yaml", []byte("name: evil\ntarget: /somewhere/else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planted+".power.log", []byte("a log that isn't vibe's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeVibe(t, "true")
	asked := vibeAsked(t)
	rel, _ := filepath.Rel(filepath.Join(lay.Home, "logs"), planted)
	for _, name := range []string{"../evil", "../../evil", rel, "a/b", ".."} {
		if got := d.Check(name); got.Error == "" || len(got.Actions) != 0 {
			t.Errorf("Check(%q) = %+v", name, got)
		}
		if err := d.Do(name, companion.PowerStop); err == nil {
			t.Errorf("Do(%q) was done", name)
		}
		if text, _, _ := d.Log(name, 0); text != "" {
			t.Errorf("Log(%q) read %q", name, text)
		}
	}
	if *asked != 0 {
		t.Errorf("vibe was run %d times for a name that isn't a sandbox's", *asked)
	}
}

func TestWhileAnActionIsUnderWayNoOtherIsStartedAndEachActionSaysWhy(t *testing.T) {
	d, _, _ := powerStandIns(t, nil, false, true)
	fakeVibe(t, "sleep 0.5")
	if err := d.Do("box", companion.PowerStop); err != nil {
		t.Fatal(err)
	}
	check := d.Check("box")
	for _, a := range []string{companion.PowerStop, companion.PowerRestart, companion.PowerStart} {
		if got := check.Actions[a]; got.OK || !strings.Contains(got.Why, "already being done") {
			t.Errorf("%s: %+v", a, got)
		}
	}
	if err := d.Do("box", companion.PowerStop); err == nil {
		t.Error("a second stop was started alongside the first")
	}
	waitFor(t, func() bool { return d.Check("box").Actions[companion.PowerStop].OK })
}

func TestAnActionWhosePIDHasBeenReusedByAnotherProcessIsNotTakenForRunning(t *testing.T) {
	d, lay, _ := powerStandIns(t, nil, false, true)
	now := time.Now()
	d.now = func() time.Time { return now }
	// A live process (this one) whose number was noted long ago: the action it was has long since ended.
	note := fmt.Sprintf("%d %d", os.Getpid(), now.Add(-2*powerActionLongest).Unix())
	if err := os.MkdirAll(filepath.Join(lay.Home, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(powerPidPath(lay.Home, "box"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(powerLogPath(lay.Home, "box"), []byte("=== started ===\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, running := d.Log("box", 0); running {
		t.Error("a PID noted long ago, and now another process's, is taken for the action")
	}
	if !d.Check("box").Actions[companion.PowerStop].OK {
		t.Error("stop is refused for an action that ended long ago")
	}
	fresh := fmt.Sprintf("%d %d", os.Getpid(), now.Unix())
	os.WriteFile(powerPidPath(lay.Home, "box"), []byte(fresh), 0o644)
	if _, _, running := d.Log("box", 0); !running {
		t.Error("a live action just started isn't taken for running")
	}
}

func TestADeskSeesTheVibeItIsRunningInAsOneOfThoseHoldingTheFolder(t *testing.T) {
	// The page's own sandbox: the vibe serving the page is the one to stop or restart, and Claims leaves it out as it is not "other".
	d, lay, target := powerStandIns(t, nil, true, true)
	d.claims = nil
	s, err := running.Register(lay.Home, target)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	if err := s.MarkHeadless(target); err != nil {
		t.Fatal(err)
	}
	check := d.Check("box")
	restart := check.Actions[companion.PowerRestart]
	if !restart.OK {
		t.Errorf("a vibe is running here, headless, and restart is refused: %+v", restart)
	}
	if len(restart.Warns) != 1 || !strings.Contains(restart.Warns[0], "part-way through a turn") {
		t.Errorf("restart doesn't warn of the turn its own agent is part-way through: %+v", restart)
	}
	if stop := check.Actions[companion.PowerStop]; len(stop.Warns) != 1 {
		t.Errorf("stop: %+v", stop)
	}
	if start := check.Actions[companion.PowerStart]; start.OK {
		t.Errorf("a vibe is running here, and start is offered: %+v", start)
	}
}

func TestTheActionDoesNotInheritBeingTheBackgroundCopyAHeadlessVibeIs(t *testing.T) {
	// A headless vibe is started with this set, which tells vibe it is that copy: to skip --restart's ending of the vibe
	// that has the folder and the choice of mode. An action it runs is the command from a terminal, not that copy.
	t.Setenv(headlessChildEnv, "1")
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, false, true)
	seen := filepath.Join(t.TempDir(), "seen")
	fakeVibe(t, `echo "[${`+headlessChildEnv+`-unset}]" > `+seen+`.tmp && mv `+seen+`.tmp `+seen)
	if err := d.Do("box", companion.PowerRestart); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, err := os.Stat(seen); return err == nil })
	if data, _ := os.ReadFile(seen); strings.TrimSpace(string(data)) != "[unset]" {
		t.Errorf("the action ran with %s = %s", headlessChildEnv, data)
	}
	if os.Getenv(headlessChildEnv) != "1" {
		t.Error("this process's own environment was changed")
	}
}

func TestOnlyARestartOfThePagesOwnSandboxIsToldNotToOpenTheBrowserAgain(t *testing.T) {
	// The page that asked is open: a new tab for the new vibe would make the old one reload under its progress box
	// and leave a tab saying it is already open. Another sandbox's page, or a start, is not open to be reloaded.
	// A vibe that was itself started that way has it in its environment for good, and what it runs is not for its page.
	t.Setenv(companionNoOpenEnv, "1")
	d, _, _ := powerStandIns(t, []running.Claim{{PID: 7, Headless: true}}, false, true)
	d.own = "box"
	seen := filepath.Join(t.TempDir(), "seen")
	fakeVibe(t, `echo "[${`+companionNoOpenEnv+`-unset}]" > `+seen+`.tmp && mv `+seen+`.tmp `+seen)
	read := func(action string) string {
		os.Remove(seen)
		if err := d.Do("box", action); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { _, err := os.Stat(seen); return err == nil })
		waitFor(t, func() bool { return !d.actionRunning("box") })
		data, _ := os.ReadFile(seen)
		return strings.TrimSpace(string(data))
	}
	if got := read(companion.PowerRestart); got != "[1]" {
		t.Errorf("a restart of the page's own sandbox: %s", got)
	}
	d.own = "somebody-else"
	if got := read(companion.PowerRestart); got != "[unset]" {
		t.Errorf("a restart of another sandbox: %s", got)
	}
	d.own = "box"
	d.claims = func(string, string) []running.Claim { return nil }
	if got := read(companion.PowerStart); got != "[unset]" {
		t.Errorf("a start: %s", got)
	}
}
