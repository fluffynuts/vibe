package main

import (
	"context"
	"strings"
	"time"

	"vibe/internal/sbxrun"
	"vibe/internal/signin"
)

// networkPolicyWithin bounds each sbx call of the network policy step.
const networkPolicyWithin = 30 * time.Second

// networkPolicySteps are what the step does to the machine, apart so that tests can answer for it.
type networkPolicySteps struct {
	// probe is what `sbx ls` printed, and its error.
	probe      func(ctx context.Context) (string, error)
	choose     func(question string, labels []string, def int) (int, bool)
	initPolicy func(ctx context.Context, choice string) (string, error)
	say        func(line string)
}

// networkPolicyChoices are sbx's three network policies, in the order they are offered: the argument sbx policy init
// takes, and how it is put to the user. Balanced is first, as the default, because sbx recommends it.
var networkPolicyChoices = []struct{ arg, label string }{
	{"balanced", "Balanced: default deny, common development sites allowed (recommended)"},
	{"allow-all", "Open: all network traffic allowed, no restrictions"},
	{"deny-all", "Locked down: all network traffic blocked unless you allow it"},
}

// setUpNetworkPolicy asks which network policy sbx starts with, when sbx has none yet, so that it is chosen here rather
// than by sbx's own question coming up in the middle of something else (it asks before any command, and a command with
// no one to answer fails). It needs a terminal, and says nothing when the policy is already chosen.
func setUpNetworkPolicy() {
	if !sbxrun.Available() || !interactive() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkPolicyWithin)
	defer cancel()
	setUpNetworkPolicyWith(ctx, networkPolicySteps{
		probe:  func(ctx context.Context) (string, error) { return sbxrun.Probe(ctx, "ls") },
		choose: chooseFrom,
		initPolicy: func(ctx context.Context, choice string) (string, error) {
			return sbxrun.Probe(ctx, "policy", "init", choice)
		},
		say: func(line string) { note("%s", line) },
	})
}

func setUpNetworkPolicyWith(ctx context.Context, s networkPolicySteps) {
	out, err := s.probe(ctx)
	if signin.Docker(out, err, ctx.Err() != nil).State != signin.NeedsPolicy {
		return
	}
	labels := make([]string, len(networkPolicyChoices))
	for i, c := range networkPolicyChoices {
		labels[i] = c.label
	}
	idx, ok := s.choose("which network policy should sandboxes start with? (it applies to every sandbox, and can be changed only by sbx policy reset, which clears every sandbox's rules)", labels, 0)
	if !ok {
		s.say("  no network policy chosen: sbx will ask when it first needs one, or choose now with: sbx policy init balanced")
		return
	}
	choice := networkPolicyChoices[idx].arg
	if out, err := s.initPolicy(ctx, choice); err != nil {
		s.say("  WARNING: sbx policy init " + choice + " failed: " + firstLineOf(out, err))
		return
	}
	s.say("  network policy: " + choice + " (sbx policy init " + choice + ")")
}

// firstLineOf is the first line sbx said, or the error when it said nothing.
func firstLineOf(out string, err error) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}
