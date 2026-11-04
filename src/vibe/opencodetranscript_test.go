package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sh(t *testing.T, home, script, stdin string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_DATA_HOME=")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("%s: %v: %s", script, err, out)
	}
	if err != nil {
		return "FAILED"
	}
	return string(out)
}

func openCodeHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "opencode")
	os.MkdirAll(dir, 0o755)
	for name, content := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	return home
}

func TestAnOpencodeConversationSurvivesBeingCarriedToAnotherSandbox(t *testing.T) {
	for _, tool := range []string{"sh", "tar", "base64"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	old := openCodeHome(t, map[string]string{"opencode.db": "...ses_abc123...", "opencode.db-wal": "wal ses_abc123", "opencode.db-shm": "shm"})
	if sh(t, old, opencodeHasScript("ses_abc123"), "") == "FAILED" {
		t.Fatal("the sandbox holding the session said it didn't")
	}
	if sh(t, old, opencodeHasScript("ses_other"), "") != "FAILED" {
		t.Fatal("a session it doesn't hold was said to be there")
	}
	packed := sh(t, old, opencodeFetchScript, "")
	if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(packed)); err != nil {
		t.Fatalf("fetch isn't base64: %v", err)
	}

	fresh := t.TempDir()
	if sh(t, fresh, opencodeHasScript("ses_abc123"), "") != "FAILED" {
		t.Fatal("a sandbox with no database said it held the session")
	}
	if sh(t, fresh, opencodePutScript, packed) == "FAILED" {
		t.Fatal("put failed")
	}
	if sh(t, fresh, opencodeHasScript("ses_abc123"), "") == "FAILED" {
		t.Fatal("the session didn't arrive")
	}
	got, _ := os.ReadFile(filepath.Join(fresh, ".local", "share", "opencode", "opencode.db-shm"))
	if string(got) != "shm" {
		t.Errorf("-shm = %q", got)
	}
}
