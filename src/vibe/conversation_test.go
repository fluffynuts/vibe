package main

import (
	"context"
	"encoding/json"
	"errors"
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
	for _, want := range []string{"'stream-json'", "'--permission-mode' 'auto'"} {
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
	if !strings.HasSuffix(inner, "'auto' --model 'opus' --effort 'high' --resume 'abc-123'") {
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

// agentSaying starts an agent that reports, at once, having answered in
// session.
func agentSaying(session string) companion.StartAgent {
	return func(_ context.Context, _ string, onLine func([]byte)) (companion.Agent, error) {
		onLine([]byte(`{"type":"result","session_id":"` + session + `"}`))
		return &endedAgent{done: closedChan()}, nil
	}
}

func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// endedAgent is an agent that has already ended.
type endedAgent struct{ done chan struct{} }

func (endedAgent) Prompt(string, string) error { return nil }
func (endedAgent) Interrupt() error            { return nil }
func (endedAgent) StopTask(string) error       { return nil }
func (endedAgent) End()                        {}
func (endedAgent) Kill() error                 { return nil }
func (a endedAgent) Done() <-chan struct{}     { return a.done }
func (endedAgent) Err() error                  { return nil }

func turn(t *testing.T, start companion.StartAgent, session string) {
	t.Helper()
	if _, err := start(context.Background(), session, func([]byte) {}); err != nil {
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

// fakeSession is the agent's stdin and output, as startSession gives them: what
// was written to it, as lines.
type fakeSession struct {
	mu     sync.Mutex
	lines  []string
	closed bool
	done   chan struct{}
}

func newFakeSession() *fakeSession { return &fakeSession{done: make(chan struct{})} }

func (f *fakeSession) WriteLine(line []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, string(line))
	return nil
}

func (f *fakeSession) CloseInput() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	return nil
}

func (f *fakeSession) Done() <-chan struct{} { return f.done }
func (f *fakeSession) Err() error            { return nil }

func (f *fakeSession) written() []map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]interface{}
	for _, l := range f.lines {
		var m map[string]interface{}
		json.Unmarshal([]byte(l), &m)
		out = append(out, m)
	}
	return out
}

// startedAgent starts an agent for session as the page does, with a fake
// session, no leftover to end, and choice as the user's choice; it gives the
// agent, its session and the command it was started with.
func startedAgent(t *testing.T, choice *turnChoice) (companion.Agent, *fakeSession, []string, *[]string) {
	t.Helper()
	sess := newFakeSession()
	var args []string
	var ended []string
	wasStart, wasEnd := startSession, endLeftover
	t.Cleanup(func() { startSession, endLeftover = wasStart, wasEnd })
	startSession = func(_ context.Context, name string, _ func([]byte), a ...string) (agentSession, error) {
		args = a
		return sess, nil
	}
	endLeftover = func(name, pidFile string) error {
		ended = append(ended, name+" "+pidFile)
		return nil
	}
	agent, err := startAgent("box", "/w", func() (turnChoice, error) { return *choice, nil })(context.Background(), "sess-1", func([]byte) {})
	if err != nil {
		t.Fatal(err)
	}
	return agent, sess, args, &ended
}

func TestTheAgentIsStartedOnceForTheConversationAfterWhatAVibeThatDiedLeftIsEnded(t *testing.T) {
	_, _, args, ended := startedAgent(t, &turnChoice{})
	if len(*ended) != 1 || (*ended)[0] != "box "+conversationPidFile {
		t.Errorf("ended %q before starting, want the conversation's pid file", *ended)
	}
	inner := args[len(args)-1]
	for _, want := range []string{"'--input-format' 'stream-json'", conversationPidFile, "--resume 'sess-1'"} {
		if !strings.Contains(inner, want) {
			t.Errorf("the agent's command lacks %s: %s", want, inner)
		}
	}
}

func TestAPromptIsAUserMessageNamedForTheAgentsReceipts(t *testing.T) {
	agent, sess, _, _ := startedAgent(t, &turnChoice{})
	if err := agent.Prompt("hello", "id-1"); err != nil {
		t.Fatal(err)
	}
	w := sess.written()
	if len(w) != 1 || w[0]["type"] != "user" || w[0]["uuid"] != "id-1" {
		t.Fatalf("written %v", w)
	}
	if msg := w[0]["message"].(map[string]interface{}); msg["role"] != "user" || msg["content"] != "hello" {
		t.Errorf("message %v", msg)
	}
}

func TestAChangedChoiceIsToldTheAgentBeforeThePromptWithoutARestart(t *testing.T) {
	choice := turnChoice{Model: "haiku", Effort: "low"}
	agent, sess, _, _ := startedAgent(t, &choice)
	agent.Prompt("one", "id-1")
	if n := len(sess.written()); n != 1 {
		t.Fatalf("an unchanged choice was sent again: %v", sess.written())
	}
	choice = turnChoice{Model: "sonnet", Effort: "high"}
	agent.Prompt("two", "id-2")
	w := sess.written()[1:]
	if len(w) != 3 {
		t.Fatalf("written %v", w)
	}
	model, effort, prompt := w[0]["request"].(map[string]interface{}), w[1]["request"].(map[string]interface{}), w[2]
	if model["subtype"] != "set_model" || model["model"] != "sonnet" {
		t.Errorf("model request %v", model)
	}
	if effort["subtype"] != "apply_flag_settings" || effort["settings"].(map[string]interface{})["effortLevel"] != "high" {
		t.Errorf("effort request %v", effort)
	}
	if prompt["uuid"] != "id-2" {
		t.Errorf("then the prompt: %v", prompt)
	}
}

func TestAChoiceThatCantBeUsedSendsNothing(t *testing.T) {
	agent, sess, _, _ := startedAgent(t, &turnChoice{})
	bad := agent.(*claudeAgent)
	bad.choose = func() (turnChoice, error) { return turnChoice{}, errors.New("the model chosen can't be used") }
	if err := agent.Prompt("hello", "id-1"); err == nil {
		t.Error("a prompt with a choice that can't be used was sent")
	}
	if len(sess.written()) != 0 {
		t.Errorf("written %v", sess.written())
	}
}

func TestAStopSparesTheBackgroundWorkAndATaskCanBeStoppedAlone(t *testing.T) {
	agent, sess, _, _ := startedAgent(t, &turnChoice{})
	agent.Interrupt()
	agent.StopTask("t1")
	w := sess.written()
	interrupt, stop := w[0]["request"].(map[string]interface{}), w[1]["request"].(map[string]interface{})
	if w[0]["type"] != "control_request" || interrupt["subtype"] != "interrupt" || interrupt["spare_background_tasks"] != true {
		t.Errorf("interrupt %v", w[0])
	}
	if stop["subtype"] != "stop_task" || stop["task_id"] != "t1" {
		t.Errorf("stop_task %v", w[1])
	}
	if w[0]["request_id"] == w[1]["request_id"] {
		t.Error("two requests with one id")
	}
}

func TestEndingTheAgentClosesItsInput(t *testing.T) {
	agent, sess, _, _ := startedAgent(t, &turnChoice{})
	agent.End()
	select {
	case <-sess.Done():
	default:
		t.Error("the agent's input is still open")
	}
}

// Bypass can be disabled for a whole organisation by its managed settings (disableBypassPermissionsMode), and Claude Code then
// falls back, silently, to the settings' defaultMode: a Manual one refused nearly everything. Auto is allowed under that policy,
// and wins over a Manual defaultMode, so it is the default; the user can choose another in settings.yaml.
func TestTheAgentRunsInTheChosenPermissionModeElseAuto(t *testing.T) {
	if inner := conversationCommand("/w", "", "/tmp/t.pid", turnChoice{})[4]; !strings.Contains(inner, "'--permission-mode' 'auto'") {
		t.Errorf("unchosen: %s", inner)
	}
	inner := conversationCommand("/w", "", "/tmp/t.pid", turnChoice{PermissionMode: "bypassPermissions"})[4]
	if !strings.Contains(inner, "'--permission-mode' 'bypassPermissions'") || strings.Contains(inner, "'auto'") {
		t.Errorf("chosen bypassPermissions: %s", inner)
	}
	if n := strings.Count(inner, "--permission-mode"); n != 1 {
		t.Errorf("--permission-mode given %d times: %s", n, inner)
	}
}
