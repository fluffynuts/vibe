package packagereq

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newMailbox(t *testing.T) Mailbox {
	t.Helper()
	m := Mailbox{Dir: t.TempDir()}
	for _, d := range []string{m.Inbox(), m.View()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func drop(t *testing.T, m Mailbox, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.Inbox(), name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTakeGivesRequestsOldestFirstAndEmptiesTheInbox(t *testing.T) {
	m := newMailbox(t)
	drop(t, m, "200-b.json", `{"ecosystem":"nuget","id":"B","version":"2.0.0","framework":"net8.0"}`)
	drop(t, m, "100-a.json", `{"ecosystem":"nuget","id":"A","version":"1.0.0","framework":"net8.0"}`)
	taken := m.Take()
	if len(taken) != 2 || taken[0].Request.ID != "A" || taken[1].Request.ID != "B" {
		t.Fatalf("taken = %+v", taken)
	}
	if taken[0].Nonce != "100-a" {
		t.Errorf("the nonce is the file's name, not what the agent wrote inside: %q", taken[0].Nonce)
	}
	if left, _ := os.ReadDir(m.Inbox()); len(left) != 0 {
		t.Errorf("inbox not emptied: %v", left)
	}
}

func TestTakeSaysWhyARequestCouldntBeTakenAndStillClearsIt(t *testing.T) {
	m := newMailbox(t)
	drop(t, m, "1-bad.json", `not json`)
	drop(t, m, "2-big.json", strings.Repeat("x", maxRequest+1))
	taken := m.Take()
	if len(taken) != 2 || taken[0].Err == nil || taken[1].Err == nil {
		t.Fatalf("each should say why: %+v", taken)
	}
	if taken[0].Nonce != "1-bad" {
		t.Errorf("an answer can still go to the one that asked: %q", taken[0].Nonce)
	}
	if left, _ := os.ReadDir(m.Inbox()); len(left) != 0 {
		t.Errorf("a request that can't be read would be taken again, forever: %v", left)
	}
}

func TestTakeIgnoresFilesNamedNothingLikeANonce(t *testing.T) {
	m := newMailbox(t)
	drop(t, m, "Has Space.json", `{"ecosystem":"nuget"}`)
	drop(t, m, "notes.txt", `hello`)
	for _, tk := range m.Take() {
		if tk.Err == nil {
			t.Errorf("taken as a request: %+v", tk)
		}
		if tk.Nonce != "" {
			t.Errorf("answers must never be written to a name the agent chose freely: %q", tk.Nonce)
		}
	}
}

func TestReplyWritesTheAnswerWhereTheSandboxLooks(t *testing.T) {
	m := newMailbox(t)
	if err := m.Reply("100-a", Answer{Status: Fetched, Message: "Demo.Pkg 1.0.0 fetched"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.View(), "100-a.answer"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fetched\nDemo.Pkg 1.0.0 fetched\n" {
		t.Errorf("answer = %q", data)
	}
	if left, _ := filepath.Glob(filepath.Join(m.View(), "*")); len(left) != 1 {
		t.Errorf("a half-written file must never be where the sandbox looks: %v", left)
	}
}

func TestReplyRefusesANonceThatCouldBeAPath(t *testing.T) {
	m := newMailbox(t)
	if err := m.Reply("../escape", Answer{Status: Failed}); err == nil {
		t.Error("wrote an answer at a path the agent chose")
	}
}
