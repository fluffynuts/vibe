package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/instructions"
)

// The agent's background work goes on when the user prompts, and calls it back; a stop spares it, unless the user stops it too; and the run
// ends once it has no turn and no tasks, taking anything else it left waiting. Nothing in the agent says so: every guided sandbox tells it,
// with the way to leave work running that outlives the run. What it has to say is pinned.
func TestEverySandboxIsToldWhatBecomesOfItsBackgroundWork(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "library", "base", "background-work", "agent-instructions.md"))
	if err != nil {
		t.Fatalf("the base feature that tells agents so: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "## "+instructions.BackgroundWorkHeading+"\n") {
		t.Fatalf("the feature has no %q section for the pointer to name:\n%s", instructions.BackgroundWorkHeading, text)
	}
	for what, want := range map[string]string{
		"that a new prompt leaves it running":   "keeps running when they send the",
		"that it is called back":                "you are called back",
		"that a stop spares it, then offers":    "leaves your background tasks running, and the page then offers",
		"that a stopped task did not finish":    "rather than assuming it finished",
		"that the run ends once it has no work": "about 30 seconds after you have no turn and no tasks",
		"the detached way":                      "setsid nohup",
		"that no notice comes of detached work": "won't be told",
		"never to kill by name":                 "never by name",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the instruction doesn't say %s (%q):\n%s", what, want, text)
		}
	}
}
