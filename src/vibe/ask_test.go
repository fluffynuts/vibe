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
