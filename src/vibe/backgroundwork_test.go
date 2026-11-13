package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/instructions"
)

// A new prompt ends what the last run left running (the server supersedes the turn), and the agent is not told: so every guided sandbox
// tells it, with the one way to leave work running that survives, and what to do about it. What it has to say is pinned.
func TestEverySandboxIsToldThatANewPromptEndsBackgroundWork(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "library", "base", "background-work", "agent-instructions.md"))
	if err != nil {
		t.Fatalf("the base feature that tells agents so: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "## "+instructions.BackgroundWorkHeading+"\n") {
		t.Fatalf("the feature has no %q section for the pointer to name:\n%s", instructions.BackgroundWorkHeading, text)
	}
	for what, want := range map[string]string{
		"that a new prompt ends it":           "a new prompt ends",
		"not to leave it pending at turn end": "Don't end your turn with background work pending",
		"the detached way":                    "setsid nohup",
		"that no notice comes":                "won't be told",
		"never to kill by name":               "never by name",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the instruction doesn't say %s (%q):\n%s", what, want, text)
		}
	}
}
