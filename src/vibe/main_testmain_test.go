package main

import (
	"os"
	"testing"

	"vibe/internal/prompt"
)

// TestMain runs every test with no terminal to ask on, whatever the test
// binary was started from. Code that would prompt takes its no-terminal path
// (an error, or -f's answer) instead of waiting on a keypress no one will
// make — which is exactly what it did on a Windows runner, whose console
// opens as readily as a real one.
func TestMain(m *testing.M) {
	openTerminal = func() (*prompt.Terminal, bool) { return nil, false }
	openTerminalInput = func() (*os.File, func(), bool) { return nil, func() {}, false }
	os.Exit(m.Run())
}
