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
	if signedOut.URL != "https://login.docker.com/activate?user_code=FCXR-MHDV" || signedOut.Code != "FCXR-MHDV" {
		t.Errorf("link = %q, %q", signedOut.URL, signedOut.Code)
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
