package main

import (
	"strings"
	"testing"
	"time"
)

func TestAHeldSessionWaitsForTheSandboxToComeBack(t *testing.T) {
	h := &sessionHold{}
	if h.waiting() != nil {
		t.Fatal("a session nobody held waits")
	}
	h.begin()
	resume := h.waiting()
	if resume == nil {
		t.Fatal("a held session doesn't wait")
	}
	h.begin() // a second action under the same hold changes nothing
	if h.waiting() != resume {
		t.Error("holding again made a new wait")
	}
	h.release()
	select {
	case <-resume:
	default:
		t.Error("releasing didn't let the session attach again")
	}
	if h.waiting() != nil {
		t.Error("still held after release")
	}
	h.release() // nothing held: nothing to do
}

func TestWaitingEndsWhenAnotherVibeAsksItToStop(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	if awaitResume(make(chan struct{}), stop) {
		t.Error("waiting went on after a stop request")
	}
	resume := make(chan struct{})
	close(resume)
	if !awaitResume(resume, make(chan struct{})) {
		t.Error("the sandbox came back, but waiting didn't end")
	}
}

func TestShellWordQuotesOnlyWhatNeedsIt(t *testing.T) {
	for in, want := range map[string]string{
		"/home/me/code/vibe": "/home/me/code/vibe",
		"/home/me/my code":   "'/home/me/my code'",
		"/tmp/it's":          `'/tmp/it'\''s'`,
		"":                   "''",
	} {
		if got := shellWord(in); got != want {
			t.Errorf("shellWord(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestTheAdminTabOnlyShowsTheGitToken(t *testing.T) {
	was := timeNow
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	timeNow = func() time.Time { return now }
	defer func() { timeNow = was }()

	none := adminGitToken("box", "/w", sandboxInfo{})
	if none.Has || !strings.HasPrefix(none.NewTokenURL, "https://github.com/") || none.RenewURL != "" {
		t.Errorf("no token: %+v", none)
	}
	if none.Days != 30 || none.TokenName != "vibe box" || !strings.Contains(none.NewTokenURL, "expires_in=30") {
		t.Errorf("the link's token: %+v", none)
	}
	if none.Command != "vibe --git-token /w" {
		t.Errorf("command = %q", none.Command)
	}
	soon := adminGitToken("box", "/w", sandboxInfo{gitToken: "own", gitTokenExpires: now.Add(24 * time.Hour)})
	if !soon.Has || !soon.Expiring || soon.RenewURL == "" || soon.NewTokenURL != "" {
		t.Errorf("expiring: %+v", soon)
	}
	fine := adminGitToken("box", "/w", sandboxInfo{gitToken: "own", gitTokenExpires: now.Add(60 * 24 * time.Hour)})
	if fine.Expiring || fine.Expired || fine.RenewURL != "" || fine.Expiry == "" {
		t.Errorf("fine: %+v", fine)
	}
}
