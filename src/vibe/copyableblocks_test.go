package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Agents are told, in every guided sandbox, to hand over text meant for pasting as one fenced block the page can copy from, and to
// tag Markdown as such so that the page shows it formatted: the instruction is what makes the page's block worth having, so
// what it has to say is pinned.
func TestEverySandboxIsToldToPutTextToPasteInOneTaggedBlock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "library", "base", "copyable-blocks", "agent-instructions.md"))
	if err != nil {
		t.Fatalf("the base feature that tells agents so: %v", err)
	}
	text := string(data)
	for what, want := range map[string]string{
		"one block, with only the text in it": "exactly that text",
		"the Markdown tag":                    "markdown",
		"remarks outside":                     "outside",
		"a longer fence round fences":         "four backticks",
		"no tag for showing syntax":           "as typed",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the instruction doesn't say %s (%q):\n%s", what, want, text)
		}
	}
}
