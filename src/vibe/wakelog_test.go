package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWakeNotesAreAppendedToTheFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sbx-wake.log")
	appendWakeNote(file, "first\nwith output", 1<<20)
	appendWakeNote(file, "second", 1<<20)
	got, _ := os.ReadFile(file)
	if string(got) != "first\nwith output\n\nsecond\n\n" {
		t.Errorf("file = %q", got)
	}
}

func TestTheWakeLogStartsAgainOnceItIsPastItsLimit(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sbx-wake.log")
	appendWakeNote(file, strings.Repeat("x", 100), 50)
	appendWakeNote(file, "fresh", 50)
	got, _ := os.ReadFile(file)
	if string(got) != "fresh\n\n" {
		t.Errorf("file = %q", got)
	}
}
