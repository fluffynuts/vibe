package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"vibe/internal/running"
)

func TestAVibeAskedToCancelSaysWhatItEnded(t *testing.T) {
	home := t.TempDir()
	s, err := running.Register(home, "/work/a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	done := make(chan struct{})
	defer close(done)
	ended := []string{"the agent's turn", "the Admin tab's rebuild"}
	go answerCancels(s, func() []string { e := ended; ended = nil; return e }, done)

	answer, err := running.AskToCancel(home, os.Getpid(), 5*time.Second)
	if err != nil || answer != "cancelled the agent's turn and the Admin tab's rebuild" {
		t.Errorf("first: %q, %v", answer, err)
	}
	answer, err = running.AskToCancel(home, os.Getpid(), 5*time.Second)
	if err != nil || answer != "nothing was running to cancel" {
		t.Errorf("again: %q, %v", answer, err)
	}
}

func TestCancelWithNoVibeForTheFolderSaysSo(t *testing.T) {
	home := t.TempDir()
	got := strings.Join(notesOf(func() {
		if err := doCancel(home, "/work/nothing-here"); err != nil {
			t.Errorf("err = %v", err)
		}
	}), "\n")
	if !strings.Contains(got, "no vibe is running for /work/nothing-here") {
		t.Errorf("said %q", got)
	}
}

func TestAVibeWithNoPageYetStillAnswers(t *testing.T) {
	home := t.TempDir()
	s, err := running.Register(home, "/work/a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	setPageCancel(nil)
	go answerCancels(s, cancelPage, nil)
	answer, err := running.AskToCancel(home, os.Getpid(), 5*time.Second)
	if err != nil || answer != "nothing was running to cancel" {
		t.Errorf("answer %q, %v", answer, err)
	}
	setPageCancel(func() []string { return []string{"the agent's turn"} })
	defer setPageCancel(nil)
	answer, _ = running.AskToCancel(home, os.Getpid(), 5*time.Second)
	if answer != "cancelled the agent's turn" {
		t.Errorf("once there's a page: %q", answer)
	}
}
