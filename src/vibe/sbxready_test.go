package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// policyListed is `sbx policy ls` on a machine whose network policy is chosen: the row `sbx policy init` makes.
const policyListed = "  POLICY         SOURCE   APPLIES TO   SUMMARY\n  local-policy   local    all          network: 195 allow"

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
				return policyListed, nil
			}
			out := r.policyLooks[min(j, len(r.policyLooks)-1)]
			j++
			if !strings.HasPrefix(out, "error") && out != policyMenu {
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
	r := &readiness{looks: []string{"[]"}, policyLooks: []string{policyMenu, policyListed}, policy: true}
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
	r := &readiness{looks: []string{signedOutText, "[]"}, policyLooks: []string{policyMenu, policyListed}, policy: true, signIn: true}
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
	r = &readiness{looks: []string{"[]"}, policyLooks: []string{"error: the daemon fell over"}, policy: false, signIn: true}
	r.run()
	if r.done() != "policy" {
		t.Errorf("a policy listing that failed otherwise is asked about once, and given up on if declined: did %q", r.done())
	}
}

func TestAnSbxWithEverythingInPlaceIsLookedAtTwiceAndNothingIsDone(t *testing.T) {
	looks, policyLooks := 0, 0
	sbxReadySteps{
		probe:       func(context.Context) (string, error) { looks++; return "[]", nil },
		policyProbe: func(context.Context) (string, error) { policyLooks++; return policyListed, nil },
		policy:      func(context.Context) bool { t.Error("policy"); return true },
		signIn:      func(context.Context) bool { t.Error("sign-in"); return true },
	}.run(context.Background())
	if looks != 1 || policyLooks != 1 {
		t.Errorf("looks %d, policy looks %d", looks, policyLooks)
	}
}

// Once signed in, sbx doesn't put its question to `sbx policy ls`: it lists no global policy (the `local-policy` row `init`
// makes), so that is what shows that none was chosen. The real VM asked at the first sandbox, not at install.
const policyListedEmpty = "  POLICY   SOURCE   APPLIES TO   SUMMARY\n"

func TestWhetherAPolicyIsWanted(t *testing.T) {
	for name, tc := range map[string]struct {
		out    string
		err    error
		chosen bool
		want   bool
	}{
		"the menu itself":                        {policyMenu, errors.New("exit 1"), false, true},
		"the menu, even when chosen before":      {policyMenu, errors.New("exit 1"), true, true},
		"a list with the global policy":          {policyListed, nil, false, false},
		"a list with none":                       {policyListedEmpty, nil, false, true},
		"a list with only a sandbox's own rules": {"  POLICY SOURCE APPLIES TO\n  1c  kit  sandbox:box  network: 3 allow\n", nil, false, true},
		"a list with none, but chosen before":    {policyListedEmpty, nil, true, false},
		// With nothing to read an answer from, what sbx prints for its question isn't known (the menu, an error, nothing): a
		// failure that isn't a timeout is taken as the question being there, since asking is safe (sbx refuses a second policy).
		"a failure whose words are not known": {"error: cannot ask without a terminal", errors.New("exit 1"), false, true},
		"a failure with no words":             {"", errors.New("exit 1"), false, true},
		"that failure, but chosen before":     {"error: cannot ask without a terminal", errors.New("exit 1"), true, false},
		"sbx not answering in time":           {"", context.DeadlineExceeded, false, false},
	} {
		if got := policyWanted(tc.out, tc.err, tc.chosen); got != tc.want {
			t.Errorf("%s: wanted = %v, want %v", name, got, tc.want)
		}
	}
}

func TestASignedInSbxWithNoGlobalPolicyIsAskedForOne(t *testing.T) {
	r := &readiness{looks: []string{"[]"}, policyLooks: []string{policyListedEmpty, policyListed}, policy: true}
	r.run()
	if r.done() != "policy" {
		t.Errorf("did %q", r.done())
	}
}

// A policy sbx says is already chosen is not asked for again, nor looked for again in a loop.
func TestAnAlreadyChosenPolicyEndsItWithoutLooping(t *testing.T) {
	r := &readiness{looks: []string{"[]"}, policyLooks: []string{policyListedEmpty}, policy: false}
	r.run()
	if r.done() != "policy" {
		t.Errorf("did %q", r.done())
	}
}

func TestARefusalBecauseThePolicyWasAlreadyChosenIsNotAFailureAndIsRemembered(t *testing.T) {
	f := &fakePolicyStep{picked: true, initOut: "error: global network policy is already initialized; reset it first to change it", initErr: errors.New("exit 1")}
	var remembered int
	got := askNetworkPolicy(context.Background(), networkPolicySteps{
		choose:     func(string, []string, int) (int, bool) { return 0, true },
		initPolicy: func(context.Context, string) (string, error) { return f.initOut, f.initErr },
		remember:   func() { remembered++ },
		say:        func(line string) { f.messages = append(f.messages, line) },
	})
	if got {
		t.Error("it said it chose one: it would be looked for again")
	}
	if remembered != 1 {
		t.Errorf("remembered %d times", remembered)
	}
	if strings.Contains(f.said(), "WARNING") || !strings.Contains(f.said(), "already") {
		t.Errorf("said %q", f.said())
	}
}

func TestAPolicyChosenIsRemembered(t *testing.T) {
	var remembered int
	askNetworkPolicy(context.Background(), networkPolicySteps{
		choose:     func(string, []string, int) (int, bool) { return 0, true },
		initPolicy: func(context.Context, string) (string, error) { return "", nil },
		remember:   func() { remembered++ },
		say:        func(string) {},
	})
	if remembered != 1 {
		t.Errorf("remembered %d times", remembered)
	}
}
