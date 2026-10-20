package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	agentmem "vibe/internal/memory"
	"vibe/internal/state"
)

// The page's port is remembered so its address — a bookmark, another
// machine's ssh forwards — stays the same; a rebuild mustn't forget it.
func TestARebuiltSandboxKeepsItsPagesPort(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.CompanionPort = 10007
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	loggingSbx(t, "box running\\n")

	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	after, _, err := state.Load(lay.Home, "box")
	if err != nil {
		t.Fatal(err)
	}
	if after.CompanionPort != 10007 {
		t.Errorf("page port after the rebuild: %d, want 10007", after.CompanionPort)
	}
}

// The Review tab's folder is on the host. Only two folders of it are
// mounted into the sandbox: the inbox, where vibe-review drops the agent's
// comments, and, read-only, the view it reads the review from. The review
// itself is the host's alone.
func TestASandboxIsBuiltWithItsReviewFolderMounted(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	calls := loggingSbx(t, "box running\\n")
	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(calls)
	var create string
	for _, call := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(call, "create ") {
			create = call
		}
	}
	dir := state.ReviewPath(lay.Home, "box")
	inbox, view := filepath.Join(dir, "inbox"), filepath.Join(dir, "view")
	if !strings.Contains(create, " "+inbox+" ") || !strings.Contains(create, " "+view+":ro") || !strings.Contains(create, "-e VIBE_REVIEW_DIR="+dir) || !strings.Contains(create, "-e VIBE_REVIEW_WORKSPACE="+inst.Target) {
		t.Errorf("sbx create wasn't given the inbox, and the view read-only:\n%s", create)
	}
	if strings.Contains(create, " "+dir+" ") || strings.HasSuffix(create, " "+dir) {
		t.Errorf("the whole review folder was mounted:\n%s", create)
	}
	for _, d := range []string{inbox, view} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("%s wasn't made", d)
		}
	}
}

// createCall is the 'sbx create' a logging stand-in sbx saw.
func createCall(t *testing.T, calls string) string {
	t.Helper()
	data, _ := os.ReadFile(calls)
	for _, call := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(call, "create ") {
			return call
		}
	}
	t.Fatalf("sbx create wasn't run:\n%s", data)
	return ""
}

// Every folder of the host's a sandbox is given, and the variable that
// tells the sandbox where it is: the packages in the NuGet folder the profile
// names (read-only, when they're there), the agent's memories, the review's inbox and (read-only)
// view, and the clipboard's history.
func TestASandboxIsGivenEachOfItsFolders(t *testing.T) {
	lay := updateLayout(t)
	nuget := t.TempDir()
	packages := filepath.Join(nuget, "packages")
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "nugetDir: "+nuget+"\n"+
		"publish:\n  - name: clipboard\n    ports:\n      - 5390\n")
	inst := sandboxRecord(t, lay, "box", "p").inst
	calls := loggingSbx(t, "box running\\n")
	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	create := createCall(t, calls)
	reviews := state.ReviewPath(lay.Home, "box")
	memories := agentmem.StoreFor(lay.Home, "", "box")
	clipboard := state.ClipboardPath(lay.Home, "box")
	for _, want := range []string{
		" " + packages + ":ro", "-e SBX_HOST_NUGET_PACKAGES=" + packages,
		" " + memories + " ", "-e VIBE_MEMORY_STORE=" + memories,
		" " + filepath.Join(reviews, "inbox") + " ", " " + filepath.Join(reviews, "view") + ":ro",
		"-e VIBE_REVIEW_DIR=" + reviews,
		" " + clipboard, "-e VIBE_CLIPBOARD_DIR=" + clipboard,
	} {
		if !strings.Contains(create+" ", want) {
			t.Errorf("sbx create lacks %q:\n%s", want, create)
		}
	}
}

// How many CPUs a sandbox gets is the cpus setting, else half the machine's:
// sbx would give it all of them.
func TestASandboxIsGivenHalfTheMachinesCPUsUnlessTheProfileSaysOtherwise(t *testing.T) {
	for setting, want := range map[string]string{"": "--cpus 8 ", "cpus: 3\n": "--cpus 3 ", "cpus: 40\n": "--cpus 15 "} {
		lay := updateLayout(t)
		writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), setting)
		inst := sandboxRecord(t, lay, "box", "p").inst
		calls := loggingSbx(t, "box running\\n")
		if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
			t.Fatal(err)
		}
		if create := createCall(t, calls); !strings.Contains(create+" ", want) {
			t.Errorf("settings %q: sbx create lacks %q:\n%s", setting, want, create)
		}
	}
}

// What a sandbox was created with is kept in its record, so a rebuild keeps it.
func TestACreatedSandboxRecordsItsMemoryAndCPUs(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "memory: 6g\ncpus: 3\n")
	inst := sandboxRecord(t, lay, "box", "p").inst
	loggingSbx(t, "box running\\n")
	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	got, found, err := state.Load(lay.Home, "box")
	if err != nil || !found || got.Memory != "6g" || got.CPUs != 3 {
		t.Errorf("record = %+v (found %v, %v), want 6g and 3 CPUs", got, found, err)
	}
}

func rebuiltWith(t *testing.T, recorded state.Instance, settingsYaml string, args cliargs.Args) (create string, notes []string) {
	t.Helper()
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), settingsYaml)
	inst := sandboxRecord(t, lay, "box", "p").inst
	recorded.Name, recorded.Profile, recorded.Target = "box", "p", inst.Target
	if err := state.Save(lay.Home, recorded); err != nil {
		t.Fatal(err)
	}
	calls := loggingSbx(t, "box running\\n")
	notes = notesOf(func() {
		if err := rebuildSandbox(context.Background(), lay, args, "box", inst.Target, true, false); err != nil {
			t.Fatal(err)
		}
	})
	return createCall(t, calls), notes
}

func TestARebuildKeepsTheMemoryAndCPUsTheSandboxWasCreatedWith(t *testing.T) {
	create, _ := rebuiltWith(t, state.Instance{Memory: "8g", CPUs: 5}, "memory: 12g\ncpus: 10\n", cliargs.Args{Force: true})
	for _, want := range []string{"--cpus 5 ", "--memory 8g "} {
		if !strings.Contains(create+" ", want) {
			t.Errorf("sbx create lacks %q (the settings say 12g and 10, the sandbox was made with 8g and 5):\n%s", want, create)
		}
	}
}

func TestARebuildOfASandboxWithNoRecordedSizesUsesTheSettings(t *testing.T) {
	create, _ := rebuiltWith(t, state.Instance{}, "memory: 4g\ncpus: 2\n", cliargs.Args{Force: true})
	for _, want := range []string{"--cpus 2 ", "--memory 4g "} {
		if !strings.Contains(create+" ", want) {
			t.Errorf("sbx create lacks %q:\n%s", want, create)
		}
	}
}

func TestARebuildLowersWhatItRecordedIfThisMachineCantGiveItNow(t *testing.T) {
	create, _ := rebuiltWith(t, state.Instance{Memory: "200g", CPUs: 40}, "", cliargs.Args{Force: true})
	for _, want := range []string{"--cpus 15 ", "--memory 32g "} {
		if !strings.Contains(create+" ", want) {
			t.Errorf("sbx create lacks %q:\n%s", want, create)
		}
	}
}

// A rebuild started at a terminal asks, starting on what the sandbox has: it's
// the one way to change them. Nobody is there to answer in these tests, so
// "keeping" says what the question started on.
func TestARebuildAtATerminalAsksStartingOnWhatTheSandboxHas(t *testing.T) {
	answerable(t)
	// An agent with no theme to ask about as well.
	_, notes := rebuiltWith(t, state.Instance{Agent: "shell", Memory: "8g", CPUs: 5}, "memory: 12g\ncpus: 10\n", cliargs.Args{})
	got := strings.Join(notes, "\n")
	for _, want := range []string{"keeping cpus: 5", "keeping memory: 8g"} {
		if !strings.Contains(got, want) {
			t.Errorf("the questions didn't start on the sandbox's own sizes; no %q in:\n%s", want, got)
		}
	}
}

func TestARebuildWithForceAsksNothingOfTheSize(t *testing.T) {
	answerable(t)
	_, notes := rebuiltWith(t, state.Instance{Memory: "8g", CPUs: 5}, "", cliargs.Args{Force: true})
	if got := strings.Join(notes, "\n"); strings.Contains(got, "keeping cpus") || strings.Contains(got, "keeping memory") {
		t.Errorf("asked with -f:\n%s", got)
	}
}

// answerable makes vibe think there's a terminal to ask on (so it asks), while
// none can be drawn on: each question is then kept as it started.
func answerable(t *testing.T) {
	t.Helper()
	// Written and closed: Windows can't delete a folder with a file open in it.
	keys := filepath.Join(t.TempDir(), "keys")
	if err := os.WriteFile(keys, []byte(strings.Repeat("\n", 64)), 0o644); err != nil {
		t.Fatal(err)
	}
	old := openTerminalInput
	openTerminalInput = func() (*os.File, func(), bool) {
		f, err := os.Open(keys)
		return f, func() { f.Close() }, err == nil
	}
	t.Cleanup(func() { openTerminalInput = old })
}

// answeringAgent stands in for the user picking an agent on a rebuild: it
// answers with picked, and says what the question started on.
func answeringAgent(t *testing.T, picked string, startedOn *string, asked *bool) {
	t.Helper()
	old := pickRebuildAgent
	pickRebuildAgent = func(current string, ask bool, question string) string {
		*asked = ask
		*startedOn = current
		return picked
	}
	t.Cleanup(func() { pickRebuildAgent = old })
}

// A rebuild is the moment to change agent: the question starts on the one the
// sandbox has, and what's picked is what's built and what's recorded.
func TestARebuildAtATerminalCanSwitchTheAgent(t *testing.T) {
	answerable(t)
	var startedOn string
	var asked bool
	answeringAgent(t, "shell", &startedOn, &asked)
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "claude"
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	calls := loggingSbx(t, "box running\\n")
	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	if !asked || startedOn != "claude" {
		t.Errorf("asked %v, started on %q: want it asked, starting on claude", asked, startedOn)
	}
	if create := createCall(t, calls); !strings.Contains(create, " shell ") || strings.Contains(create, " claude ") {
		t.Errorf("sbx create wasn't for the shell agent:\\n%s", create)
	}
	got, _, _ := state.Load(lay.Home, "box")
	if got.Agent != "shell" {
		t.Errorf("the record says %q, want shell", got.Agent)
	}
}

func TestARebuildWithForceKeepsTheAgentAndDoesntAsk(t *testing.T) {
	var startedOn string
	asked := false
	answeringAgent(t, "shell", &startedOn, &asked)
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "claude"
	state.Save(lay.Home, inst)
	calls := loggingSbx(t, "box running\\n")
	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	if startedOn != "" {
		t.Errorf("the agent was asked about with -f, starting on %q", startedOn)
	}
	if create := createCall(t, calls); !strings.Contains(create, " claude ") {
		t.Errorf("the agent changed under -f:\\n%s", create)
	}
}

// What a Claude sandbox holds is checked for, whatever agent comes next: a
// sandbox rebuilt as another agent mustn't lose its memories unasked.
func TestSwitchingAwayFromClaudeStillChecksTheOldSandboxForMemories(t *testing.T) {
	answerable(t)
	var startedOn string
	var asked bool
	answeringAgent(t, "shell", &startedOn, &asked)
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "claude"
	state.Save(lay.Home, inst)
	calls := loggingSbx(t, "box running\\n")
	notes := notesOf(func() {
		if err := rebuildSandbox(context.Background(), lay, cliargs.Args{}, "box", inst.Target, true, false); err != nil {
			t.Fatal(err)
		}
	})
	if got := strings.Join(notes, "\\n"); !strings.Contains(got, "no memories found in 'box'") {
		t.Errorf("the old sandbox wasn't checked for memories:\\n%s", got)
	}
	if data, _ := os.ReadFile(calls); !strings.Contains(string(data), "exec box") {
		t.Errorf("sbx wasn't asked to look in the sandbox being replaced:\\n%s", data)
	}
}

func TestSwitchingToClaudeDoesntLookForMemoriesTheOldAgentCouldntHave(t *testing.T) {
	answerable(t)
	var startedOn string
	var asked bool
	answeringAgent(t, "claude", &startedOn, &asked)
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "shell"
	state.Save(lay.Home, inst)
	loggingSbx(t, "box running\\n")
	notes := notesOf(func() {
		// Claude also asks which theme: no terminal, so that ends the test
		// before the build; what's checked is what came first.
		rebuildSandbox(context.Background(), lay, cliargs.Args{}, "box", inst.Target, true, false)
	})
	if got := strings.Join(notes, "\\n"); strings.Contains(got, "memories") {
		t.Errorf("a shell sandbox was looked in for Claude's memories:\\n%s", got)
	}
}
