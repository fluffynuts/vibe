package sbxrun

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"vibe/internal/sbxtest"
)

// The last-ditch look: a wake failed for a reason nothing recognised (sbx may have reworded what it says), so `sbx login` is
// asked, which is clear either way: signed out it prints the flow's address, and signed in it finishes without one.
func resetSignInCheck(t *testing.T) {
	t.Helper()
	signInCheckMu.Lock()
	lastSignInCheck = time.Time{}
	signInCheckMu.Unlock()
}

const unrecognisedWakeFailure = "case \"$1\" in\n  exec) echo 'quelque chose a mal tourné' >&2; exit 125 ;;\n"

func TestAnUnrecognisedFailureIsFoundToBeSignedOutWhenSbxLoginStartsItsFlow(t *testing.T) {
	resetSignInCheck(t)
	sbxtest.OnPath(t, unrecognisedWakeFailure+
		"  login)\n    echo 'Open: https://login.docker.com/activate?user_code=WXYZ-1234'\n    exec sleep 60 ;;\n"+
		"  *) echo 'unknown command' >&2; exit 1 ;;\nesac\n")
	var err error
	finishesWithin(t, 20*time.Second, "the start", func() { err = StartChecked(context.Background(), "box") })
	var signedOut *NotSignedInError
	if !errors.As(err, &signedOut) || signedOut.Code != "WXYZ-1234" {
		t.Errorf("err = %v, want signed out with the flow's code", err)
	}
}

func TestAnUnrecognisedFailureSaysSbxIsSignedInWhenSbxLoginSaysNothingNeedsDoing(t *testing.T) {
	resetSignInCheck(t)
	sbxtest.OnPath(t, unrecognisedWakeFailure+
		"  login) echo 'You are signed in [username: someone]'; exit 0 ;;\n"+
		"  *) echo 'unknown command' >&2; exit 1 ;;\nesac\n")
	err := StartChecked(context.Background(), "box")
	var signedOut *NotSignedInError
	if err == nil || errors.As(err, &signedOut) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "quelque chose a mal tourné") || !strings.Contains(err.Error(), "isn't the cause") {
		t.Errorf("err = %v, want the reason and that sign-in isn't it", err)
	}
}

func TestSbxLoginIsNotAskedMoreThanOnceInAWhile(t *testing.T) {
	resetSignInCheck(t)
	logins := t.TempDir() + "/logins"
	sbxtest.OnPath(t, unrecognisedWakeFailure+
		"  login) echo x >>"+logins+"; exit 0 ;;\n"+
		"  *) echo 'unknown command' >&2; exit 1 ;;\nesac\n")
	StartChecked(context.Background(), "box")
	StartChecked(context.Background(), "box")
	got := readLines(t, logins)
	if got != 1 {
		t.Errorf("sbx login ran %d times", got)
	}
}

func TestSbxLoginIsNotAskedWhenTheFailureWasRecognised(t *testing.T) {
	resetSignInCheck(t)
	logins := t.TempDir() + "/logins"
	sbxtest.OnPath(t, "case \"$1\" in\n  exec) echo 'try: sbx login' >&2; exit 125 ;;\n  login) echo x >>"+logins+" ;;\nesac\n")
	StartChecked(context.Background(), "box")
	if got := readLines(t, logins); got != 0 {
		t.Errorf("sbx login ran %d times for a failure that said so", got)
	}
}

func readLines(t *testing.T, file string) int {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(data)))
}
