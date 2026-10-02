package sbxrun

import (
	"reflect"
	"testing"
	"time"
)

// TestStartGivesUpOnASandboxItCannotBoot checks Start's failure path: with no
// such sandbox (or no sbx at all) it reports false promptly rather than
// waiting out startTimeout on something that is never going to answer.
func TestStartGivesUpOnASandboxItCannotBoot(t *testing.T) {
	done := make(chan bool, 1)
	go func() { done <- Start("vibe-test-no-such-sandbox-3f9c1") }()
	select {
	case started := <-done:
		if started {
			t.Error("Start reported a nonexistent sandbox as running")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Start hung on a sandbox that cannot boot")
	}
}

func TestParseAgents(t *testing.T) {
	help := "Omit the path to mount the current directory.\n\n" +
		"Available agents: claude, codex, copilot, cursor, devin, docker-agent, droid, gemini, kiro, opencode, shell\n\n" +
		"With --cloud:\n"
	want := []string{"claude", "codex", "copilot", "cursor", "devin", "docker-agent", "droid", "gemini", "kiro", "opencode", "shell"}
	if got := ParseAgents(help); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAgents = %v, want %v", got, want)
	}
	if got := ParseAgents("no agents here\n"); got != nil {
		t.Errorf("ParseAgents of help without the line = %v", got)
	}
}
