package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSbxSignIn struct {
	after    string // what sbx says after the sign-in
	picked   bool
	pick     int
	loginErr error
	logins   int
	asked    []string
	messages []string
}

func (f *fakeSbxSignIn) run() bool {
	return offerSbxSignIn(context.Background(), sbxSignInSteps{
		choose: func(question string, labels []string, def int) (int, bool) {
			f.asked = append(f.asked, question+"\n"+strings.Join(labels, "\n"))
			return f.pick, f.picked
		},
		login: func() error { f.logins++; return f.loginErr },
		recheck: func(context.Context) (string, error) {
			if f.after == "[]" {
				return f.after, nil
			}
			return f.after, errors.New("exit 1")
		},
		say: func(line string) { f.messages = append(f.messages, line) },
	})
}

func (f *fakeSbxSignIn) said() string { return strings.Join(f.messages, "\n") }

func TestSigningInIsOfferedAndGivenOnYes(t *testing.T) {
	f := &fakeSbxSignIn{after: "[]", picked: true, pick: 0}
	if !f.run() || f.logins != 1 {
		t.Fatalf("logins = %d", f.logins)
	}
	if !strings.Contains(f.said(), "signed in") {
		t.Errorf("it doesn't say it worked: %q", f.said())
	}
	q := f.asked[0]
	for _, want := range []string{"Docker", "Sign in now"} {
		if !strings.Contains(q, want) {
			t.Errorf("the question lacks %q:\n%s", want, q)
		}
	}
}

func TestDecliningLeavesSbxSignedOutAndSaysHowToByHand(t *testing.T) {
	f := &fakeSbxSignIn{picked: true, pick: 1}
	if f.run() || f.logins != 0 || !strings.Contains(f.said(), "sbx login") {
		t.Errorf("logins %d, said %q", f.logins, f.said())
	}
}

func TestNoAnswerAtAllIsTakenAsDeclining(t *testing.T) {
	f := &fakeSbxSignIn{picked: false}
	if f.run() || f.logins != 0 || !strings.Contains(f.said(), "sbx login") {
		t.Errorf("logins %d, said %q", f.logins, f.said())
	}
}

func TestASignInThatFailsIsAWarningWithTheReasonAndHowToByHand(t *testing.T) {
	f := &fakeSbxSignIn{picked: true, loginErr: errors.New("exit status 1")}
	if f.run() || !strings.Contains(f.said(), "WARNING") || !strings.Contains(f.said(), "exit status 1") || !strings.Contains(f.said(), "sbx login") {
		t.Errorf("said %q", f.said())
	}
}

func TestASignInThatSbxStillDoesNotAcceptIsSaidAndNotCountedAsDone(t *testing.T) {
	f := &fakeSbxSignIn{after: "error: not signed in to Docker", picked: true}
	if f.run() || !strings.Contains(f.said(), "WARNING") {
		t.Errorf("it called a sign-in that didn't take a success: %q", f.said())
	}
}
