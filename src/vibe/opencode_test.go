package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vibe/internal/state"
)

func TestOpencodeCommandRunsOpencodeInTheWorkspaceWithJSONEventsAndNoTerminal(t *testing.T) {
	cmd := opencodeCommand("/home/me/my project", "", "/tmp/t.pid", "", "")
	if cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != asAgentScript || cmd[3] != "converse" || len(cmd) != 5 {
		t.Fatalf("command = %q", cmd)
	}
	inner := cmd[4]
	if !strings.HasPrefix(inner, wrapTurn("", "/tmp/t.pid")+`cd '/home/me/my project' && `) {
		t.Errorf("inner = %s", inner)
	}
	for _, want := range []string{"'opencode'", "'run'", "'--format'", "'json'"} {
		if !strings.Contains(inner, want) {
			t.Errorf("inner lacks %s: %s", want, inner)
		}
	}
	for _, not := range []string{"-s ", "-m ", "--variant", "--auto"} {
		if strings.Contains(inner, not) {
			t.Errorf("a first turn with nothing chosen has %q: %s", not, inner)
		}
	}
}

func TestOpencodeCommandKeepsThePromptOffItsCommandLine(t *testing.T) {
	// The prompt goes in on stdin (see sbxrun.ExecStream): nothing of it can be in the command, where it would be
	// quoted wrongly, or seen in ps.
	if inner := opencodeCommand("/w", "", "/tmp/t.pid", "", "")[4]; strings.Contains(inner, "prompt") {
		t.Errorf("inner = %s", inner)
	}
}

func TestOpencodeCommandResumesASessionButNeverAnythingElse(t *testing.T) {
	if inner := opencodeCommand("/w", "ses_e5a2b4b9affeqA6a9GygRjN4KP", "/tmp/t.pid", "", "")[4]; !strings.HasSuffix(inner, "'-s' 'ses_e5a2b4b9affeqA6a9GygRjN4KP'") {
		t.Errorf("inner = %s", inner)
	}
	for _, bad := range []string{"x; rm -rf /", "a b", "-s x", strings.Repeat("a", 101)} {
		if inner := opencodeCommand("/w", bad, "/tmp/t.pid", "", "")[4]; strings.Contains(inner, "'-s'") {
			t.Errorf("resumed %q: %s", bad, inner)
		}
	}
}

func TestOpencodeCommandAsksForTheModelAndEffortItWasGiven(t *testing.T) {
	inner := opencodeCommand("/w", "", "/tmp/t.pid", "opencode/big-pickle", "high")[4]
	for _, want := range []string{"'-m' 'opencode/big-pickle'", "'--variant' 'high'"} {
		if !strings.Contains(inner, want) {
			t.Errorf("inner lacks %s: %s", want, inner)
		}
	}
}

func TestOpencodeCommandQuotesAWorkspaceThatTriesToBeShell(t *testing.T) {
	inner := opencodeCommand("/x'; touch /pwned; '", "", "/tmp/t.pid", "", "")[4]
	if !strings.HasPrefix(inner, wrapTurn("", "/tmp/t.pid")+`cd '/x'\''; touch /pwned; '\''' && `) {
		t.Errorf("inner = %s", inner)
	}
}

// opencodeSays stands in for the sandbox's opencode: it gives lines, as the events OpenCode prints, and ends with err.
func opencodeSays(t *testing.T, lines []string, err error) (prompts *[]string) {
	t.Helper()
	old := opencodeStream
	t.Cleanup(func() { opencodeStream = old })
	var got []string
	opencodeStream = func(ctx context.Context, name, input string, delivered func(), onLine func([]byte), args ...string) error {
		got = append(got, input)
		delivered()
		for _, l := range lines {
			onLine([]byte(l))
		}
		return err
	}
	return &got
}

const (
	ocText   = `{"type":"text","sessionID":"ses_a","part":{"type":"text","text":"hello"}}`
	ocFinish = `{"type":"step_finish","sessionID":"ses_a","part":{"type":"step-finish","reason":"stop"}}`
	ocError  = `{"type":"error","sessionID":"ses_a","error":{"name":"UnknownError","data":{"message":"Unexpected server error"}}}`
)

func TestATurnGivesThePageWhatOpencodeSaidAsWhatItDraws(t *testing.T) {
	prompts := opencodeSays(t, []string{ocText, ocFinish}, nil)
	var seen []string
	delivered := false
	err := converseOpencode("box", "/w")(context.Background(), "do the thing", "", func() { delivered = true }, func(l []byte) { seen = append(seen, string(l)) })
	if err != nil {
		t.Fatal(err)
	}
	if len(*prompts) != 1 || (*prompts)[0] != "do the thing" || !delivered {
		t.Errorf("prompts %q, delivered %v", *prompts, delivered)
	}
	if len(seen) != 2 || !strings.Contains(seen[0], `"type":"assistant"`) || !strings.Contains(seen[1], `"type":"result"`) {
		t.Errorf("lines = %q", seen)
	}
}

// OpenCode ends in exit status 1 after an error event: the event has said it, so the page isn't told twice.
func TestAnErrorOpencodeSaidIsNotSaidAgainAsAnExitStatus(t *testing.T) {
	opencodeSays(t, []string{ocError}, errors.New("exit status 1"))
	var seen []string
	err := converseOpencode("box", "/w")(context.Background(), "x", "", func() {}, func(l []byte) { seen = append(seen, string(l)) })
	if err != nil {
		t.Errorf("the turn failed again as %v", err)
	}
	if len(seen) != 1 || !strings.Contains(seen[0], `"is_error":true`) || !strings.Contains(seen[0], "Unexpected server error") {
		t.Errorf("lines = %q", seen)
	}
}

func TestAFailureOpencodeSaidNothingAboutIsTheTurnFailing(t *testing.T) {
	opencodeSays(t, nil, errors.New("exit status 127: opencode: not found"))
	err := converseOpencode("box", "/w")(context.Background(), "x", "", func() {}, func([]byte) {})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestStoppingATurnEndsWhatItStartedInTheSandbox(t *testing.T) {
	old := stopTurn
	t.Cleanup(func() { stopTurn = old })
	var stopped string
	stopTurn = func(name, pidFile string) { stopped = name + " " + pidFile }
	oldStream := opencodeStream
	t.Cleanup(func() { opencodeStream = oldStream })
	ctx, cancel := context.WithCancel(context.Background())
	opencodeStream = func(ctx context.Context, name, input string, delivered func(), onLine func([]byte), args ...string) error {
		cancel()
		return ctx.Err()
	}
	converseOpencode("box", "/w")(ctx, "x", "", func() {}, func([]byte) {})
	if !strings.HasPrefix(stopped, "box /tmp/vibe-turn-") {
		t.Errorf("stopTurn: %q", stopped)
	}
}

func TestOpencodeHasAConversationButNoPickersYet(t *testing.T) {
	if !hasConversation(state.Instance{Agent: "opencode"}) {
		t.Error("OpenCode has no Conversation tab")
	}
	if conversationFor("/home", "box", state.Instance{Agent: "opencode"}, profileChoice{}) == nil {
		t.Error("OpenCode has no way to talk to")
	}
	if choiceFor("/home", "box", state.Instance{Agent: "opencode"}, profileChoice{}) != nil {
		t.Error("OpenCode is offered pickers that aren't built")
	}
}
