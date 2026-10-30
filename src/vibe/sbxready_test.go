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
	looks []string
	// policyLooks are the answers to `sbx policy ls`, one per look (the last repeated; none means sbx has its policy).
	policyLooks []string
	policy      bool // whether giving the policy works
	signIn      bool
	did         []string
}

func (r *readiness) run() {
	i, j := 0, 0
	sbxReadySteps{
		probe: func(context.Context) (string, error) {
			out := r.looks[min(i, len(r.looks)-1)]
			i++
			if out == "[]" {
				return out, nil
			}
			return out, errors.New("exit 1")
		},
		policyProbe: func(context.Context) (string, error) {
			if len(r.policyLooks) == 0 {
				return "POLICY SOURCE", nil
			}
			out := r.policyLooks[min(j, len(r.policyLooks)-1)]
			j++
			if out == "POLICY SOURCE" {
				return out, nil
			}
			return out, errors.New("exit 1")
		},
		policy: func(context.Context) bool { r.did = append(r.did, "policy"); return r.policy },
		signIn: func(context.Context) bool { r.did = append(r.did, "sign-in"); return r.signIn },
	}.run(context.Background())
}

func (r *readiness) done() string { return strings.Join(r.did, ",") }

// sbx doesn't ask for its policy of `sbx ls` once signed in: it does of `sbx policy ls`, which is why that is looked at too.
func TestAnSbxSignedInButWaitingForAPolicyIsGivenOne(t *testing.T) {
	r := &readiness{looks: []string{"[]"}, policyLooks: []string{policyMenu, "POLICY SOURCE"}, policy: true}
	r.run()
	if r.done() != "policy" {
		t.Errorf("did %q", r.done())
	}
}

func TestAnSbxThatAsksForAPolicyOfSbxLsItselfIsGivenOneToo(t *testing.T) {
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

// A new machine: signed out, then (once signed in) waiting for the policy: the order the real VM showed.
func TestASignInIsFollowedByThePolicyItLeadsTo(t *testing.T) {
	r := &readiness{looks: []string{signedOutText, "[]"}, policyLooks: []string{policyMenu, "POLICY SOURCE"}, policy: true, signIn: true}
	r.run()
	if r.done() != "sign-in,policy" {
		t.Errorf("did %q", r.done())
	}
}

func TestDecliningOrFailingEndsItAndDoesNotAskAgain(t *testing.T) {
	r := &readiness{looks: []string{signedOutText}, signIn: false}
	r.run()
	if r.done() != "sign-in" {
		t.Errorf("did %q", r.done())
	}
	r = &readiness{looks: []string{"[]"}, policyLooks: []string{policyMenu}, policy: false}
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
	r = &readiness{looks: []string{"[]"}, policyLooks: []string{"error: the daemon fell over"}, policy: true, signIn: true}
	r.run()
	if r.done() != "" {
		t.Errorf("a policy listing that failed otherwise: did %q", r.done())
	}
}

func TestAnSbxWithEverythingInPlaceIsLookedAtTwiceAndNothingIsDone(t *testing.T) {
	looks, policyLooks := 0, 0
	sbxReadySteps{
		probe:       func(context.Context) (string, error) { looks++; return "[]", nil },
		policyProbe: func(context.Context) (string, error) { policyLooks++; return "POLICY SOURCE", nil },
		policy:      func(context.Context) bool { t.Error("policy"); return true },
		signIn:      func(context.Context) bool { t.Error("sign-in"); return true },
	}.run(context.Background())
	if looks != 1 || policyLooks != 1 {
		t.Errorf("looks %d, policy looks %d", looks, policyLooks)
	}
}
