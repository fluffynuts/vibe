package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vibe/internal/companion"
	"vibe/internal/state"
)

func TestCursorCommandRunsCursorInTheWorkspaceWithoutATerminal(t *testing.T) {
	cmd := cursorCommand("/home/me/my project", "", "/tmp/t.pid", "")
	if cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != asAgentScript || cmd[3] != "converse" || len(cmd) != 5 {
		t.Fatalf("command = %q", cmd)
	}
	inner := cmd[4]
	if !strings.HasPrefix(inner, wrapTurn("", "/tmp/t.pid")+`cd '/home/me/my project' && `) {
		t.Errorf("inner = %s", inner)
	}
	for _, want := range []string{"'--trust'", "'--force'", "'-p'", "'stream-json'", `"$bin"`} {
		if !strings.Contains(inner, want) {
			t.Errorf("inner lacks %s: %s", want, inner)
		}
	}
	if strings.Contains(inner, "--resume") {
		t.Errorf("a first turn resumes nothing: %s", inner)
	}
}

func TestCursorCommandKeepsThePromptOffItsCommandLine(t *testing.T) {
	// The prompt goes in on stdin (see sbxrun.ExecStream): nothing of it
	// can be in the command, where it would be quoted wrongly, or seen in ps.
	if inner := cursorCommand("/w", "", "/tmp/t.pid", "")[4]; strings.Contains(inner, "prompt") {
		t.Errorf("inner = %s", inner)
	}
}

func TestCursorCommandFindsTheAgentUnderEitherNameItIsInstalledAs(t *testing.T) {
	inner := cursorCommand("/w", "", "/tmp/t.pid", "")[4]
	if !strings.Contains(inner, "bin=cursor-agent;") || !strings.Contains(inner, "|| bin=agent;") {
		t.Errorf("inner = %s", inner)
	}
}

func TestCursorCommandResumesASessionButNeverAnythingElse(t *testing.T) {
	if inner := cursorCommand("/w", "abc-123", "/tmp/t.pid", "")[4]; !strings.HasSuffix(inner, "--resume 'abc-123'") {
		t.Errorf("inner = %s", inner)
	}
	for _, bad := range []string{"x; rm -rf /", "a b", "--force x", strings.Repeat("a", 101)} {
		if inner := cursorCommand("/w", bad, "/tmp/t.pid", "")[4]; strings.Contains(inner, "--resume") {
			t.Errorf("resumed %q: %s", bad, inner)
		}
	}
}

func TestCursorCommandQuotesAWorkspaceThatTriesToBeShell(t *testing.T) {
	inner := cursorCommand("/x'; touch /pwned; '", "", "/tmp/t.pid", "")[4]
	if !strings.HasPrefix(inner, wrapTurn("", "/tmp/t.pid")+`cd '/x'\''; touch /pwned; '\''' && `) {
		t.Errorf("inner = %s", inner)
	}
}

// rebuiltBox is a sandbox whose Cursor has sessions, and takes back a kept
// copy of one when it can.
type rebuiltBox struct {
	sessions map[string]bool
	restores bool
}

func (b *rebuiltBox) transcripts() transcripts {
	return transcripts{
		lost: cursorSessionLost,
		has:  func(_ context.Context, s string) bool { return b.sessions[s] },
		fetch: func(_ context.Context, s string) ([]byte, error) {
			return []byte("tgz of " + s), nil
		},
		put: func(_ context.Context, _ []byte) error {
			if b.restores {
				b.sessions["sess-1"] = true
			}
			return nil
		},
	}
}

// resumedTurn runs a turn resuming sess-1, giving the lines the page got.
func resumedTurn(t *testing.T, c companion.Converser) []string {
	t.Helper()
	var lines []string
	if err := c(context.Background(), "hi", "sess-1", func() {}, func(l []byte) { lines = append(lines, string(l)) }); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestASessionCursorHasNoRecordOfIsSaidSoOnceNotSilentlyStartedAgain(t *testing.T) {
	// Asked to resume a session it hasn't, Cursor starts a new one and
	// answers as though nothing were wrong: only vibe can say so.
	file := filepath.Join(t.TempDir(), "box.transcript.tgz")
	kept := keptCopies(t)
	box := &rebuiltBox{sessions: map[string]bool{}}
	c := keepingTranscript(agentSaying("sess-1"), box.transcripts(), file)
	lines := resumedTurn(t, c)
	<-kept
	if len(lines) != 2 || !strings.Contains(lines[0], `"vibe_note"`) || !strings.Contains(lines[0], "no saved session for this conversation") {
		t.Fatalf("lines = %q", lines)
	}
	if again := resumedTurn(t, c); len(again) != 1 {
		t.Errorf("said again on the next turn: %q", again)
	}
	<-kept
}

func TestASessionPutBackFromTheKeptCopyIsNotSaidToBeLost(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.transcript.tgz")
	os.WriteFile(file, []byte("kept"), 0o600)
	kept := keptCopies(t)
	box := &rebuiltBox{sessions: map[string]bool{}, restores: true}
	lines := resumedTurn(t, keepingTranscript(agentSaying("sess-1"), box.transcripts(), file))
	<-kept
	if len(lines) != 1 {
		t.Errorf("lines = %q", lines)
	}
}

func TestClaudesSessionsAreNeverSaidToBeLost(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.transcript.tgz")
	kept := keptCopies(t)
	box := &rebuiltBox{sessions: map[string]bool{}}
	tr := box.transcripts()
	tr.lost = ""
	lines := resumedTurn(t, keepingTranscript(agentSaying("sess-1"), tr, file))
	<-kept
	if len(lines) != 1 {
		t.Errorf("lines = %q", lines)
	}
}

func TestOnlyAgentsThePageCanDriveHaveAConversation(t *testing.T) {
	home := t.TempDir()
	for agent, want := range map[string]bool{"": true, "claude": true, "cursor": true, "codex": false, "gemini": false, "shell": false} {
		got := conversationFor(home, "box", state.Instance{Agent: agent, Target: "/w"}, profileChoice{}) != nil
		if got != want {
			t.Errorf("agent %q: a conversation = %v, want %v", agent, got, want)
		}
	}
}

// turnsOf is a sandbox whose Cursor agent answers each run of a turn as the
// outcomes say, in order: an error is a run that failed with it on stderr.
// It records the commands it was run with.
func turnsOf(t *testing.T, outcomes ...error) (commands *[][]string) {
	t.Helper()
	commands = &[][]string{}
	was := streamTurn
	t.Cleanup(func() { streamTurn = was })
	turnPrompts = nil
	// Nothing was pasted, unless a test says so (see pasting, which goes after this).
	wasPasted := pastedFor
	t.Cleanup(func() { pastedFor = wasPasted })
	pastedFor = func(context.Context, string) (string, error) { return "", nil }
	streamTurn = func(_ context.Context, _, input string, _ func(), onLine func([]byte), args ...string) error {
		turnPrompts = append(turnPrompts, input)
		*commands = append(*commands, args)
		onLine([]byte(`{"type":"system","subtype":"init","session_id":"new-1","model":"x"}`))
		if len(*commands) > len(outcomes) {
			t.Fatalf("run %d: no more were expected", len(*commands))
		}
		return outcomes[len(*commands)-1]
	}
	return commands
}

// turnPrompts are what each run of the last turnsOf was given on its stdin.
var turnPrompts []string

const namedModelFailure = "exit status 1: ActionRequiredError: Named models unavailable Free plans can only use Auto. Switch to Auto or upgrade plans to continue."

func cursorTurn(t *testing.T) (lines []string, err error) {
	t.Helper()
	return cursorTurnOn(t, "")
}

// setBack is what a turn that fell back to Auto set the choice back with.
type setBack struct {
	calls int
	err   error
}

func (s *setBack) set() error {
	s.calls++
	return s.err
}

// cursorTurnOn is a turn for which model is the one chosen.
func cursorTurnOn(t *testing.T, model string) (lines []string, err error) {
	t.Helper()
	return cursorTurnSetting(t, model, &setBack{})
}

func cursorTurnSetting(t *testing.T, model string, back *setBack) (lines []string, err error) {
	t.Helper()
	return cursorTurnKnowing(t, model, back, newCursorPlan(filepath.Join(t.TempDir(), "plan.json")))
}

// cursorTurnKnowing is a turn that learns of the plan into plan.
func cursorTurnKnowing(t *testing.T, model string, back *setBack, plan *cursorPlan) (lines []string, err error) {
	t.Helper()
	box := converseCursor("box", "/w", newTurnLog(filepath.Join(t.TempDir(), "box.turns.log")), func() (string, error) { return model, nil }, back.set, plan)
	err = box(context.Background(), "hi", "sess-1", func() {}, func(l []byte) { lines = append(lines, string(l)) })
	return lines, err
}

func TestAModelAFreePlanCantRunIsSaidSoAndTheTurnRunAgainOnAuto(t *testing.T) {
	// Cursor remembers the model last asked for, so one picked in its
	// terminal is what every turn runs. A free plan only runs Auto, and
	// says so on stderr alone; with an API key (as a sandbox has) the plan
	// can't be read beforehand.
	commands := turnsOf(t, errors.New(namedModelFailure), nil)
	lines, err := cursorTurn(t)
	if err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	if len(*commands) != 2 {
		t.Fatalf("%d runs, want 2", len(*commands))
	}
	if first := (*commands)[0][4]; strings.Contains(first, "--model") {
		t.Errorf("the first run named a model: %s", first)
	}
	if second := (*commands)[1][4]; !strings.Contains(second, "--model 'auto'") || !strings.Contains(second, "--resume 'sess-1'") {
		t.Errorf("the second run: %s", second)
	}
	said := strings.Join(lines, "\n")
	if !strings.Contains(said, `"vibe_note"`) || !strings.Contains(said, "Auto") {
		t.Errorf("the user wasn't told: %q", lines)
	}
}

func TestOnlyThatFailureIsRunAgainAndOnlyOnce(t *testing.T) {
	other := errors.New("exit status 1: something else is wrong")
	for name, tc := range map[string]struct {
		outcomes []error
		runs     int
		fails    bool
	}{
		"another failure":     {[]error{other}, 1, true},
		"that failure twice":  {[]error{errors.New(namedModelFailure), errors.New(namedModelFailure)}, 2, true},
		"a turn that is fine": {[]error{nil}, 1, false},
	} {
		commands := turnsOf(t, tc.outcomes...)
		_, err := cursorTurn(t)
		if len(*commands) != tc.runs {
			t.Errorf("%s: %d runs, want %d", name, len(*commands), tc.runs)
		}
		if (err != nil) != tc.fails {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestATurnStoppedOnThePageIsNotRunAgain(t *testing.T) {
	commands := turnsOf(t, errors.New(namedModelFailure))
	stopTurnWas := stopTurn
	stopTurn = func(string, string) {}
	t.Cleanup(func() { stopTurn = stopTurnWas })
	ctx, stop := context.WithCancel(context.Background())
	stop()
	box := converseCursor("box", "/w", newTurnLog(filepath.Join(t.TempDir(), "box.turns.log")), func() (string, error) { return "", nil }, func() error { return nil }, newCursorPlan(filepath.Join(t.TempDir(), "plan.json")))
	box(ctx, "hi", "", func() {}, func([]byte) {})
	if len(*commands) != 1 {
		t.Errorf("%d runs after a stop", len(*commands))
	}
}

func TestBothAgentsTheTabCanDriveHaveAModelChoiceAndOthersHaveNone(t *testing.T) {
	home := t.TempDir()
	for agent, want := range map[string]bool{"": true, "claude": true, "cursor": true, "codex": false, "shell": false} {
		got := choiceFor(home, "box", state.Instance{Agent: agent, Target: "/w"}, profileChoice{}) != nil
		if got != want {
			t.Errorf("agent %q: a choice = %v, want %v", agent, got, want)
		}
	}
}

func TestAModelChosenIsNamedOnTheCommandOfTheTurn(t *testing.T) {
	commands := turnsOf(t, nil)
	if _, err := cursorTurnOn(t, "gpt-5.2-fast"); err != nil {
		t.Fatal(err)
	}
	if got := (*commands)[0][4]; !strings.Contains(got, "--model 'gpt-5.2-fast' '--trust'") {
		t.Errorf("command: %s", got)
	}
}

func TestATurnOnAutoThatFailsAnyWayIsNotRunAgainOnAuto(t *testing.T) {
	commands := turnsOf(t, errors.New(namedModelFailure))
	if _, err := cursorTurnOn(t, "auto"); err == nil {
		t.Error("the failure was swallowed")
	}
	if len(*commands) != 1 {
		t.Errorf("%d runs", len(*commands))
	}
}

func TestAChoiceThatCantBeReadIsAFailedTurnNothingWasRunFor(t *testing.T) {
	commands := turnsOf(t)
	box := converseCursor("box", "/w", newTurnLog(filepath.Join(t.TempDir(), "box.turns.log")), func() (string, error) { return "", errors.New("bad choice") }, func() error { return nil }, newCursorPlan(filepath.Join(t.TempDir(), "plan.json")))
	if err := box(context.Background(), "hi", "", func() {}, func([]byte) {}); err == nil || !strings.Contains(err.Error(), "bad choice") {
		t.Errorf("err = %v", err)
	}
	if len(*commands) != 0 {
		t.Errorf("ran %d", len(*commands))
	}
}

func TestAFallbackToAutoSetsTheChoiceBackToAutoAndSaysSo(t *testing.T) {
	// Otherwise the selector goes on showing a model that can't be had, and
	// every prompt is first refused, a few seconds lost, to get Auto anyway.
	turnsOf(t, errors.New(namedModelFailure), nil)
	back := &setBack{}
	lines, err := cursorTurnSetting(t, "gpt-5.2-fast", back)
	if err != nil {
		t.Fatal(err)
	}
	if back.calls != 1 {
		t.Errorf("the choice was set back %d times", back.calls)
	}
	said := strings.Join(lines, "\n")
	if !strings.Contains(said, `"refresh":"choice"`) {
		t.Errorf("the page isn't told to read the choice again: %q", lines)
	}
	if !strings.Contains(said, "set to Auto") || strings.Contains(said, "stays on Auto") {
		t.Errorf("what the user was told: %q", lines)
	}
}

func TestAChoiceThatCantBeSetBackIsSaidWhyAndTheTurnStillRuns(t *testing.T) {
	commands := turnsOf(t, errors.New(namedModelFailure), nil)
	lines, err := cursorTurnSetting(t, "gpt-5.2-fast", &setBack{err: errors.New("profile 'p' is vibe's own")})
	if err != nil || len(*commands) != 2 {
		t.Fatalf("err = %v, %d runs", err, len(*commands))
	}
	said := strings.Join(lines, "\n")
	if strings.Contains(said, `"refresh"`) {
		t.Errorf("the page is told to read a choice that didn't change: %q", lines)
	}
	if !strings.Contains(said, "couldn't be set to Auto") || !strings.Contains(said, "vibe's own") {
		t.Errorf("what the user was told: %q", lines)
	}
}

func TestTheChoiceIsLeftAloneWhenThereWasNoFallback(t *testing.T) {
	for name, outcomes := range map[string][]error{
		"a turn that is fine": {nil},
		"another failure":     {errors.New("exit status 1: something else is wrong")},
	} {
		turnsOf(t, outcomes...)
		back := &setBack{}
		cursorTurnSetting(t, "gpt-5.2-fast", back)
		if back.calls != 0 {
			t.Errorf("%s: the choice was set back", name)
		}
	}
	turnsOf(t, errors.New(namedModelFailure))
	back := &setBack{}
	cursorTurnSetting(t, "auto", back)
	if back.calls != 0 {
		t.Error("Auto failing was set back to Auto")
	}
}

func TestASandboxWithoutCursorsAgentSaysSoAndWhatToCheck(t *testing.T) {
	// "agent: command not found" says nothing of there having been two names
	// to look for, nor what to do.
	if runtime.GOOS == "windows" {
		t.Skip("the turn's command is for a Linux sandbox's shell, and symbolic links need privileges on Windows")
	}
	bin := t.TempDir()
	rm, err := exec.LookPath("rm")
	if err != nil {
		t.Skip("no rm to stand in for what the turn's wrapper needs")
	}
	if err := os.Symlink(rm, filepath.Join(bin, "rm")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", cursorCommand(t.TempDir(), "", filepath.Join(t.TempDir(), "t.pid"), "")[4])
	cmd.Env = []string{"PATH=" + bin}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 127 {
		t.Fatalf("err = %v, want exit status 127; output: %s", err, out)
	}
	for _, want := range []string{"cursor-agent", "agent", "isn't installed", "vibe --info"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the message lacks %q: %s", want, out)
		}
	}
	if strings.Contains(string(out), "command not found") {
		t.Errorf("the shell's own complaint is what's said: %s", out)
	}
}

func freshPlan(t *testing.T) *cursorPlan {
	t.Helper()
	return newCursorPlan(filepath.Join(t.TempDir(), "plan.json"))
}

func TestAModelRefusedTeachesVibeThatThePlanOnlyRunsAuto(t *testing.T) {
	// Cursor says so only in the refusal, and with an API key the plan can't be read.
	turnsOf(t, errors.New(namedModelFailure), nil)
	plan := freshPlan(t)
	if _, err := cursorTurnKnowing(t, "gpt-5.2-fast", &setBack{}, plan); err != nil {
		t.Fatal(err)
	}
	if _, only := plan.onlyAuto(); !only {
		t.Error("the refusal wasn't learned from")
	}
}

func TestARefusalOfWhateverCursorWasSetToIsLearnedFromToo(t *testing.T) {
	// No model chosen: it is Cursor's own, that the user's terminal set.
	turnsOf(t, errors.New(namedModelFailure), nil)
	plan := freshPlan(t)
	cursorTurnKnowing(t, "", &setBack{}, plan)
	if _, only := plan.onlyAuto(); !only {
		t.Error("not learned from")
	}
}

func TestAnotherFailureTeachesNothingOfThePlan(t *testing.T) {
	turnsOf(t, errors.New("exit status 1: the network is down"))
	plan := freshPlan(t)
	cursorTurnKnowing(t, "gpt-5.2-fast", &setBack{}, plan)
	if _, only := plan.onlyAuto(); only {
		t.Error("learned the plan from a failure that isn't about it")
	}
}

func TestAModelOtherThanAutoThatRunsShowsThePlanRunsMoreThanAuto(t *testing.T) {
	turnsOf(t, nil)
	plan := freshPlan(t)
	plan.learnedOnlyAuto()
	if _, err := cursorTurnKnowing(t, "gpt-5.2-fast", &setBack{}, plan); err != nil {
		t.Fatal(err)
	}
	if _, only := plan.onlyAuto(); only {
		t.Error("the limit was kept though a model other than Auto ran")
	}
}

func TestAutoRunningSaysNothingOfThePlanEitherWay(t *testing.T) {
	for name, model := range map[string]string{"auto chosen": "auto", "nothing chosen": ""} {
		turnsOf(t, nil)
		plan := freshPlan(t)
		plan.learnedOnlyAuto()
		cursorTurnKnowing(t, model, &setBack{}, plan)
		if _, only := plan.onlyAuto(); !only {
			t.Errorf("%s: the limit was forgotten because Auto ran, which every plan does", name)
		}
	}
}

func TestAPlanThatCantBeRememberedDoesNotStopTheTurn(t *testing.T) {
	turnsOf(t, errors.New(namedModelFailure), nil)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-folder")
	os.WriteFile(blocker, []byte("x"), 0o600)
	plan := newCursorPlan(filepath.Join(blocker, "plan.json"))
	if _, err := cursorTurnKnowing(t, "gpt-5.2-fast", &setBack{}, plan); err != nil {
		t.Errorf("the turn failed because the plan couldn't be kept: %v", err)
	}
}

// pasting stands in for the clipboard page: each turn is told text was pasted (or err), and it is
// counted how often that was asked.
func pasting(t *testing.T, text string, err error) (asked *int) {
	t.Helper()
	asked = new(int)
	was := pastedFor
	t.Cleanup(func() { pastedFor = was })
	pastedFor = func(context.Context, string) (string, error) {
		*asked++
		return text, err
	}
	return asked
}

func TestWhatWasPastedOnTheClipboardPageGoesInFrontOfThePrompt(t *testing.T) {
	// Cursor has no hook to hand it over, as claude has.
	turnsOf(t, nil)
	pasting(t, "The user sent this to you through the vibe clipboard page:\n- image at /x/1.png", nil)
	if _, err := cursorTurn(t); err != nil {
		t.Fatal(err)
	}
	want := "The user sent this to you through the vibe clipboard page:\n- image at /x/1.png\n\nhi"
	if len(turnPrompts) != 1 || turnPrompts[0] != want {
		t.Errorf("the prompt was %q", turnPrompts)
	}
}

func TestAPromptWithNothingPastedIsSentAsItWas(t *testing.T) {
	turnsOf(t, nil)
	pasting(t, "", nil)
	cursorTurn(t)
	if len(turnPrompts) != 1 || turnPrompts[0] != "hi" {
		t.Errorf("the prompt was %q", turnPrompts)
	}
}

func TestTheRunOnAutoIsGivenTheSamePasteWithoutClaimingAgain(t *testing.T) {
	// Claimed once handed over: a second claim would find nothing, and the run on Auto would lose them.
	turnsOf(t, errors.New(namedModelFailure), nil)
	asked := pasting(t, "PASTED", nil)
	if _, err := cursorTurn(t); err != nil {
		t.Fatal(err)
	}
	if *asked != 1 {
		t.Errorf("the clipboard was claimed %d times", *asked)
	}
	if len(turnPrompts) != 2 || turnPrompts[0] != turnPrompts[1] || !strings.HasPrefix(turnPrompts[1], "PASTED") {
		t.Errorf("the prompts were %q", turnPrompts)
	}
}

func TestAClipboardThatCantBeAskedForDoesNotStopTheTurn(t *testing.T) {
	turnsOf(t, nil)
	pasting(t, "", errors.New("the sandbox said no"))
	lines, err := cursorTurn(t)
	if err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	if len(turnPrompts) != 1 || turnPrompts[0] != "hi" {
		t.Errorf("the prompt was %q", turnPrompts)
	}
	_ = lines
}

func TestOnlyWhatTheHookPrintedBetweenTheMarkersIsWhatWasPasted(t *testing.T) {
	// A login shell may say something first.
	out := "Welcome to the sandbox\n" + pastedStart + "\nline one\nline two\n\n" + pastedEnd + "\n$ "
	if got := pastedIn(out); got != "line one\nline two" {
		t.Errorf("got %q", got)
	}
	for _, none := range []string{"", "no markers at all", pastedStart + "\n\n" + pastedEnd + "\n", pastedStart + "\nnever ends"} {
		if got := pastedIn(none); got != "" {
			t.Errorf("%q gave %q", none, got)
		}
	}
}

func TestTheClaimScriptRunsTheHooksTextModeAndSaysNothingWithoutOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script is for a Linux sandbox's shell")
	}
	home := t.TempDir()
	run := func() string {
		cmd := exec.Command("sh", "-c", claimPastedScript)
		cmd.Env = append(os.Environ(), "HOME="+home)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return pastedIn(string(out))
	}
	if got := run(); got != "" {
		t.Errorf("without a hook it gave %q", got)
	}
	bin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "clipboard-hook"), []byte("#!/bin/sh\n[ \"$1\" = --text ] && printf 'pasted: %s' \"$1\"\n"), 0o755)
	if got := run(); got != "pasted: --text" {
		t.Errorf("with a hook it gave %q", got)
	}
}

func TestTheUserIsToldWhatWasSentWithThePromptAndThatItWas(t *testing.T) {
	// The clipboard page marks an item "sent" too, but the conversation is where the user is looking.
	turnsOf(t, nil)
	pasting(t, "The user sent these to you through the vibe clipboard page:\n- image at /x/1.png (image/png, 1 KB) — look at it\n- text (/x/2.txt):\n```\nhello\n```", nil)
	lines, err := cursorTurn(t)
	if err != nil {
		t.Fatal(err)
	}
	said := strings.Join(lines, "\n")
	if !strings.Contains(said, `"vibe_note"`) || !strings.Contains(said, "2 items from the clipboard page") {
		t.Errorf("the user wasn't told: %q", lines)
	}
}

func TestOneItemIsSaidToBeOneItem(t *testing.T) {
	turnsOf(t, nil)
	pasting(t, "The user sent this to you through the vibe clipboard page:\n- image at /x/1.png (image/png, 1 KB) — look at it", nil)
	lines, _ := cursorTurn(t)
	if said := strings.Join(lines, "\n"); !strings.Contains(said, "1 item from the clipboard page") {
		t.Errorf("said %q", lines)
	}
}

func TestNothingIsSaidOfTheClipboardWhenNothingWasPasted(t *testing.T) {
	turnsOf(t, nil)
	pasting(t, "", nil)
	lines, _ := cursorTurn(t)
	if strings.Contains(strings.Join(lines, "\n"), "clipboard") {
		t.Errorf("said %q", lines)
	}
}

func TestWhatWasClaimedOrWhyNotIsInTheTurnLogForWhenSomethingGoesWrong(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		err  error
		want string
	}{
		"items":   {"The user sent this to you:\n- image at /x/1.png", nil, "clipboard: 1 item"},
		"none":    {"", nil, "clipboard: nothing pasted"},
		"failure": {"", errors.New("the sandbox said no"), "clipboard: couldn't be asked (the sandbox said no)"},
	} {
		turnsOf(t, nil)
		pasting(t, tc.text, tc.err)
		file := filepath.Join(t.TempDir(), "box.turns.log")
		box := converseCursor("box", "/w", newTurnLog(file), func() (string, error) { return "", nil }, func() error { return nil }, newCursorPlan(filepath.Join(t.TempDir(), "plan.json")))
		box(context.Background(), "hi", "", func() {}, func([]byte) {})
		if got := loggedTurns(t, file); !strings.Contains(got, tc.want) {
			t.Errorf("%s: the log lacks %q:\n%s", name, tc.want, got)
		}
	}
}

func TestOnlyTheItemsTheHookListsAreCountedNotLinesOfTheTextPastedInThem(t *testing.T) {
	text := "The user sent these to you through the vibe clipboard page:\n" +
		"- image at /x/1.png (image/png, 1 KB) — look at it\n" +
		"- text (/x/2.txt):\n````\n- image at /not/an/item\n```\n- file at /not/either\n````\n" +
		"- file at /x/3.pdf (application/pdf, 2 KB) — read it"
	if got := itemsIn(text); got != 3 {
		t.Errorf("counted %d, want 3", got)
	}
}
