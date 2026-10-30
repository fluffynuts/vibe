package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const policyMenu = "Choose the network policy for sandboxes\n  Choice [1-3, default 2]:\n  error: operation cancelled by user"

type fakePolicyStep struct {
	sbxOut   string
	sbxErr   error
	pick     int
	picked   bool
	initOut  string
	initErr  error
	asked    []string
	inits    []string
	messages []string
}

func (f *fakePolicyStep) run() {
	setUpNetworkPolicyWith(context.Background(), networkPolicySteps{
		probe: func(context.Context) (string, error) { return f.sbxOut, f.sbxErr },
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

func TestNothingIsAskedWhenSbxHasAPolicyAlready(t *testing.T) {
	f := &fakePolicyStep{sbxOut: "[]", picked: true}
	f.run()
	if len(f.asked) != 0 || len(f.inits) != 0 {
		t.Errorf("asked %v, init %v", f.asked, f.inits)
	}
}

func TestNothingIsAskedOfAnSbxThatIsNotSignedInEither(t *testing.T) {
	f := &fakePolicyStep{sbxOut: "error: not signed in to Docker", sbxErr: errors.New("exit 1"), picked: true}
	f.run()
	if len(f.asked) != 0 || len(f.inits) != 0 {
		t.Errorf("asked %v, init %v", f.asked, f.inits)
	}
}

func TestWhenSbxAsksForAPolicyTheUserIsAskedAndTheAnswerIsGiven(t *testing.T) {
	for pick, want := range map[int]string{0: "balanced", 1: "allow-all", 2: "deny-all"} {
		f := &fakePolicyStep{sbxOut: policyMenu, sbxErr: errors.New("exit 1"), pick: pick, picked: true}
		f.run()
		if len(f.inits) != 1 || f.inits[0] != want {
			t.Errorf("pick %d: init = %v, want [%s]", pick, f.inits, want)
		}
		if !strings.Contains(f.said(), "sbx policy init "+want) {
			t.Errorf("pick %d: it doesn't say what it ran: %q", pick, f.said())
		}
	}
}

func TestTheQuestionNamesTheChoicesAndTheBalancedOneIsTheDefault(t *testing.T) {
	f := &fakePolicyStep{sbxOut: policyMenu, sbxErr: errors.New("exit 1"), picked: true}
	f.run()
	q := f.asked[0]
	for _, want := range []string{"Balanced", "Open", "Locked down", "recommended", "sbx policy reset"} {
		if !strings.Contains(q, want) {
			t.Errorf("the question lacks %q:\n%s", want, q)
		}
	}
}

func TestAnUnansweredQuestionChoosesNothingAndSaysHowToByHand(t *testing.T) {
	f := &fakePolicyStep{sbxOut: policyMenu, sbxErr: errors.New("exit 1"), picked: false}
	f.run()
	if len(f.inits) != 0 {
		t.Errorf("init = %v", f.inits)
	}
	if !strings.Contains(f.said(), "sbx policy init balanced") {
		t.Errorf("said %q", f.said())
	}
}

func TestAPolicyThatCouldNotBeSetIsAWarningWithWhatSbxSaid(t *testing.T) {
	f := &fakePolicyStep{sbxOut: policyMenu, sbxErr: errors.New("exit 1"), picked: true, initOut: "error: something broke", initErr: errors.New("exit 1")}
	f.run()
	if !strings.Contains(f.said(), "WARNING") || !strings.Contains(f.said(), "something broke") {
		t.Errorf("said %q", f.said())
	}
}
