package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A sandbox composed before vibe added a command or skill lacks it, and the skill's own text can't say so: it
// isn't there. What the kits tell the agent has to, kindly, with the one thing that puts it right.
func TestEachKitTellsTheAgentWhatToSayWhenItsCommandIsMissing(t *testing.T) {
	for file, command := range map[string]string{
		filepath.Join("base", "review", "agent-instructions.md"):           "vibe-review",
		filepath.Join("dotnet", "agent-instructions.md"):                   "vibe-nuget",
		filepath.Join("base", "clipboard-bridge", "agent-instructions.md"): "clipboard-url",
	} {
		t.Run(command, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "library", file))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, paragraph := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n\n") {
				flat := strings.Join(strings.Fields(paragraph), " ")
				if strings.Contains(flat, command) && strings.Contains(flat, "isn't there") && strings.Contains(flat, "`vibe -C`") {
					found = true
				}
			}
			if !found {
				t.Errorf("%s has no paragraph saying what to do when %s isn't there, naming `vibe -C`", file, command)
			}
		})
	}
}
