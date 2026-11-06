package sbxrun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vibe/internal/sbxtest"
)

// What sbx's exec prints for a stopped sandbox when sbx is signed out: it starts the device flow and waits for it.
const signedOutExec = "echo 'not signed in to Docker; starting the sign-in flow…'\n" +
	"echo 'Your one-time device confirmation code is: FCXR-MHDV'\n" +
	"echo 'Open this URL to sign in: https://login.docker.com/activate?user_code=FCXR-MHDV'\n" +
	"echo 'Waiting for authentication...'\n" +
	"exec sleep 60\n"

func finishesWithin(t *testing.T, limit time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("%s was still waiting after %s", what, limit)
	}
}

func TestAStartThatMeetsTheSignInFlowSaysSbxIsSignedOutAtOnce(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec)\n"+signedOutExec+"  ;;\nesac\n")
	var err error
	finishesWithin(t, 20*time.Second, "the start", func() { err = StartChecked(context.Background(), "box") })
	var signedOut *NotSignedInError
	if !errors.As(err, &signedOut) {
		t.Fatalf("err = %v, want one saying sbx is signed out", err)
	}
}

// What a wake prints is what the command run in the sandbox printed, and that is the agent's to make: a script in place of
// /bin/true could start a Docker device login of its own and print the address and code, for a user who follows them to
// authorise it. So the address is a sign of being signed out, and never something to hand to the user.
func TestTheSignInAddressASandboxPrintsIsNeverGivenToTheUser(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec)\n"+signedOutExec+"  ;;\nesac\n")
	err := StartChecked(context.Background(), "box")
	var signedOut *NotSignedInError
	if !errors.As(err, &signedOut) {
		t.Fatalf("err = %v", err)
	}
	if signedOut.URL != "" || signedOut.Code != "" || signedOut.Hint() != "" {
		t.Errorf("the sandbox's address and code were taken: %q, %q, %q", signedOut.URL, signedOut.Code, signedOut.Hint())
	}
	for _, leaked := range []string{"FCXR-MHDV", "login.docker.com"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("the message carries %q, which the sandbox printed: %v", leaked, err)
		}
	}
}

func TestASandboxIsNotReachableWhileSbxWaitsForASignIn(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec)\n"+signedOutExec+"  ;;\nesac\n")
	var reachable bool
	finishesWithin(t, 20*time.Second, "the probe", func() { reachable = Reachable(context.Background(), "box") })
	if reachable {
		t.Error("reachable")
	}
}

func TestAnExecThatSaysNothingAboutSigningInIsLeftToFinish(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec) sleep 1; echo 'https://www.docker.com/legal/'; exit 0 ;;\nesac\n")
	if !Reachable(context.Background(), "box") {
		t.Error("not reachable")
	}
}

func TestAWakeThatMeetsTheSignInFlowIsLoggedWithWhatSbxSaid(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec)\n"+signedOutExec+"  ;;\nesac\n")
	var logged []string
	old := WakeLog
	WakeLog = func(entry string) { logged = append(logged, entry) }
	defer func() { WakeLog = old }()
	finishesWithin(t, 20*time.Second, "the start", func() { StartChecked(context.Background(), "box") })
	if len(logged) == 0 || !strings.Contains(logged[0], "FCXR-MHDV") || !strings.Contains(logged[0], "box") {
		t.Errorf("logged = %q", logged)
	}
}

func TestAQuickWakeThatWorksIsNotLogged(t *testing.T) {
	sbxtest.OnPath(t, "exit 0\n")
	var logged []string
	old := WakeLog
	WakeLog = func(entry string) { logged = append(logged, entry) }
	defer func() { WakeLog = old }()
	if !Reachable(context.Background(), "box") || len(logged) != 0 {
		t.Errorf("logged = %q", logged)
	}
}

// What sbx's exec does with no terminal to answer on, as seen on a real machine: it fails at once (exit 125), saying the
// command that puts it right, and `sbx start` isn't a command at all.
const signedOutNoTerminal = "case \"$1\" in\n" +
	"  exec) echo 'error: not signed in to Docker' >&2; echo '  try: sbx login' >&2; exit 125 ;;\n" +
	"  *) echo \"error: sbx: unknown command: sbx $1\" >&2; exit 1 ;;\n" +
	"esac\n"

func TestAStartThatSbxRefusesForWantOfASignInSaysSoNotThatStartIsUnknown(t *testing.T) {
	sbxtest.OnPath(t, signedOutNoTerminal)
	err := StartChecked(context.Background(), "box")
	var signedOut *NotSignedInError
	if !errors.As(err, &signedOut) {
		t.Fatalf("err = %v, want one saying sbx is signed out", err)
	}
	if strings.Contains(err.Error(), "unknown command") {
		t.Errorf("the real reason is hidden by what sbx start said: %v", err)
	}
}

// Whatever the words, it is the command it names that is looked for: they are sbx's own to change or translate.
func TestAnExecThatPointsToSbxLoginIsTakenForSignedOutInAnyLanguage(t *testing.T) {
	sbxtest.OnPath(t, "echo 'erreur : pas connecté à Docker' >&2; echo '  essayez : sbx login' >&2; exit 125\n")
	var signedOut *NotSignedInError
	if err := StartChecked(context.Background(), "box"); !errors.As(err, &signedOut) {
		t.Errorf("err = %v", err)
	}
}

func TestAnExecThatFailsForAnotherReasonIsNotTakenForSignedOut(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec) echo 'error: no such sandbox' >&2; exit 125 ;;\n  *) echo 'error: unknown command' >&2; exit 1 ;;\nesac\n")
	err := StartChecked(context.Background(), "box")
	var signedOut *NotSignedInError
	if err == nil || errors.As(err, &signedOut) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "no such sandbox") {
		t.Errorf("what exec said is lost: %v", err)
	}
}

// Output the agent makes is the host's to bear: a flood is stopped at a cap rather than read, and scanned, until the probe's
// own limit (two minutes), and a wake that printed endlessly is a failed wake.
func TestAWakeThatPrintsWithoutEndIsStoppedAtACap(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec) exec yes 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' ;;\nesac\n")
	var err error
	finishesWithin(t, 20*time.Second, "the wake", func() { err = wake(context.Background(), "box") })
	if err == nil {
		t.Fatal("a wake that printed endlessly worked")
	}
	if len(err.Error()) > 1000 {
		t.Errorf("the message is %d bytes", len(err.Error()))
	}
}

// Polling for a sandbox to come up asks once a second; sbx being signed out won't change by asking again.
func TestWaitingForASandboxStopsAtOnceWhenSbxIsSignedOut(t *testing.T) {
	sbxtest.OnPath(t, signedOutNoTerminal)
	var logged []string
	old := WakeLog
	WakeLog = func(entry string) { logged = append(logged, entry) }
	defer func() { WakeLog = old }()
	var reachable bool
	began := time.Now()
	finishesWithin(t, 20*time.Second, "the wait", func() { reachable = WaitReachable(context.Background(), "box", 30) })
	if reachable {
		t.Error("reachable")
	}
	if took := time.Since(began); took > 8*time.Second {
		t.Errorf("waited %s for something that won't change", took)
	}
	if len(logged) != 1 {
		t.Errorf("%d notes of the same failure: %q", len(logged), logged)
	}
}

// One note for a run of failed probes at the start and one at the end, not one a second: a boot that takes minutes would
// otherwise fill the log, and push out what it is kept for.
func TestAWaitThatFailsNotesItsFirstAndLastFailureOnly(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec) echo 'still starting' >&2; exit 1 ;;\nesac\n")
	var logged []string
	old := WakeLog
	WakeLog = func(entry string) { logged = append(logged, entry) }
	defer func() { WakeLog = old }()
	WaitReachable(context.Background(), "box", 4)
	if len(logged) != 2 {
		t.Errorf("%d notes for 4 failed probes, want the first and the last: %q", len(logged), logged)
	}
}

// What a wake prints is the agent's to make, and it is shown in a terminal (the log, cat) and on a page.
func TestWhatAWakePrintedIsKeptWithoutControlCharactersAndShownShort(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n  exec)\n"+
		"    printf 'before \\033[31mred\\033[0m \\033]0;title\\007after\\n' >&2\n"+
		"    head -c 20000 /dev/zero | tr '\\0' a >&2; echo >&2\n"+
		"    exit 1 ;;\n  *) exit 1 ;;\nesac\n")
	var logged []string
	old := WakeLog
	WakeLog = func(entry string) { logged = append(logged, entry) }
	defer func() { WakeLog = old }()
	err := wake(context.Background(), "box")
	if err == nil {
		t.Fatal("worked")
	}
	Reachable(context.Background(), "box") // which notes a failure
	for _, text := range append(logged, err.Error()) {
		if strings.ContainsAny(text, "\x1b\x07") {
			t.Errorf("a control character got through: %q", text[:min(len(text), 120)])
		}
	}
	if !strings.Contains(strings.Join(logged, ""), "before") || !strings.Contains(strings.Join(logged, ""), "after") {
		t.Errorf("the words were lost with the escapes: %q", logged)
	}
	if len(err.Error()) > 1000 {
		t.Errorf("the message is %d bytes: it goes on a page", len(err.Error()))
	}
}

// After sbx start has been accepted, a sandbox that never answers says what the last try said: that is what explains it.
func TestASandboxThatNeverAnswersAfterAStartSaysWhatItLastSaid(t *testing.T) {
	old := startTimeout
	startTimeout = 2
	defer func() { startTimeout = old }()
	sbxtest.OnPath(t, "case \"$1\" in\n  start) exit 0 ;;\n  exec) echo 'the disk is full' >&2; exit 1 ;;\nesac\n")
	err := StartChecked(context.Background(), "box")
	if err == nil || !strings.Contains(err.Error(), "the disk is full") || !strings.Contains(err.Error(), "didn't accept a command") {
		t.Errorf("err = %v", err)
	}
}
