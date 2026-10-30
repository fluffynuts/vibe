package main

import (
	"context"
	"fmt"
	"time"

	"vibe/internal/sbxrun"
	"vibe/internal/signin"
)

// sbxSignInSteps are what the sign-in step does to the machine, apart so that tests can answer for it.
type sbxSignInSteps struct {
	// probe is what `sbx ls` printed, and its error.
	probe  func(ctx context.Context) (string, error)
	choose func(question string, labels []string, def int) (int, bool)
	// login runs sbx login on the terminal, until the user has confirmed the code in their browser.
	login func() error
	say   func(line string)
}

// setUpSbx makes sbx ready to be used: its network policy chosen and its sign-in done, the questions it would otherwise
// put to the user in the middle of their first run. sbx wants the policy before it will answer anything, and may ask
// again after the sign-in, so the policy is looked at once more at the end (it says nothing when there is nothing to ask).
func setUpSbx() {
	setUpSbxSteps{policy: setUpNetworkPolicy, signIn: setUpSbxSignIn}.run()
}

type setUpSbxSteps struct{ policy, signIn func() }

func (s setUpSbxSteps) run() {
	s.policy()
	s.signIn()
	s.policy()
}

// setUpSbxSignIn offers to sign sbx in to Docker, when it isn't, with the terminal as sbx's own command has it: an
// address, a code, and a wait.
func setUpSbxSignIn() {
	if !sbxrun.Available() || !interactive() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkPolicyWithin)
	defer cancel()
	setUpSbxSignInWith(ctx, sbxSignInSteps{
		probe:  func(ctx context.Context) (string, error) { return sbxrun.Probe(ctx, "ls") },
		choose: chooseFrom,
		login:  sbxrun.Login,
		say:    func(line string) { note("%s", line) },
	})
}

func setUpSbxSignInWith(ctx context.Context, s sbxSignInSteps) {
	out, err := s.probe(ctx)
	if signin.Docker(out, err, ctx.Err() != nil).State != signin.SignedOut {
		return
	}
	byHand := "  not signed in to Docker: sbx can't be used until you are. Run: sbx login"
	labels := []string{
		"yes: sign in now (it shows an address and a code to confirm in your browser)",
		"not now: I'll run sbx login myself",
	}
	idx, ok := s.choose("sbx isn't signed in to Docker, which it needs for everything. Sign in now?", labels, 0)
	if !ok || idx != 0 {
		s.say(byHand)
		return
	}
	if err := s.login(); err != nil {
		s.say(fmt.Sprintf("  WARNING: sbx login failed (%s). Run it again by hand: sbx login", err))
		return
	}
	// Whether it took is asked of sbx, not taken from its having exited.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err = s.probe(ctx)
	if signin.Docker(out, err, ctx.Err() != nil).State == signin.SignedIn {
		s.say("  signed in to Docker")
		return
	}
	s.say("  WARNING: sbx login finished, but sbx still isn't signed in. Run it again by hand: sbx login")
}
