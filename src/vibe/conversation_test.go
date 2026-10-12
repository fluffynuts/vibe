package main

import (
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain":      `'plain'`,
		"with space": `'with space'`,
		"it's":       `'it'\''s'`,
		"$(rm -rf)":  `'$(rm -rf)'`,
		"":           `''`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestConversationCommandRunsClaudeInTheWorkspace(t *testing.T) {
	cmd := conversationCommand("/home/me/my project", "")
	if cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != asAgentScript || cmd[3] != "converse" || len(cmd) != 5 {
		t.Fatalf("command = %q", cmd)
	}
	inner := cmd[4]
	if !strings.HasPrefix(inner, `cd '/home/me/my project' && exec 'claude' '-p'`) {
		t.Errorf("inner = %s", inner)
	}
	for _, want := range []string{"'stream-json'", "'bypassPermissions'"} {
		if !strings.Contains(inner, want) {
			t.Errorf("inner lacks %s: %s", want, inner)
		}
	}
	if strings.Contains(inner, "--resume") {
		t.Errorf("a first turn resumes nothing: %s", inner)
	}
}

func TestConversationCommandResumesASessionButNeverAnythingElse(t *testing.T) {
	if inner := conversationCommand("/w", "abc-123")[4]; !strings.HasSuffix(inner, "--resume 'abc-123'") {
		t.Errorf("inner = %s", inner)
	}
	for _, bad := range []string{"x; rm -rf /", "a b", "--dangerously-skip-permissions x", strings.Repeat("a", 101)} {
		if inner := conversationCommand("/w", bad)[4]; strings.Contains(inner, "--resume") {
			t.Errorf("resumed %q: %s", bad, inner)
		}
	}
}

func TestConversationCommandQuotesAWorkspaceThatTriesToBeShell(t *testing.T) {
	inner := conversationCommand("/x'; touch /pwned; '", "")[4]
	if !strings.HasPrefix(inner, `cd '/x'\''; touch /pwned; '\''' && `) {
		t.Errorf("inner = %s", inner)
	}
}

func TestConversationCommandWithoutAWorkspaceStaysWhereItIs(t *testing.T) {
	if inner := conversationCommand("", "")[4]; strings.Contains(inner, "cd ") {
		t.Errorf("inner = %s", inner)
	}
}
