package main

import (
	"fmt"
	"time"

	"vibe/internal/policy"
	"vibe/internal/state"
)

// rulesFor is the sbx policy client for sandbox name: a variable for tests.
var rulesFor = func(name string) *policy.Client { return &policy.Client{Sandbox: name} }

// keepRules keeps the sandbox's own network rules on the host (see
// policy.Snapshot), replacing those kept before; rules that can't be read
// leave them as they were.
func keepRules(vibeHome, name string) error {
	_, err := rulesFor(name).SnapshotTo(state.RulesPath(vibeHome, name), time.Now())
	return err
}

// restoreRules gives a sandbox just made the network rules kept for it:
// what to say of what was put back, and what wasn't, and why.
func restoreRules(vibeHome, name string) (said []string, problems []error) {
	snap, found, err := policy.LoadSnapshot(state.RulesPath(vibeHome, name))
	if err != nil {
		return nil, []error{fmt.Errorf("%w: none of the sandbox's own network rules were put back", err)}
	}
	if !found || len(snap.Rules) == 0 {
		return nil, nil
	}
	restored, failed := rulesFor(name).Restore(snap)
	if len(restored) > 0 {
		said = append(said, fmt.Sprintf("network rules of its own put back, as kept on %s:", snap.Taken.Local().Format("2006-01-02 15:04")))
		for _, r := range restored {
			said = append(said, "  "+r)
		}
	}
	return said, failed
}

// noteRestoredRules restores the sandbox's kept rules, saying what came of it.
func noteRestoredRules(vibeHome, name string) {
	said, problems := restoreRules(vibeHome, name)
	for _, line := range said {
		note("%s", line)
	}
	for _, p := range problems {
		note("ERROR: %s", p)
	}
}

// noteKeptRules keeps the sandbox's own rules, warning when it can't.
func noteKeptRules(vibeHome, name string) {
	if err := keepRules(vibeHome, name); err != nil {
		note("WARNING: %s", err)
	}
}
