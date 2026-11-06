package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/instructions"
	"vibe/internal/layout"
	"vibe/internal/state"
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
	// The rule agents broke when it was a bullet at the end: it is in the opening paragraph, before the list.
	if opening, _, _ := strings.Cut(text, "\n- "); !strings.Contains(opening, "four backticks") {
		t.Errorf("the four-backticks rule isn't up front, before the list:\n%s", text)
	}
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

// The kits' instructions are only read by an agent when it thinks a kit is relevant, and one asked for a pull request description
// doesn't (seen with a fresh agent, which never opened them): the instructions it always loads have to send it there.
func TestTheInstructionsAnAgentAlwaysLoadsSendItToTheCopyableBlocksSection(t *testing.T) {
	feature, err := os.ReadFile(filepath.Join(repoRoot(t), "library", "base", "copyable-blocks", "agent-instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(feature), "## "+instructions.CopyableBlocksHeading+"\n") {
		t.Fatalf("the feature has no %q section for the pointer to name", instructions.CopyableBlocksHeading)
	}

	lay := layout.New(t.TempDir(), t.TempDir())
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	writeFile(t, lay.HomePath("agent-instructions.md"), "Be kind.")
	writeFile(t, lay.HomePath("profiles", "p", "kit-instructions.md"), string(feature))

	merged, _, err := instructionsFor(lay, "box")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Be kind.", instructions.CopyableBlocksHeading} {
		if !strings.Contains(merged, want) {
			t.Errorf("the always-loaded instructions lack %q:\n%s", want, merged)
		}
	}

	writeFile(t, lay.HomePath("profiles", "p", "kit-instructions.md"), "")
	if merged, _, _ := instructionsFor(lay, "box"); strings.Contains(merged, instructions.CopyableBlocksHeading) {
		t.Errorf("a profile without the feature is pointed to it:\n%s", merged)
	}
}
