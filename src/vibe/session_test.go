package main

import (
	"context"
	"errors"
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
	if busy || stop != "stop vibe (PID 7) — it saves its agent's memories first — then continue" || len(why) != 1 {
		t.Errorf("a session alone: why %v, stop %q, busy %v", why, stop, busy)
	}
}

// The menu says what each choice does to the other vibe: stopping ends it
// properly (its agent's memories are saved), carrying on leaves it running.
func TestTheMenuForATakenFolderSaysWhatEachChoiceDoesToTheOtherVibe(t *testing.T) {
	menu := folderMenu([]running.Claim{{PID: 7}}, false)
	if len(menu.labels) != 3 || menu.choices[1] != chooseContinue || menu.choices[2] != chooseStop {
		t.Fatalf("menu = %+v, want exit, continue, stop", menu)
	}
	if menu.labels[1] != "continue anyway, leaving vibe (PID 7) running beside this one" {
		t.Errorf("continue is %q", menu.labels[1])
	}
	if !strings.Contains(menu.labels[2], "stop vibe (PID 7)") || !strings.Contains(menu.labels[2], "memories") {
		t.Errorf("stop is %q", menu.labels[2])
	}
}

// A rebuild or delete takes the sandbox away from whatever else is using it:
// carrying on beside the other vibe isn't offered.
func TestNoContinuingBesideAnotherVibeWhenTheSandboxIsAboutToGo(t *testing.T) {
	menu := folderMenu([]running.Claim{{PID: 7}}, true)
	if len(menu.labels) != 2 || menu.choices[0] != chooseExit || menu.choices[1] != chooseStop {
		t.Errorf("menu = %+v, want just exit and stop", menu)
	}
	for _, label := range menu.labels {
		if strings.Contains(label, "continue anyway") {
			t.Errorf("offered %q", label)
		}
	}
}

// -f doesn't leave two vibes on a sandbox that's being rebuilt or deleted: it
// stops the other first, as the menu's third choice does.
func TestForceStopsTheOtherVibeBeforeARebuildRatherThanCarryingOnBesideIt(t *testing.T) {
	var said []string
	teeNotes(func(line string) { said = append(said, line) })
	defer teeNotes(nil)
	// No live vibe holds this home's folder, so there's nothing to stop.
	if err := resolveOthers(t.TempDir(), "/work/a", []running.Claim{{PID: 7}}, true, true); err != nil {
		t.Fatalf("resolveOthers = %v", err)
	}
	got := strings.Join(said, "\n")
	if strings.Contains(got, "continuing anyway") || !strings.Contains(got, "stopping") {
		t.Errorf("said:\n%s\nwant it to say it's stopping the other, not carrying on beside it", got)
	}
}

// ...but never one in the middle of work (a rebuild half done): -f won't end
// that unasked.
func TestForceWontEndAnotherVibesWorkPartWayToStartARebuild(t *testing.T) {
	err := resolveOthers(t.TempDir(), "/work/a", []running.Claim{{PID: 8, Work: "rebuilding sandbox 'a'"}}, true, true)
	if err == nil || !strings.Contains(err.Error(), "rebuilding sandbox 'a'") || !strings.Contains(err.Error(), "without -f") {
		t.Errorf("resolveOthers = %v, want a refusal saying why and what to do", err)
	}
}

// Anything that isn't taking the sandbox away keeps -f's old meaning.
func TestForceStillCarriesOnBesideAnotherVibeWhenNothingIsBeingTakenAway(t *testing.T) {
	var said []string
	teeNotes(func(line string) { said = append(said, line) })
	defer teeNotes(nil)
	if err := resolveOthers(t.TempDir(), "/work/a", []running.Claim{{PID: 7}}, true, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(said, "\n"), "continuing anyway") {
		t.Errorf("said %q", said)
	}
}

// With no terminal, the refusal says why the folder is taken.
func TestARefusalForATakenFolderSaysWhy(t *testing.T) {
	err := resolveOthers(t.TempDir(), "/work/a", []running.Claim{{PID: 8, Work: "rebuilding sandbox 'a'"}}, false, false)
	if err == nil || !strings.Contains(err.Error(), "rebuilding sandbox 'a'") {
		t.Errorf("resolveOthers = %v", err)
	}
}

// While a page's action runs, what sbx prints goes to its log, as plain
// lines; otherwise to the terminal (nil).
func TestSbxsOutputGoesToTheActionsLogWhileOneRuns(t *testing.T) {
	if actionOutput() != nil {
		t.Fatal("with no action running, sbx's output didn't go to the terminal")
	}
	var lines []string
	teeNotes(func(line string) { lines = append(lines, line) })
	defer teeNotes(nil)
	out := actionOutput()
	if out == nil {
		t.Fatal("while an action runs, sbx's output went to the terminal")
	}
	out.Write([]byte("✓ image \x1b[32mready\x1b[0m\r\n\n  creat"))
	out.Write([]byte("ing\rdone\n"))
	if want := []string{"✓ image ready", "  creating", "done"}; strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", lines, want)
	}
}

// fakeWake stands in for sbx and the memory backup while a session that
// was waiting on its stopped sandbox ends, recording what was asked of them.
func fakeWake(t *testing.T, starts bool, stopErr error) *[]string {
	t.Helper()
	var calls []string
	oldStart, oldSave, oldStop := startToBackUp, backUpMemories, stopAfterBackUp
	t.Cleanup(func() { startToBackUp, backUpMemories, stopAfterBackUp = oldStart, oldSave, oldStop })
	startToBackUp = func(_ context.Context, name string) bool { calls = append(calls, "start "+name); return starts }
	backUpMemories = func(_, name string) { calls = append(calls, "save "+name) }
	stopAfterBackUp = func(_ context.Context, name string) error { calls = append(calls, "stop "+name); return stopErr }
	return &calls
}

// A session that ends while its sandbox is stopped (asleep, or stopped
// from the page) still has its agent's memories in the sandbox: vibe wakes
// it to back them up, then leaves it stopped as it found it.
func TestEndingWhileTheSandboxIsStoppedWakesItToBackUpMemories(t *testing.T) {
	calls := fakeWake(t, true, nil)
	notesOf(func() { backUpWhileStopped(t.TempDir(), "box") })
	if got, want := strings.Join(*calls, ", "), "start box, save box, stop box"; got != want {
		t.Errorf("asked %q, want %q", got, want)
	}
}

func TestASandboxThatWontWakeSaysTheMemoriesAreStillInIt(t *testing.T) {
	calls := fakeWake(t, false, nil)
	notes := notesOf(func() { backUpWhileStopped(t.TempDir(), "box") })
	if got := strings.Join(*calls, ", "); got != "start box" {
		t.Errorf("asked %q, want only the start", got)
	}
	if said := strings.Join(notes, "\n"); !strings.Contains(said, "weren't backed up") || !strings.Contains(said, "still in it") {
		t.Errorf("said %q, want why the memories weren't backed up and that they aren't lost", said)
	}
}

func TestASandboxThatWontStopAgainSaysHowToStopIt(t *testing.T) {
	fakeWake(t, true, errors.New("sbx stop: daemon not running"))
	notes := notesOf(func() { backUpWhileStopped(t.TempDir(), "box") })
	if said := strings.Join(notes, "\n"); !strings.Contains(said, "daemon not running") || !strings.Contains(said, "vibe --stop") {
		t.Errorf("said %q, want sbx's reason and what to do", said)
	}
}

// fakeSandboxState says whether the sandbox is running, and reachable, as
// a headless vibe ends.
func fakeSandboxState(t *testing.T, running, known, reachable bool) {
	t.Helper()
	oldRunning, oldReachable := sandboxRunning, sandboxReachable
	t.Cleanup(func() { sandboxRunning, sandboxReachable = oldRunning, oldReachable })
	sandboxRunning = func(string) (bool, bool) { return running, known }
	sandboxReachable = func(context.Context, string) bool { return reachable }
}

// A headless vibe ending with its sandbox asleep wakes it to back up its
// agent's memories, saying so, and leaves it stopped as it found it.
func TestAHeadlessVibeEndingWithItsSandboxAsleepWakesItToBackUp(t *testing.T) {
	calls := fakeWake(t, true, nil)
	fakeSandboxState(t, false, true, false)
	notesOf(func() { backUpAsHeadlessEnds(t.TempDir(), "box", true) })
	if got, want := strings.Join(*calls, ", "), "start box, save box, stop box"; got != want {
		t.Errorf("asked %q, want %q", got, want)
	}
}

func TestAHeadlessVibeEndingWithItsSandboxUpBacksUpAndLeavesItRunning(t *testing.T) {
	calls := fakeWake(t, true, nil)
	fakeSandboxState(t, true, true, true)
	notes := notesOf(func() { backUpAsHeadlessEnds(t.TempDir(), "box", false) })
	if got := strings.Join(*calls, ", "); got != "save box" {
		t.Errorf("asked %q, want just the backup", got)
	}
	if said := strings.Join(notes, "\n"); !strings.Contains(said, "left sandbox 'box' running") {
		t.Errorf("said %q, want that it was left running", said)
	}
	*calls = nil
	notes = notesOf(func() { backUpAsHeadlessEnds(t.TempDir(), "box", true) })
	if strings.Contains(strings.Join(notes, "\n"), "left sandbox") {
		t.Errorf("asked to stop, it said the sandbox was left running: %q", notes)
	}
}
