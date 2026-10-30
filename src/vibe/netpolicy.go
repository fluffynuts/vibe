package main

import (
	"context"
	"strings"

	"vibe/internal/sbxrun"
)

// networkPolicySteps are what the step does to the machine, apart so that tests can answer for it.
type networkPolicySteps struct {
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

// realNetworkPolicySteps are the terminal's, and sbx's, answers to the network policy question.
func realNetworkPolicySteps() networkPolicySteps {
	return networkPolicySteps{
		choose: chooseFrom,
		initPolicy: func(ctx context.Context, choice string) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, sbxReadyWithin)
			defer cancel()
			return sbxrun.Probe(ctx, "policy", "init", choice)
		},
		say: func(line string) { note("%s", line) },
	}
}

// askNetworkPolicy asks which network policy sbx starts with, which it is waiting to be told (it asks before any command,
// and a command with no one to answer fails), and gives it. It says whether it did.
func askNetworkPolicy(ctx context.Context, s networkPolicySteps) bool {
	labels := make([]string, len(networkPolicyChoices))
	for i, c := range networkPolicyChoices {
		labels[i] = c.label
	}
	idx, ok := s.choose("which network policy should sandboxes start with? (it applies to every sandbox, and can be changed only by sbx policy reset, which clears every sandbox's rules)", labels, 0)
	if !ok {
		s.say("  no network policy chosen: sbx will ask when it first needs one, or choose now with: sbx policy init balanced")
		return false
	}
	choice := networkPolicyChoices[idx].arg
	if out, err := s.initPolicy(ctx, choice); err != nil {
		s.say("  WARNING: sbx policy init " + choice + " failed: " + firstLineOf(out, err))
		return false
	}
	s.say("  network policy: " + choice + " (sbx policy init " + choice + ")")
	return true
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
