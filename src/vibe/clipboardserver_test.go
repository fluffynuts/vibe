package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clipboardServer runs the clipboard feature's real server, with its
// history in a folder of the test's own, and gives its address.
func clipboardServer(t *testing.T) (base, dir string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed: the clipboard server's tests need it")
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	dir = t.TempDir()
	script := filepath.Join("..", "..", "library", "base", "clipboard-bridge", "agent-files", ".local", "share", "vibe", "clipboard-server.js")
	cmd := exec.Command(node, script)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VIBE_CLIPBOARD_PORT=%d", port), "VIBE_CLIPBOARD_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	// It says so once it's listening.
	listening := make(chan bool, 1)
	go func() {
		line, _ := bufio.NewReader(out).ReadString('\n')
		listening <- strings.Contains(line, "listening")
	}()
	select {
	case ok := <-listening:
		if !ok {
			t.Fatal("the clipboard server didn't start")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the clipboard server didn't say it was listening within 10s")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), dir
}

func clipboardItems(t *testing.T, base string) int {
	t.Helper()
	res, err := http.Get(base + "/api/items")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	return len(body.Items)
}

func TestTheClipboardCanBeClearedAtOnce(t *testing.T) {
	base, dir := clipboardServer(t)
	for _, text := range []string{"one", "two"} {
		res, err := http.Post(base+"/api/items", "text/plain", strings.NewReader(text))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	if n := clipboardItems(t, base); n != 2 {
		t.Fatalf("%d items before clearing, want 2", n)
	}

	req, _ := http.NewRequest(http.MethodDelete, base+"/api/items", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/items: %d", res.StatusCode)
	}
	if n := clipboardItems(t, base); n != 0 {
		t.Errorf("%d items after clearing, want none", n)
	}
	if files, _ := os.ReadDir(filepath.Join(dir, "items")); len(files) != 0 {
		t.Errorf("their files are still there: %d", len(files))
	}
}

// runClipboardHook runs the clipboard feature's hook as Claude Code does,
// for event, against the server at base, and gives what it printed.
func runClipboardHook(t *testing.T, base, event string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed: the clipboard hook's tests need it")
	}
	script := filepath.Join("..", "..", "library", "base", "clipboard-bridge", "agent-files", ".local", "share", "vibe", "clipboard-hook.js")
	cmd := exec.Command(node, script)
	cmd.Env = append(os.Environ(), "VIBE_CLIPBOARD_SERVER="+base)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"` + event + `"}`)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the hook failed: %v", err)
	}
	return string(out)
}

// A paste waits for the user's next message: handed over mid-turn, it
// reached the agent with no word of what it was for, before the user could
// even title it.
func TestAPasteWaitsForTheUsersNextMessage(t *testing.T) {
	base, _ := clipboardServer(t)
	res, err := http.Post(base+"/api/items", "text/plain", strings.NewReader("pasted while the agent worked"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if out := runClipboardHook(t, base, "PostToolUse"); out != "" {
		t.Errorf("handed over after a tool call: %s", out)
	}
	if out := runClipboardHook(t, base, "UserPromptSubmit"); !strings.Contains(out, "pasted while the agent worked") {
		t.Errorf("not handed over with the user's message: %q", out)
	}
}

func TestTheClipboardHookIsRegisteredForPromptsAlone(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "library", "base", "clipboard-bridge", "install-scripts", "01-add-clipboard-hooks"))
	if err != nil {
		t.Fatal(err)
	}
	// The settings are the heredoc's body: the line after <<'JSON', up to
	// the line JSON. Git on Windows checks the script out with CRLF.
	text := strings.ReplaceAll(string(script), "\r\n", "\n")
	marker := strings.Index(text, "<<'JSON'")
	if marker < 0 {
		t.Fatal("no settings found in the install script")
	}
	body := text[marker:]
	body = body[strings.Index(body, "\n")+1:]
	end := strings.Index(body, "\nJSON\n")
	if end < 0 {
		t.Fatal("the install script's settings don't end")
	}
	var settings struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(body[:end]), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Hooks) != 1 || settings.Hooks["UserPromptSubmit"] == nil {
		t.Errorf("hooks registered: %v, want UserPromptSubmit alone", keysOf(settings.Hooks))
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// A paste the agent hasn't been given yet isn't pasted twice: pasting A, B,
// then A again keeps one A, and says so. Once they've gone with a message,
// the same paste is welcome again: the user may be pointing back to it.
func TestThePasteWaitingIsntPastedTwice(t *testing.T) {
	base, _ := clipboardServer(t)
	paste := func(body string) (int, bool) {
		t.Helper()
		res, err := http.Post(base+"/api/items", "image/png", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var got struct {
			Duplicate bool `json:"duplicate"`
		}
		json.NewDecoder(res.Body).Decode(&got)
		return res.StatusCode, got.Duplicate
	}
	for _, body := range []string{"image A", "image B"} {
		if code, dup := paste(body); code != http.StatusCreated || dup {
			t.Fatalf("%s: %d, duplicate %v", body, code, dup)
		}
	}
	if code, dup := paste("image A"); code != http.StatusOK || !dup {
		t.Errorf("A again: %d, duplicate %v", code, dup)
	}
	if n := clipboardItems(t, base); n != 2 {
		t.Errorf("%d items, want 2", n)
	}
	runClipboardHook(t, base, "UserPromptSubmit")
	if code, dup := paste("image A"); code != http.StatusCreated || dup {
		t.Errorf("A after it was sent: %d, duplicate %v", code, dup)
	}
}
