package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
