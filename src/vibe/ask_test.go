package main

import (
	"testing"

	"vibe/internal/prompt"
	"vibe/internal/state"
)

func TestRecordedAgentAndTimezoneComeFromTheRecord(t *testing.T) {
	home := t.TempDir()
	if err := state.Save(home, state.Instance{Name: "proj", Agent: "opencode", Timezone: "Europe/Paris"}); err != nil {
		t.Fatal(err)
	}
	if got := recordedAgent(home, "proj", "claude"); got != "opencode" {
		t.Errorf("recordedAgent = %q", got)
	}
	if got := recordedTimezone(home, "proj"); got != "Europe/Paris" {
		t.Errorf("recordedTimezone = %q", got)
	}
}

func TestRecordedFallsBackWhereTheRecordIsMissingOrEmpty(t *testing.T) {
	home := t.TempDir()
	if err := state.Save(home, state.Instance{Name: "bare"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bare", "never-made"} {
		if got := recordedAgent(home, name, "claude"); got != "claude" {
			t.Errorf("recordedAgent(%s) = %q, want the default", name, got)
		}
		if got := recordedTimezone(home, name); got == "" {
			t.Errorf("recordedTimezone(%s) is empty, want the host's", name)
		}
	}
}

func TestAskingGivesUpWithoutATerminal(t *testing.T) {
	if interactive() {
		t.Skip("there is a terminal to ask on")
	}
	asked := false
	got, ok := asking("which?", func(*prompt.Terminal) (int, bool) { asked = true; return 7, true })
	if ok || got != 0 || asked {
		t.Errorf("asking = %d, %v (asked: %v); want 0, false, never asked", got, ok, asked)
	}
}

// Which agent a sandbox runs is picked from a list that grows with sbx, and the filter narrows it as the user types.
func TestTheAgentIsPickedWithTheFilterStartingOnTheOneInUse(t *testing.T) {
	old := chooseAgent
	t.Cleanup(func() { chooseAgent = old })
	var asked []string
	var started int
	chooseAgent = func(question string, labels []string, def int) (int, bool) {
		asked = labels
		started = def
		return len(labels) - 1, true
	}
	got := pickAgent("claude", true, "which agent?")
	if len(asked) == 0 || got != asked[len(asked)-1] {
		t.Errorf("picked %q from %v", got, asked)
	}
	if asked[started] != "claude" {
		t.Errorf("the list started on %q, not on the agent in use", asked[started])
	}
}

func TestAnAgentNotInSbxsListIsStillOfferedFirstAndKeptWhenThePickIsCancelled(t *testing.T) {
	old := chooseAgent
	t.Cleanup(func() { chooseAgent = old })
	var asked []string
	chooseAgent = func(question string, labels []string, def int) (int, bool) {
		asked = labels
		return 0, false
	}
	if got := pickAgent("brand-new-agent", true, "which agent?"); got != "brand-new-agent" {
		t.Errorf("cancelling changed the agent to %q", got)
	}
	if len(asked) == 0 || asked[0] != "brand-new-agent" {
		t.Errorf("the agent in use isn't first in %v", asked)
	}
}

func TestNothingIsAskedWhenTheUserIsNotAsked(t *testing.T) {
	old := chooseAgent
	t.Cleanup(func() { chooseAgent = old })
	chooseAgent = func(string, []string, int) (int, bool) { t.Error("asked"); return 0, false }
	if got := pickAgent("", false, "q"); got != "claude" {
		t.Errorf("the default is %q", got)
	}
}
