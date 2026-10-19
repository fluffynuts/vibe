package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/running"
	"vibe/internal/state"
)

// headlessStandIn stands in for starting the background vibe, and says what
// was true when it was started.
type headlessStandIn struct {
	started    int
	sbxCalls   string
	claimsThen int
	args       cliargs.Args
}

func standInForHeadless(t *testing.T, calls func() string) *headlessStandIn {
	t.Helper()
	f := &headlessStandIn{}
	old := startHeadlessInBackground
	startHeadlessInBackground = func(lay layout.Layout, args cliargs.Args, name, target string) error {
		f.started++
		f.args = args
		f.sbxCalls = calls()
		f.claimsThen = len(running.Claims(lay.Home, target))
		return nil
	}
	t.Cleanup(func() { startHeadlessInBackground = old })
	return f
}

func rebuildableSandbox(t *testing.T) (layout.Layout, state.Instance, func() string) {
	t.Helper()
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "shell"
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	path := loggingSbx(t, "box running\\n")
	return lay, inst, func() string { data, _ := os.ReadFile(path); return string(data) }
}

// The same as 'vibe -r --no-start && vibe --headless': the rebuild first, with
// no terminal session after it, then the headless vibe once the folder is let go.
func TestARebuildWithHeadlessRebuildsThenStartsTheBackgroundVibe(t *testing.T) {
	lay, inst, calls := rebuildableSandbox(t)
	headless := standInForHeadless(t, calls)
	args := cliargs.Args{ReInit: true, Headless: true, Force: true}
	if err := rebuildThenHeadless(lay, args, "box", inst.Target); err != nil {
		t.Fatal(err)
	}
	if headless.started != 1 {
		t.Fatalf("the background vibe was started %d times, want 1", headless.started)
	}
	if !strings.Contains(headless.sbxCalls, "create ") {
		t.Errorf("the sandbox wasn't rebuilt before the background vibe started:\\n%s", headless.sbxCalls)
	}
	for _, line := range strings.Split(calls(), "\\n") {
		if strings.HasPrefix(line, "run ") {
			t.Errorf("a terminal session was attached: %q", line)
		}
	}
	if headless.claimsThen != 0 {
		t.Errorf("the folder was still claimed (%d) when the background vibe started: it takes the folder itself", headless.claimsThen)
	}
	if !headless.args.Headless {
		t.Error("the background vibe wasn't asked for as headless")
	}
	if session != nil {
		t.Error("this vibe's claim on the folder was kept")
	}
}

func TestARebuildThatFailsStartsNoBackgroundVibeAndLetsGoOfTheFolder(t *testing.T) {
	lay := updateLayout(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p", Target: gone, Agent: "shell"}); err != nil {
		t.Fatal(err)
	}
	calls := loggingSbx(t, "box running\\n")
	headless := standInForHeadless(t, func() string { d, _ := os.ReadFile(calls); return string(d) })
	err := rebuildThenHeadless(lay, cliargs.Args{ReInit: true, Headless: true, Force: true}, "box", gone)
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("err = %v, want the rebuild's own reason", err)
	}
	if headless.started != 0 {
		t.Error("the background vibe was started on a rebuild that failed")
	}
	if n := len(running.Claims(lay.Home, gone)); n != 0 {
		t.Errorf("the folder is still claimed (%d) after the failure", n)
	}
}

// The rebuild is the one without --headless: same functions, so same questions
// and the same ending.
func TestTheRebuildsAreTheSameWithOrWithoutHeadless(t *testing.T) {
	for args, want := range map[string]func(layout.Layout, cliargs.Args, string, string) error{
		"re-init": doReInit, "re-create": doReCreate, "re-compose": doReCompose,
	} {
		var a cliargs.Args
		switch args {
		case "re-init":
			a.ReInit = true
		case "re-create":
			a.ReCreate = true
		default:
			a.ReCompose = true
		}
		got := rebuildAction(a)
		if got == nil || reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Errorf("--%s isn't what it was", args)
		}
	}
	if rebuildAction(cliargs.Args{Headless: true}) != nil {
		t.Error("--headless alone is a rebuild")
	}
}

// 'type vibe to get started' is for a rebuild that stops short of starting; a
// rebuild going on to headless has started it.
func TestARebuildGoingOnToHeadlessDoesntSayToTypeVibe(t *testing.T) {
	lay := updateLayout(t)
	said := strings.Join(notesOf(func() { finish(lay, cliargs.Args{NoStart: true, Headless: true}, "box") }), "\\n")
	if strings.Contains(said, "type vibe") {
		t.Errorf("said %q", said)
	}
	said = strings.Join(notesOf(func() { finish(lay, cliargs.Args{NoStart: true}, "box") }), "\\n")
	if !strings.Contains(said, "type vibe to get started") {
		t.Errorf("a plain --no-start no longer says what to do: %q", said)
	}
}

// Through run(): the arguments are accepted together, and do what they say.
func TestRunAcceptsARebuildWithHeadlessAndDoesBoth(t *testing.T) {
	lay, inst, calls := rebuildableSandbox(t)
	t.Setenv("VIBE_HOME", lay.Home)
	headless := standInForHeadless(t, calls)
	if err := run([]string{"-r", "--headless", "-f", "--name", "box", inst.Target}); err != nil {
		t.Fatalf("run = %v", err)
	}
	if headless.started != 1 || !strings.Contains(headless.sbxCalls, "create ") {
		t.Errorf("started %d times; sbx had been asked:\n%s", headless.started, headless.sbxCalls)
	}
}

func TestRunStillRefusesHeadlessWithWhatItCantFollow(t *testing.T) {
	for _, argv := range [][]string{{"--delete", "--headless"}, {"-r", "-C", "--headless"}, {"--info", "--headless"}} {
		err := run(append(argv, "-f", t.TempDir()))
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") || !strings.Contains(err.Error(), "--re-init, --re-create and --re-compose") {
			t.Errorf("run(%v) = %v, want a refusal saying what --headless goes with", argv, err)
		}
	}
}
