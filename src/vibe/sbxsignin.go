package main

import (
	"context"
	"fmt"

	"vibe/internal/sbxrun"
	"vibe/internal/signin"
)

// sbxSignInSteps are what the sign-in step does to the machine, apart so that tests can answer for it.
type sbxSignInSteps struct {
	choose func(question string, labels []string, def int) (int, bool)
	// login runs sbx login on the terminal, until the user has confirmed the code in their browser.
	login func() error
	// recheck is what `sbx ls` printed afterwards, and its error.
	recheck func(ctx context.Context) (string, error)
	say     func(line string)
}

// realSbxSignInSteps are the terminal's, and sbx's, answers to the sign-in offer.
func realSbxSignInSteps() sbxSignInSteps {
	return sbxSignInSteps{
		choose:  chooseFrom,
		login:   sbxrun.Login,
		recheck: func(ctx context.Context) (string, error) { return probeWithin(ctx, "ls") },
		say:     func(line string) { note("%s", line) },
	}
}

// offerSbxSignIn offers to sign sbx in to Docker, which it isn't, with the terminal as sbx's own command has it: an
// address, a code, and a wait. It says whether sbx is signed in after.
func offerSbxSignIn(ctx context.Context, s sbxSignInSteps) bool {
	byHand := "  not signed in to Docker: sbx can't be used until you are. Run: sbx login"
	labels := []string{
		"yes: sign in now (it shows an address and a code to confirm in your browser)",
		"not now: I'll run sbx login myself",
	}
	idx, ok := s.choose("sbx isn't signed in to Docker, which it needs for everything. Sign in now?", labels, 0)
	if !ok || idx != 0 {
		s.say(byHand)
		return false
	}
	if err := s.login(); err != nil {
		s.say(fmt.Sprintf("  WARNING: sbx login failed (%s). Run it again by hand: sbx login", err))
		return false
	}
	// Whether it took is asked of sbx, not taken from its having exited.
	out, err := s.recheck(ctx)
	if signin.Docker(out, err, ctx.Err() != nil).State == signin.SignedIn {
		s.say("  signed in to Docker")
		return true
	}
	s.say("  WARNING: sbx login finished, but sbx still isn't signed in. Run it again by hand: sbx login")
	return false
}
