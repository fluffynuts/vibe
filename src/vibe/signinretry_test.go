package main

import (
	"context"
	"errors"
	"testing"

	"vibe/internal/cliargs"
)

func parsed(t *testing.T, argv ...string) cliargs.Args {
	t.Helper()
	args, err := cliargs.Parse(argv)
	if err != nil {
		t.Fatal(err)
	}
	return args
}

// What a read-only command like --list fails with when sbx is signed out: it is sbx's own words, passed on.
var signedOutFailure = errors.New("sbx ls --json: exit status 1: error: not signed in to Docker\n  try: sbx login")

func signInStepsFor(offered *int, signedInAfter bool) sbxSignInSteps {
	return sbxSignInSteps{
		choose: func(string, []string, int) (int, bool) { *offered++; return 0, true },
		login:  func() error { return nil },
		recheck: func(context.Context) (string, error) {
			if signedInAfter {
				return "[]", nil
			}
			return "error: not signed in to Docker", errors.New("exit 1")
		},
		say: func(string) {},
	}
}

func TestAListThatFailedForWantOfASignInOffersOneAndIsAskedAgainOnceItWorked(t *testing.T) {
	offered := 0
	if !retryAfterSignIn(signedOutFailure, parsed(t, "-l"), true, signInStepsFor(&offered, true)) || offered != 1 {
		t.Errorf("offered %d times, and the command wasn't asked to run again", offered)
	}
}

func TestAListIsNotRunAgainWhenTheSignInDidNotTake(t *testing.T) {
	offered := 0
	if retryAfterSignIn(signedOutFailure, parsed(t, "-l"), true, signInStepsFor(&offered, false)) {
		t.Error("ran again although sbx is still signed out")
	}
}

func TestAFailureForAnotherReasonIsLeftAlone(t *testing.T) {
	offered := 0
	if retryAfterSignIn(errors.New("sbx ls --json: exit status 1: error: the daemon is not running"), parsed(t, "-l"), true, signInStepsFor(&offered, true)) || offered != 0 {
		t.Errorf("offered %d times for a failure that isn't a sign-in", offered)
	}
}

func TestNobodyIsOfferedASignInWithNoTerminalOrWithForce(t *testing.T) {
	offered := 0
	if retryAfterSignIn(signedOutFailure, parsed(t, "-l"), false, signInStepsFor(&offered, true)) || offered != 0 {
		t.Error("offered with no terminal")
	}
	if retryAfterSignIn(signedOutFailure, parsed(t, "-l", "-f"), true, signInStepsFor(&offered, true)) || offered != 0 {
		t.Error("offered with -f")
	}
}

// Running a command again is safe only for one that changes nothing.
func TestOnlyACommandThatChangesNothingIsRunAgain(t *testing.T) {
	offered := 0
	if retryAfterSignIn(signedOutFailure, parsed(t, "--stop-all"), true, signInStepsFor(&offered, true)) || offered != 0 {
		t.Errorf("offered %d times for a command that acts on sandboxes", offered)
	}
}
