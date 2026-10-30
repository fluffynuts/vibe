package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vibe/internal/fscopy"
	"vibe/internal/sbxrun"
	"vibe/internal/signin"
)

// sbxReadyWithin bounds each sbx call made to get sbx ready.
const sbxReadyWithin = 30 * time.Second

// sbxReadySteps are what getting sbx ready does, apart so that tests can answer for them.
type sbxReadySteps struct {
	// probe is what `sbx ls` printed, and its error; policyProbe the same of `sbx policy ls`, which is where sbx asks for its
	// network policy once signed in (`sbx ls` doesn't).
	probe       func(ctx context.Context) (string, error)
	policyProbe func(ctx context.Context) (string, error)
	// policy asks for the network policy sbx is waiting for, and gives it; signIn offers to sign sbx in. Each says whether
	// it did what was needed (declining, or failing, is not).
	policy func(ctx context.Context) bool
	signIn func(ctx context.Context) bool
	// chosenBefore says vibe has had a policy chosen on this machine before (given, or sbx refused another as already chosen).
	chosenBefore func() bool
}

// policyWanted says, from what `sbx policy ls` printed (and its error), whether sbx has no network policy yet. sbx puts its
// question to it, and what it prints for that with nothing to read an answer from isn't known (on a real machine it was not
// what a terminal gets): so a listing that works but lacks the local-policy row (the global one `sbx policy init` makes) is
// none, and so is a failure other than a timeout, unless vibe has had one chosen here before. Asking when unsure is safe,
// since sbx refuses a second policy.
func policyWanted(out string, err error, chosenBefore bool) bool {
	if signin.Docker(out, err, false).State == signin.NeedsPolicy {
		return true
	}
	if chosenBefore || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	return err != nil || !strings.Contains(out, "local-policy")
}

// maxSbxReadyRounds is how many things sbx may ask for, one after another: its policy, and its sign-in, and the policy again,
// since it may ask for either before the other.
const maxSbxReadyRounds = 3

// run looks at sbx, and while it is waiting for something vibe can give it (a network policy, a sign-in), gives it, and
// looks again. When sbx is ready that is an `sbx ls` and an `sbx policy ls`, and nothing said.
func (s sbxReadySteps) run(ctx context.Context) {
	for i := 0; i < maxSbxReadyRounds; i++ {
		out, err := s.probe(ctx)
		var done bool
		switch signin.Docker(out, err, ctx.Err() != nil).State {
		case signin.NeedsPolicy:
			done = s.policy(ctx)
		case signin.SignedOut:
			done = s.signIn(ctx)
		case signin.SignedIn:
			// Signed in; sbx may still be waiting to be told its policy, which it asks of `policy ls`.
			out, err = s.policyProbe(ctx)
			chosen := s.chosenBefore != nil && s.chosenBefore()
			if !policyWanted(out, err, chosen) {
				return
			}
			done = s.policy(ctx)
		default:
			return
		}
		if !done {
			return
		}
	}
}

// ensureSbxReady gets sbx ready before anything is asked of it: the network policy it wants chosen, and its sign-in to
// Docker, offered on the terminal, rather than sbx's own error (or question, which a command with no one to answer
// fails). It does nothing without a terminal, or with -f, where nobody can be asked.
func ensureSbxReady() {
	if !sbxrun.Available() || !interactive() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sbxReadySteps{
		probe:       func(ctx context.Context) (string, error) { return probeWithin(ctx, "ls") },
		policyProbe: func(ctx context.Context) (string, error) { return probeWithin(ctx, "policy", "ls") },
		policy:      func(ctx context.Context) bool { return askNetworkPolicy(ctx, realNetworkPolicySteps()) },
		signIn:      func(ctx context.Context) bool { return offerSbxSignIn(ctx, realSbxSignInSteps()) },
	}.run(ctx)
}

// probeWithin is an sbx command, given sbxReadyWithin.
func probeWithin(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, sbxReadyWithin)
	defer cancel()
	out, err := sbxrun.Probe(ctx, args...)
	if ctx.Err() != nil {
		err = ctx.Err() // what killed it was the wait, not sbx
	}
	return out, err
}

// policyChosenFile is where vibe notes that a network policy has been chosen on this machine, so that a listing it can't
// read as having one doesn't have the question asked at every start.
func policyChosenFile() string { return filepath.Join(vibeHomeDir(), "sbx-policy-chosen") }

func policyChosenBefore() bool {
	_, err := os.Stat(policyChosenFile())
	return err == nil
}

func rememberPolicyChosen() {
	if err := os.MkdirAll(filepath.Dir(policyChosenFile()), 0o755); err == nil {
		fscopy.WriteWholeMode(policyChosenFile(), []byte("a network policy has been chosen for sbx on this machine\n"), 0o644)
	}
}
