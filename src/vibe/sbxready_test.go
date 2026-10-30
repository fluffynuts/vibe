package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const signedOutText = "error: not signed in to Docker\n  try: sbx login"

// readiness is a sbx whose answers to `sbx ls` are the list, one per look (the last repeated), and what is done about
// what it wants, in order.
type readiness struct {
	looks  []string
	policy bool // whether giving the policy works
	signIn bool
	did    []string
}

func (r *readiness) run() {
	i := 0
	sbxReadySteps{
		probe: func(context.Context) (string, error) {
			out := r.looks[min(i, len(r.looks)-1)]
			i++
			if out == "[]" {
				return out, nil
			}
			return out, errors.New("exit 1")
		},
		policy: func(context.Context) bool { r.did = append(r.did, "policy"); return r.policy },
		signIn: func(context.Context) bool { r.did = append(r.did, "sign-in"); return r.signIn },
	}.run(context.Background())
}

func (r *readiness) done() string { return strings.Join(r.did, ",") }

func TestAnSbxThatIsReadyIsLookedAtOnceAndNothingIsDone(t *testing.T) {
	r := &readiness{looks: []string{"[]"}, policy: true, signIn: true}
	looks := 0
	steps := sbxReadySteps{
		probe:  func(context.Context) (string, error) { looks++; return "[]", nil },
		policy: func(context.Context) bool { t.Error("policy"); return true },
		signIn: func(context.Context) bool { t.Error("sign-in"); return true },
	}
	steps.run(context.Background())
	if looks != 1 || r.done() != "" {
		t.Errorf("looks = %d", looks)
	}
}

func TestAnSbxWaitingForAPolicyIsGivenOne(t *testing.T) {
	r := &readiness{looks: []string{policyMenu, "[]"}, policy: true}
	r.run()
	if r.done() != "policy" {
		t.Errorf("did %q", r.done())
	}
}

func TestAnSbxSignedOutIsOfferedASignIn(t *testing.T) {
	r := &readiness{looks: []string{signedOutText, "[]"}, signIn: true}
	r.run()
	if r.done() != "sign-in" {
		t.Errorf("did %q", r.done())
	}
}

// sbx asks for its policy before anything, and may ask after a sign-in what it asked before: whichever it wants next is given.
func TestWhatSbxWantsNextAfterTheFirstIsGivenToo(t *testing.T) {
	r := &readiness{looks: []string{policyMenu, signedOutText, policyMenu, "[]"}, policy: true, signIn: true}
	r.run()
	if r.done() != "policy,sign-in,policy" {
		t.Errorf("did %q", r.done())
	}
}

func TestDecliningOrFailingEndsItAndDoesNotAskAgain(t *testing.T) {
	r := &readiness{looks: []string{signedOutText}, signIn: false}
	r.run()
	if r.done() != "sign-in" {
		t.Errorf("did %q", r.done())
	}
	r = &readiness{looks: []string{policyMenu}, policy: false}
	r.run()
	if r.done() != "policy" {
		t.Errorf("did %q", r.done())
	}
}

func TestAnSbxThatKeepsAskingIsAskedAboutNoMoreThanThreeTimes(t *testing.T) {
	r := &readiness{looks: []string{signedOutText}, signIn: true}
	r.run()
	if r.done() != "sign-in,sign-in,sign-in" {
		t.Errorf("did %q", r.done())
	}
}

func TestAnSbxThatSaysSomethingElseIsLeftAlone(t *testing.T) {
	r := &readiness{looks: []string{"error: the daemon fell over"}, policy: true, signIn: true}
	r.run()
	if r.done() != "" {
		t.Errorf("did %q", r.done())
	}
}
