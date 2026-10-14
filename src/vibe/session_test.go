package main

import (
	"strings"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/running"
	"vibe/internal/state"
)

// notesOf collects what vibe says while do runs.
func notesOf(do func()) []string {
	var lines []string
	teeNotes(func(line string) { lines = append(lines, line) })
	defer teeNotes(nil)
	do()
	return lines
}

func TestTheSessionStartsWithATipForEachBlankInstructionsFile(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	writeFile(t, lay.HomePath("agent-instructions.md"), "")
	writeFile(t, lay.HomePath("profiles", "p", "agent-instructions.md"), "  \n")

	got := strings.Join(notesOf(func() { remindOfInstructions(lay, "box") }), "\n")
	for _, want := range []string{"for every sandbox go in", "for this profile's sandboxes go in"} {
		if !strings.Contains(got, want) {
			t.Errorf("no tip saying %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "agent instructions:") {
		t.Errorf("blank files were listed as in use:\n%s", got)
	}

	writeFile(t, lay.HomePath("agent-instructions.md"), "use British spelling\n")
	got = strings.Join(notesOf(func() { remindOfInstructions(lay, "box") }), "\n")
	if !strings.Contains(got, "agent instructions:") || strings.Contains(got, "for every sandbox go in") {
		t.Errorf("once written, the universal file should be listed, not tipped:\n%s", got)
	}
	if !strings.Contains(got, "for this profile's sandboxes go in") {
		t.Errorf("the still-blank profile file lost its tip:\n%s", got)
	}
}

func TestOnlyASessionThatHadItsSandboxCountsAsStoppedUnderIt(t *testing.T) {
	// Neither of these needs to ask sbx: an agent quit as usual, and a
	// sandbox sbx never got going, end vibe as they always have.
	if stoppedUnderSession("box", 0, true) {
		t.Error("a session that ended without an error was taken as stopped under it")
	}
	if stoppedUnderSession("box", 1, false) {
		t.Error("a session whose sandbox was never reached was taken as stopped under it")
	}
}

// A vibe holding the folder for a rebuild says so, before the user is
// offered to stop it, and what stopping it ends.
func TestAFolderTakenForARebuildSaysWhy(t *testing.T) {
	why, stop, busy := othersSay("/work/a", []running.Claim{{PID: 7}, {PID: 8, Work: "rebuilding sandbox 'a'"}})
	joined := strings.Join(why, "\n")
	for _, want := range []string{"vibe is already running for /work/a (PID 7)", "vibe (PID 8) is rebuilding sandbox 'a' for /work/a", "Profiles page"} {
		if !strings.Contains(joined, want) {
			t.Errorf("why doesn't say %q:\n%s", want, joined)
		}
	}
	if !busy || !strings.Contains(stop, "PIDs 7, 8") || !strings.Contains(stop, "half-made") {
		t.Errorf("busy %v, stop = %q", busy, stop)
	}
	why, stop, busy = othersSay("/work/a", []running.Claim{{PID: 7}})
	if busy || stop != "stop vibe (PID 7), then continue" || len(why) != 1 {
		t.Errorf("a session alone: why %v, stop %q, busy %v", why, stop, busy)
	}
}

// With no terminal, the refusal says why the folder is taken.
func TestARefusalForATakenFolderSaysWhy(t *testing.T) {
	err := resolveOthers(t.TempDir(), "/work/a", []running.Claim{{PID: 8, Work: "rebuilding sandbox 'a'"}}, false)
	if err == nil || !strings.Contains(err.Error(), "rebuilding sandbox 'a'") {
		t.Errorf("resolveOthers = %v", err)
	}
}
