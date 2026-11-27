package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vibe/internal/instructions"
)

// vibe's own one-shot asks of the sandbox's claude say they are vibe's (the clipboard hook leaves the user's pastes to the user's
// next message), have no tools (--tools "" left the MCP gateway's, and the summaries are given other repositories' commits and
// prompts), and leave no session behind in the sandbox, where the agent's memories could pick them up.
func TestAOneShotAskIsMarkedAsVibesOwnAndHasNoTools(t *testing.T) {
	cmd := oneShotCommand(append(append([]string(nil), instructions.Agents["claude"].OneShot...), "--model", "haiku")...)
	for _, want := range []string{"cd /tmp && exec env " + oneShotEnv + " ", "'claude' '-p'", "'--tools' ''", "'--strict-mcp-config'", "'--no-session-persistence'", "'--model' 'haiku'"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("%q lacks %q", cmd, want)
		}
	}
}

func TestTheModelListIsAskedAsVibesOwn(t *testing.T) {
	var scripts []string
	ask := func(ctx context.Context, script string) (string, error) {
		scripts = append(scripts, script)
		if strings.Contains(script, "--help") {
			return realOutputNoT("claude-help-effort.txt"), nil
		}
		return "", errors.New("stop here")
	}
	agentOptions(context.Background(), ask, "/w")
	if len(scripts) < 2 {
		t.Fatalf("want both asks, got %q", scripts)
	}
	for _, s := range scripts {
		if !strings.Contains(s, "env "+oneShotEnv+" claude") {
			t.Errorf("%q isn't marked as vibe's own", s)
		}
	}
}
