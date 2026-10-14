package main

import (
	"slices"
	"testing"

	"vibe/internal/running"
)

func TestATerminalAttachesAlongsideHeadlessVibes(t *testing.T) {
	claims := []running.Claim{{PID: 10, Headless: true}}
	if got := alongsideHeadless(claims, true); !slices.Equal(got, []int{10}) {
		t.Errorf("got %v", got)
	}
}

func TestOnlyAttachingGoesAlongsideAHeadlessVibe(t *testing.T) {
	// A rebuild or a delete would pull the sandbox out from under it.
	claims := []running.Claim{{PID: 10, Headless: true}}
	if got := alongsideHeadless(claims, false); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestATerminalDoesntGoAlongsideAnythingElse(t *testing.T) {
	for name, claims := range map[string][]running.Claim{
		"nothing":            nil,
		"a terminal session": {{PID: 10, Headless: true}, {PID: 11}},
		"a rebuild":          {{PID: 10, Headless: true}, {PID: 12, Work: "rebuilding sandbox 'x'"}},
	} {
		if got := alongsideHeadless(claims, true); got != nil {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
