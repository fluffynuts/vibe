package main

import (
	"os"
	"testing"
	"time"

	"vibe/internal/hostcpu"
	"vibe/internal/hostmem"
	"vibe/internal/prompt"
	"vibe/internal/review"
)

// TestMain runs every test with no terminal to ask on, whatever the test
// binary was started from. Code that would prompt takes its no-terminal path
// (an error, or -f's answer) instead of waiting on a keypress no one will
// make — which is exactly what it did on a Windows runner, whose console
// opens as readily as a real one.
func TestMain(m *testing.M) {
	// A copy of this binary is the helper reading repositories, as vibe is.
	review.ServeHelperIfAsked()
	// A copy of this binary, started with this set, is a stand-in for a
	// vibe that's still running — see TestReplaceFileWhileTheTargetIsRunning.
	if os.Getenv("VIBE_TEST_STAY_RUNNING") != "" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	openTerminal = func() (*prompt.Terminal, bool) { return nil, false }
	openTerminalInput = func() (*os.File, func(), bool) { return nil, func() {}, false }
	// And on the same machine, whatever it really is: one big enough for
	// any memory setting the tests use, so none is lowered unasked.
	hostmem.Total = func() (uint64, error) { return 64 * hostmem.GiB, nil }
	// With 16 CPUs, so the middle is 8 and 1 to 15 can be asked for.
	hostcpu.Total = func() int { return 16 }
	// One whose KVM works, whether this one's does or not (a CI runner, or
	// a sandbox, may have none): tests of a machine where it doesn't say so.
	vmCheck = func() error { return nil }
	os.Exit(m.Run())
}
