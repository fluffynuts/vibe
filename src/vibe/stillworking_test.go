package main

import (
	"io"
	"testing"
)

// A terminal has sbx's own progress to say a build is under way; only a page's log, which gets none, is told.
func TestOnlyAPagesLogIsToldTheBuildIsStillGoing(t *testing.T) {
	if stillWorking(nil, "box") != nil {
		t.Error("the terminal, which shows sbx's progress, is told as well")
	}
	if stillWorking(io.Discard, "box") == nil {
		t.Error("a page's log is told nothing while a build goes on")
	}
}
