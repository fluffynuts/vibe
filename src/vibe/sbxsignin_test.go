package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSbxSignIn struct {
	probes   []string // what each look at sbx finds, in turn; the last is repeated
	picked   bool
	pick     int
	loginErr error
	logins   int
	asked    []string
	messages []string
}

func (f *fakeSbxSignIn) run() {
	call := 0
	setUpSbxSignInWith(context.Background(), sbxSignInSteps{
		probe: func(context.Context) (string, error) {
			out := f.probes[min(call, len(f.probes)-1)]
			call++
			if out == "[]" {
				return out, nil
			}
			return out, errors.New("exit 1")
		},
		choose: func(question string, labels []string, def int) (int, bool) {
			f.asked = append(f.asked, question+"\n"+strings.Join(labels, "\n"))
			return f.pick, f.picked
		},
		login: func() error { f.logins++; return f.loginErr },
		say:   func(line string) { f.messages = append(f.messages, line) },
	})
}

func (f *fakeSbxSignIn) said() string { return strings.Join(f.messages, "\n") }

const signedOutText = "error: not signed in to Docker\n  try: sbx login"

func TestNothingIsAskedOfAnSbxThatIsSignedIn(t *testing.T) {
	f := &fakeSbxSignIn{probes: []string{"[]"}, picked: true}
	f.run()
	if len(f.asked) != 0 || f.logins != 0 {
		t.Errorf("asked %v, logins %d", f.asked, f.logins)
	}
}

func TestNothingIsAskedWhenSbxIsWaitingOnSomethingElse(t *testing.T) {
	f := &fakeSbxSignIn{probes: []string{policyMenu}, picked: true}
	f.run()
	if len(f.asked) != 0 || f.logins != 0 {
		t.Errorf("asked %v, logins %d", f.asked, f.logins)
	}
}

func TestAnSbxThatIsSignedOutIsOfferedASignInAndGivenOneOnYes(t *testing.T) {
	f := &fakeSbxSignIn{probes: []string{signedOutText, "[]"}, picked: true, pick: 0}
	f.run()
	if f.logins != 1 {
		t.Fatalf("logins = %d", f.logins)
	}
	if !strings.Contains(f.said(), "signed in") {
		t.Errorf("it doesn't say it worked: %q", f.said())
	}
	q := f.asked[0]
	for _, want := range []string{"Docker", "sbx login"} {
		if !strings.Contains(q, want) {
			t.Errorf("the question lacks %q:\n%s", want, q)
		}
	}
}

func TestDecliningLeavesSbxSignedOutAndSaysHowToByHand(t *testing.T) {
	f := &fakeSbxSignIn{probes: []string{signedOutText}, picked: true, pick: 1}
	f.run()
	if f.logins != 0 || !strings.Contains(f.said(), "sbx login") {
		t.Errorf("logins %d, said %q", f.logins, f.said())
	}
}

func TestNoAnswerAtAllIsTakenAsDeclining(t *testing.T) {
	f := &fakeSbxSignIn{probes: []string{signedOutText}, picked: false}
	f.run()
	if f.logins != 0 || !strings.Contains(f.said(), "sbx login") {
		t.Errorf("logins %d, said %q", f.logins, f.said())
	}
}

func TestASignInThatFailsIsAWarningWithTheReasonAndHowToByHand(t *testing.T) {
	f := &fakeSbxSignIn{probes: []string{signedOutText}, picked: true, loginErr: errors.New("exit status 1")}
	f.run()
	if !strings.Contains(f.said(), "WARNING") || !strings.Contains(f.said(), "exit status 1") || !strings.Contains(f.said(), "sbx login") {
		t.Errorf("said %q", f.said())
	}
}

func TestASignInThatSbxStillDoesNotAcceptIsSaid(t *testing.T) {
	f := &fakeSbxSignIn{probes: []string{signedOutText}, picked: true}
	f.run()
	if !strings.Contains(f.said(), "WARNING") {
		t.Errorf("it called a sign-in that didn't take a success: %q", f.said())
	}
}

// sbx asks for its network policy before anything, and may ask after a sign-in what it asked before: setting up looks at
// the policy again afterwards, so that the order sbx wants doesn't matter.
func TestSettingUpSbxLooksAtThePolicyAgainAfterSigningIn(t *testing.T) {
	var order []string
	steps := setUpSbxSteps{
		policy: func() { order = append(order, "policy") },
		signIn: func() { order = append(order, "sign-in") },
	}
	steps.run()
	if strings.Join(order, ",") != "policy,sign-in,policy" {
		t.Errorf("order = %v", order)
	}
}
