package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const policyMenu = "Choose the network policy for sandboxes\n  Choice [1-3, default 2]:\n  error: operation cancelled by user"

type fakePolicyStep struct {
	pick     int
	picked   bool
	initOut  string
	initErr  error
	asked    []string
	inits    []string
	messages []string
}

func (f *fakePolicyStep) run() bool {
	return askNetworkPolicy(context.Background(), networkPolicySteps{
		choose: func(question string, labels []string, def int) (int, bool) {
			f.asked = append(f.asked, question+"\n"+strings.Join(labels, "\n"))
			return f.pick, f.picked
		},
		initPolicy: func(_ context.Context, choice string) (string, error) {
			f.inits = append(f.inits, choice)
			return f.initOut, f.initErr
		},
		say: func(line string) { f.messages = append(f.messages, line) },
	})
}

func (f *fakePolicyStep) said() string { return strings.Join(f.messages, "\n") }

func TestTheUserIsAskedForAPolicyAndTheAnswerIsGiven(t *testing.T) {
	for pick, want := range map[int]string{0: "balanced", 1: "allow-all", 2: "deny-all"} {
		f := &fakePolicyStep{pick: pick, picked: true}
		if !f.run() {
			t.Errorf("pick %d: it didn't say it chose one", pick)
		}
		if len(f.inits) != 1 || f.inits[0] != want {
			t.Errorf("pick %d: init = %v, want [%s]", pick, f.inits, want)
		}
		if !strings.Contains(f.said(), "sbx policy init "+want) {
			t.Errorf("pick %d: it doesn't say what it ran: %q", pick, f.said())
		}
	}
}

func TestTheQuestionNamesTheChoicesAndTheBalancedOneIsTheDefault(t *testing.T) {
	f := &fakePolicyStep{picked: true}
	f.run()
	q := f.asked[0]
	for _, want := range []string{"Balanced", "Open", "Locked down", "recommended", "sbx policy reset"} {
		if !strings.Contains(q, want) {
			t.Errorf("the question lacks %q:\n%s", want, q)
		}
	}
}

func TestAnUnansweredQuestionChoosesNothingAndSaysHowToByHand(t *testing.T) {
	f := &fakePolicyStep{picked: false}
	if f.run() {
		t.Error("it said it chose one")
	}
	if len(f.inits) != 0 {
		t.Errorf("init = %v", f.inits)
	}
	if !strings.Contains(f.said(), "sbx policy init balanced") {
		t.Errorf("said %q", f.said())
	}
}

func TestAPolicyThatCouldNotBeSetIsAWarningWithWhatSbxSaid(t *testing.T) {
	f := &fakePolicyStep{picked: true, initOut: "error: something broke", initErr: errors.New("exit 1")}
	if f.run() {
		t.Error("it said it chose one")
	}
	if !strings.Contains(f.said(), "WARNING") || !strings.Contains(f.said(), "something broke") {
		t.Errorf("said %q", f.said())
	}
}
