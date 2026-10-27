package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testSession = "c36c1417-3033-47d5-9248-6e6852a97454"

// cursorHome is a sandbox's home with Cursor's files in it: the session of
// testSession, and a transcript for it and for an earlier chat.
func cursorHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the scripts are for a Linux sandbox: GNU tar, base64 -w0, du and find -print0")
	}
	home := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		full := filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".cursor/chats/abc123/"+testSession+"/store.db", "the session")
	write(".cursor/chats/abc123/other-session/store.db", "another session")
	write(".cursor/projects/tmp-test/agent-transcripts/"+testSession+"/"+testSession+".jsonl", "this chat")
	write(".cursor/projects/tmp-test/agent-transcripts/earlier-chat/earlier-chat.jsonl", "an earlier chat")
	return home
}

// runIn runs script as a sandbox's shell would, as the user whose home is
// home, with input on its stdin.
func runIn(t *testing.T, home, script, input string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return string(out)
}

func fetched(t *testing.T, home, script string) []byte {
	t.Helper()
	out := runIn(t, home, script, "")
	tgz, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil || len(tgz) == 0 {
		t.Fatalf("not a base64 archive (%v): %.80q", err, out)
	}
	return tgz
}

func listing(t *testing.T, tgz []byte) string {
	t.Helper()
	cmd := exec.Command("tar", "-tzf", "-")
	cmd.Stdin = bytes.NewReader(tgz)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestTheSessionAndEveryChatsTranscriptAreKeptTogether(t *testing.T) {
	// The transcripts are what the agent searches for what it can't tell from
	// the conversation (after it was summarised, say): so all of them, not
	// only this chat's, are worth having after a rebuild.
	got := listing(t, fetched(t, cursorHome(t), cursorFetchScript(testSession, 1<<20)))
	for _, want := range []string{
		"chats/abc123/" + testSession + "/store.db",
		"projects/tmp-test/agent-transcripts/" + testSession + "/" + testSession + ".jsonl",
		"projects/tmp-test/agent-transcripts/earlier-chat/earlier-chat.jsonl",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the archive lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "other-session") {
		t.Errorf("another conversation's session was kept as this one's:\n%s", got)
	}
}

func TestTranscriptsTooBigToKeepAreLeftOutAndTheSessionStillKept(t *testing.T) {
	home := cursorHome(t)
	big := filepath.Join(home, ".cursor/projects/tmp-test/agent-transcripts/earlier-chat/earlier-chat.jsonl")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), 3<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	got := listing(t, fetched(t, home, cursorFetchScript(testSession, 1024)))
	if !strings.Contains(got, "chats/abc123/"+testSession+"/store.db") {
		t.Errorf("the session wasn't kept:\n%s", got)
	}
	if strings.Contains(got, "agent-transcripts") {
		t.Errorf("transcripts over the limit were kept:\n%s", got)
	}
}

func TestNothingIsKeptForASessionTheSandboxDoesNotHave(t *testing.T) {
	out := runIn(t, cursorHome(t), cursorFetchScript("no-such-session", 1<<20)+"; echo done", "")
	if strings.TrimSpace(out) != "done" {
		t.Errorf("something was written for a session that isn't there: %.80q", out)
	}
}

func TestARebuiltSandboxGetsTheSessionAndTheTranscriptsBack(t *testing.T) {
	old := cursorHome(t)
	tgz := fetched(t, old, cursorFetchScript(testSession, 1<<20))
	fresh := t.TempDir()
	runIn(t, fresh, cursorPutScript(), base64.StdEncoding.EncodeToString(tgz))
	for path, want := range map[string]string{
		".cursor/chats/abc123/" + testSession + "/store.db":                                         "the session",
		".cursor/projects/tmp-test/agent-transcripts/earlier-chat/earlier-chat.jsonl":               "an earlier chat",
		".cursor/projects/tmp-test/agent-transcripts/" + testSession + "/" + testSession + ".jsonl": "this chat",
	} {
		got, err := os.ReadFile(filepath.Join(fresh, path))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q, %v (want %q)", path, got, err, want)
		}
	}
}

func TestWhatTheSandboxAlreadyHasIsNotWrittenOverByTheKeptCopy(t *testing.T) {
	// A chat had in the sandbox's own terminal before the first prompt from
	// the page is newer than what was kept, and is not to be lost to it.
	tgz := fetched(t, cursorHome(t), cursorFetchScript(testSession, 1<<20))
	fresh := t.TempDir()
	newer := filepath.Join(fresh, ".cursor/projects/tmp-test/agent-transcripts/earlier-chat/earlier-chat.jsonl")
	if err := os.MkdirAll(filepath.Dir(newer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("a newer version"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, fresh, cursorPutScript(), base64.StdEncoding.EncodeToString(tgz))
	if got, _ := os.ReadFile(newer); string(got) != "a newer version" {
		t.Errorf("the kept copy was written over it: %q", got)
	}
	if _, err := os.Stat(filepath.Join(fresh, ".cursor/chats/abc123/"+testSession+"/store.db")); err != nil {
		t.Errorf("but the rest wasn't restored: %v", err)
	}
}
