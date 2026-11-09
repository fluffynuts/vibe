package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/claudetheme"
	"vibe/internal/cliargs"
	agentmem "vibe/internal/memory"
	"vibe/internal/sbxtest"
	"vibe/internal/state"
)

// rebuiltWithStore rebuilds sandbox "box" (agent says which agent it has) while sbx has no sandbox of that name, as it is after a
// rebuild that backed the memories up, removed the sandbox and then died making the new one (the image fetch failed): the run
// that retries finds nothing to back up, and nothing in it knows memories were saved. storeFiles are put in the sandbox's memory
// store first. It returns the sbx calls and vibe's notes.
func rebuiltWithStore(t *testing.T, agent string, storeFiles map[string]string) (calls string, notes string, store string) {
	t.Helper()
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = agent
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	store = agentmem.StoreFor(lay.Home, "", "box")
	for name, content := range storeFiles {
		writeFile(t, filepath.Join(store, name), content)
	}
	log := loggingSbx(t, "")
	got := notesOf(func() {
		if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
			t.Fatal(err)
		}
	})
	data, _ := os.ReadFile(log)
	return string(data), strings.Join(got, "\n"), store
}

func TestASandboxMadeWhereItsMemoriesWereLeftInTheStoreGetsThemBack(t *testing.T) {
	calls, notes, store := rebuiltWithStore(t, "claude", map[string]string{"-proj/memory/MEMORY.md": "- a memory\n"})
	if want := "cp -a " + store + "/. " + agentmem.AgentPath + "/"; !strings.Contains(calls, want) {
		t.Errorf("sbx wasn't asked to copy the memories in (%q):\n%s", want, calls)
	}
	if !strings.Contains(notes, "memories restored from "+store) {
		t.Errorf("the memories coming back weren't said:\n%s", notes)
	}
	if _, err := os.Stat(filepath.Join(store, "-proj", "memory", "MEMORY.md")); err != nil {
		t.Errorf("the store lost what it held: %v", err)
	}
}

func TestASandboxMadeWithNothingInItsMemoryStoreRestoresNothing(t *testing.T) {
	calls, notes, _ := rebuiltWithStore(t, "claude", nil)
	if strings.Contains(calls, "cp -a") || strings.Contains(notes, "restored") {
		t.Errorf("something was restored from an empty store:\n%s\n%s", calls, notes)
	}
}

func TestASandboxForAnAgentThatCantUseMemoriesLeavesThemWhereTheyAre(t *testing.T) {
	calls, notes, store := rebuiltWithStore(t, "shell", map[string]string{"-proj/memory/MEMORY.md": "- a memory\n"})
	if strings.Contains(calls, "cp -a") || strings.Contains(notes, "restored") {
		t.Errorf("Claude's memories were put in a shell sandbox:\n%s\n%s", calls, notes)
	}
	if _, err := os.Stat(filepath.Join(store, "-proj", "memory", "MEMORY.md")); err != nil {
		t.Errorf("the store lost what it held: %v", err)
	}
}

// A rebuild that saved the memories and removed the sandbox, then couldn't make the new one, says the sandbox is gone and the
// memories are safe, and that going again puts them back: otherwise all there is to see is an error about an image.
func TestARebuildThatFailsToMakeTheSandboxSaysWhereTheMemoriesAreAndThatTheNextRunRestoresThem(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "claude"
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	sbxtest.OnPath(t, "[ \"$1\" = ls ] && [ \"$2\" = --quiet ] && { echo box; exit 0; }\n"+
		sbxLsJSON("box running\\n")+
		"case \"$*\" in *' find '*) echo '"+agentmem.AgentPath+"/x'; exit 0;; esac\n"+
		"case \"$1\" in create) echo 'fetch image config: EOF' >&2; exit 1;; esac\n"+
		"exit 0\n")
	store := agentmem.StoreFor(lay.Home, "", "box")

	var err error
	notes := strings.Join(notesOf(func() {
		err = rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false)
	}), "\n")
	if err == nil {
		t.Fatal("making the sandbox was meant to fail")
	}
	for _, want := range []string{"memories saved to " + store, "is gone", "memories are safe in " + store, "restores them"} {
		if !strings.Contains(notes, want) {
			t.Errorf("the notes don't say %q:\n%s", want, notes)
		}
	}
}

// memorySbx stands in for sbx: it logs every call (the file it returns), lists the sandboxes in rows ("box running\\n"), and says
// a sandbox it is asked has memories in it.
func memorySbx(t *testing.T, rows string) string {
	t.Helper()
	dir := sbxtest.OnPath(t, "echo \"$*\" >>\"$(dirname \"$0\")/calls\"\n"+
		"[ \"$1\" = ls ] && [ \"$2\" = --quiet ] && { printf '"+rows+"' | cut -d' ' -f1; exit 0; }\n"+
		sbxLsJSON(rows)+
		"case \"$*\" in *' find '*) echo '"+agentmem.AgentPath+"/x'; exit 0;; esac\n"+
		"exit 0\n")
	return filepath.Join(dir, "calls")
}

// answeringEveryQuestion makes vibe think there is a terminal, and answers each yes/no question with answer ("y" or "n").
func answeringEveryQuestion(t *testing.T, answer string) {
	t.Helper()
	keys := filepath.Join(t.TempDir(), "keys")
	if err := os.WriteFile(keys, []byte(strings.Repeat(answer+"\n", 64)), 0o644); err != nil {
		t.Fatal(err)
	}
	old := openTerminalInput
	openTerminalInput = func() (*os.File, func(), bool) {
		f, err := os.Open(keys)
		return f, func() { f.Close() }, err == nil
	}
	t.Cleanup(func() { openTerminalInput = old })
}

// rebuiltAtATerminalAnswering rebuilds a running Claude sandbox "box" whose memory store holds a memory, the user answering every
// yes/no question with answer: among them, whether to preserve the sandbox's memories. It returns the sbx calls, vibe's notes and
// the store.
func rebuiltAtATerminalAnswering(t *testing.T, answer string) (calls, notes, store string) {
	t.Helper()
	answerable(t)
	answeringEveryQuestion(t, answer)
	var startedOn string
	var asked bool
	answeringAgent(t, "claude", &startedOn, &asked)
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "claude"
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	// With a theme on record, Claude doesn't ask which one: nothing can be drawn on here.
	if err := claudetheme.SaveToProfile(lay.ProfileDir("p"), claudetheme.Default); err != nil {
		t.Fatal(err)
	}
	store = agentmem.StoreFor(lay.Home, "", "box")
	writeFile(t, filepath.Join(store, "-proj", "memory", "MEMORY.md"), "- last session's memory\n")
	log := memorySbx(t, "box running\\n")
	got := notesOf(func() {
		if err := rebuildSandbox(context.Background(), lay, cliargs.Args{}, "box", inst.Target, true, false); err != nil {
			t.Fatal(err)
		}
	})
	data, _ := os.ReadFile(log)
	return string(data), strings.Join(got, "\n"), store
}

// The store is kept up to date by the copy at the end of each session, so it holds the sandbox's memories whether or not the user
// agrees to preserve them: saying no has to mean the new sandbox starts without them.
func TestARebuildWhereTheUserDeclinesToPreserveMemoriesRestoresNone(t *testing.T) {
	calls, notes, store := rebuiltAtATerminalAnswering(t, "n")
	if !strings.Contains(notes, "removing sandbox 'box'") {
		t.Fatalf("the sandbox wasn't rebuilt:\n%s", notes)
	}
	if restore := "cp -a " + store + "/. " + agentmem.AgentPath; strings.Contains(calls, restore) || strings.Contains(notes, "restored") {
		t.Errorf("memories were put into the new sandbox after the user said no:\n%s\n%s", calls, notes)
	}
	if !strings.Contains(notes, "not preserving the memories in 'box'") {
		t.Errorf("vibe didn't say the new sandbox starts without them:\n%s", notes)
	}
	if _, err := os.Stat(filepath.Join(store, "-proj", "memory", "MEMORY.md")); err != nil {
		t.Errorf("declining took what the store held away: %v", err)
	}
}

func TestARebuildWhereTheUserAgreesToPreserveMemoriesSavesThemAndRestoresThem(t *testing.T) {
	calls, notes, store := rebuiltAtATerminalAnswering(t, "y")
	for what, want := range map[string]string{
		"saved out":   "cp -a " + agentmem.AgentPath + "/. " + store + "/",
		"put back in": "cp -a " + store + "/. " + agentmem.AgentPath + "/",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("the memories weren't %s (%q):\n%s", what, want, calls)
		}
	}
	if !strings.Contains(notes, "memories restored from "+store) {
		t.Errorf("the memories coming back weren't said:\n%s", notes)
	}
}

// What the note after a failed make promises: with a record, no sandbox and a store holding memories, plain vibe makes the
// sandbox and puts them in.
func TestPlainVibeInAFolderWhoseSandboxIsGoneMakesItAndRestoresItsMemories(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.Agent = "claude"
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	store := agentmem.StoreFor(lay.Home, "", "box")
	writeFile(t, filepath.Join(store, "-proj", "memory", "MEMORY.md"), "- saved before the sandbox went\n")
	log := memorySbx(t, "")

	var err error
	notes := strings.Join(notesOf(func() {
		err = doCreateOrAttach(lay, cliargs.Args{Force: true, NoStart: true, Profile: "p"}, "box", inst.Target)
	}), "\n")
	if err != nil {
		t.Fatalf("doCreateOrAttach = %v\n%s", err, notes)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "create") {
		t.Errorf("no sandbox was made:\n%s\n%s", calls, notes)
	}
	if want := "cp -a " + store + "/. " + agentmem.AgentPath + "/"; !strings.Contains(string(calls), want) || !strings.Contains(notes, "memories restored from "+store) {
		t.Errorf("the memories weren't put into the new sandbox (%q):\n%s\n%s", want, calls, notes)
	}
}
