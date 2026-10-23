package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vibe/internal/companion"
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
	cmd := conversationCommand("/home/me/my project", "", "/tmp/t.pid", turnChoice{})
	if cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != asAgentScript || cmd[3] != "converse" || len(cmd) != 5 {
		t.Fatalf("command = %q", cmd)
	}
	inner := cmd[4]
	// After the wrapper that records where the turn runs (see wrapTurn).
	if !strings.HasPrefix(inner, wrapTurn("", "/tmp/t.pid")+`cd '/home/me/my project' && 'claude' '-p'`) {
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
	if inner := conversationCommand("/w", "abc-123", "/tmp/t.pid", turnChoice{})[4]; !strings.HasSuffix(inner, "--resume 'abc-123'") {
		t.Errorf("inner = %s", inner)
	}
	for _, bad := range []string{"x; rm -rf /", "a b", "--dangerously-skip-permissions x", strings.Repeat("a", 101)} {
		if inner := conversationCommand("/w", bad, "/tmp/t.pid", turnChoice{})[4]; strings.Contains(inner, "--resume") {
			t.Errorf("resumed %q: %s", bad, inner)
		}
	}
}

func TestConversationCommandQuotesAWorkspaceThatTriesToBeShell(t *testing.T) {
	inner := conversationCommand("/x'; touch /pwned; '", "", "/tmp/t.pid", turnChoice{})[4]
	if !strings.HasPrefix(inner, wrapTurn("", "/tmp/t.pid")+`cd '/x'\''; touch /pwned; '\''' && `) {
		t.Errorf("inner = %s", inner)
	}
}

func TestConversationCommandWithoutAWorkspaceStaysWhereItIs(t *testing.T) {
	if inner := conversationCommand("", "", "/tmp/t.pid", turnChoice{})[4]; strings.Contains(inner, "cd ") {
		t.Errorf("inner = %s", inner)
	}
}

func TestConversationCommandAsksForAModelAndEffortOnlyWhenChosen(t *testing.T) {
	inner := conversationCommand("/w", "", "/tmp/t.pid", turnChoice{})[4]
	for _, flag := range []string{"--model", "--effort"} {
		if strings.Contains(inner, flag) {
			t.Errorf("no choice, yet %s: %s", flag, inner)
		}
	}
	inner = conversationCommand("/w", "abc-123", "/tmp/t.pid", turnChoice{Model: "opus", Effort: "high"})[4]
	if !strings.HasSuffix(inner, "'bypassPermissions' --model 'opus' --effort 'high' --resume 'abc-123'") {
		t.Errorf("inner = %s", inner)
	}
	inner = conversationCommand("/w", "", "/tmp/t.pid", turnChoice{Model: "sonnet[1m]"})[4]
	if !strings.HasSuffix(inner, "--model 'sonnet[1m]'") || strings.Contains(inner, "--effort") {
		t.Errorf("a model alone: %s", inner)
	}
}

func TestConversationCommandQuotesAModelThatTriesToBeShell(t *testing.T) {
	inner := conversationCommand("/w", "", "/tmp/t.pid", turnChoice{Model: "x'; touch /pwned; '"})[4]
	if !strings.HasSuffix(inner, `--model 'x'\''; touch /pwned; '\'''`) {
		t.Errorf("inner = %s", inner)
	}
}

// fakeSandbox is a sandbox's ~/.claude, as far as keepingTranscript sees it:
// the sessions it has transcripts for, and what was put into it.
type fakeSandbox struct {
	mu       sync.Mutex
	sessions map[string]bool
	puts     [][]byte
	fetched  chan string
}

func (f *fakeSandbox) transcripts() transcripts {
	return transcripts{
		has: func(_ context.Context, session string) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.sessions[session]
		},
		fetch: func(_ context.Context, session string) ([]byte, error) {
			defer func() { f.fetched <- session }()
			return []byte("tgz of " + session), nil
		},
		put: func(_ context.Context, tgz []byte) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.puts = append(f.puts, tgz)
			return nil
		},
	}
}

// keptCopies is told of each copy keepingTranscript is done making, for
// the test to wait on: a test whose folder is cleaned away while a copy is
// still being written fails on Windows and macOS.
func keptCopies(t *testing.T) <-chan struct{} {
	t.Helper()
	kept := make(chan struct{}, 4)
	was := transcriptKept
	transcriptKept = func() { kept <- struct{}{} }
	t.Cleanup(func() { transcriptKept = was })
	return kept
}

// agentSaying is a converser whose agent reports being in session.
func agentSaying(session string) companion.Converser {
	return func(_ context.Context, _, _ string, _ func(), onLine func([]byte)) error {
		onLine([]byte(`{"type":"result","session_id":"` + session + `"}`))
		return nil
	}
}

func turn(t *testing.T, c companion.Converser, session string) {
	t.Helper()
	if err := c(context.Background(), "hi", session, func() {}, func([]byte) {}); err != nil {
		t.Fatal(err)
	}
}

func TestATurnsTranscriptIsKeptOnTheHost(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.transcript.tgz")
	kept := keptCopies(t)
	box := &fakeSandbox{sessions: map[string]bool{}, fetched: make(chan string, 1)}
	turn(t, keepingTranscript(agentSaying("sess-1"), box.transcripts(), file), "")
	if got := <-box.fetched; got != "sess-1" {
		t.Errorf("fetched %q", got)
	}
	<-kept
	if data, err := os.ReadFile(file); err != nil || string(data) != "tgz of sess-1" {
		t.Errorf("kept %q, %v", data, err)
	}
	if len(box.puts) != 0 {
		t.Errorf("a new session was handed a transcript: %q", box.puts)
	}
}

func TestARebuiltSandboxIsHandedTheTranscriptOnce(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.transcript.tgz")
	os.WriteFile(file, []byte("kept"), 0o600)
	kept := keptCopies(t)
	box := &fakeSandbox{sessions: map[string]bool{}, fetched: make(chan string, 2)}
	c := keepingTranscript(agentSaying("sess-1"), box.transcripts(), file)
	turn(t, c, "sess-1")
	<-kept
	turn(t, c, "sess-1")
	<-kept
	if len(box.puts) != 1 || string(box.puts[0]) != "kept" {
		t.Errorf("puts = %q", box.puts)
	}
}

func TestASandboxThatHasTheTranscriptIsLeftAlone(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.transcript.tgz")
	os.WriteFile(file, []byte("kept"), 0o600)
	kept := keptCopies(t)
	box := &fakeSandbox{sessions: map[string]bool{"sess-1": true}, fetched: make(chan string, 1)}
	turn(t, keepingTranscript(agentSaying("sess-1"), box.transcripts(), file), "sess-1")
	<-kept
	if len(box.puts) != 0 {
		t.Errorf("puts = %q", box.puts)
	}
}
