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
	"vibe/internal/kitspec"
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

// The hook is registered by a plugin of vibe's (as vibe-band's are), not a managed-settings drop-in: Claude Code 2.1.331 ran
// nothing from /etc/claude-code/managed-settings.d where an organisation's managed settings came from its servers, and pastes
// were never handed over.
func TestTheClipboardHookIsRegisteredByAPluginForPromptsAlone(t *testing.T) {
	dir := filepath.Join("..", "..", "library", "base", "clipboard-bridge", "agent-files", ".claude", "skills", "vibe-clipboard")
	var manifest struct {
		Name string `json:"name"`
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Name != "vibe-clipboard" {
		t.Errorf("plugin.json: %q, %v", manifest.Name, err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type, Command string
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Hooks) != 1 || len(settings.Hooks["UserPromptSubmit"]) != 1 {
		t.Fatalf("hooks registered: %+v, want UserPromptSubmit alone", settings.Hooks)
	}
	if h := settings.Hooks["UserPromptSubmit"][0].Hooks; len(h) != 1 || h[0].Type != "command" || h[0].Command != kitspec.AgentHome+"/.local/bin/clipboard-hook" {
		t.Errorf("the hook: %+v", h)
	}
}

// Where Claude Code does read managed-settings drop-ins, the old registration would run the hook a second time: the install step
// takes it away.
func TestTheOldManagedSettingsRegistrationIsTakenAway(t *testing.T) {
	scripts, err := os.ReadDir(filepath.Join("..", "..", "library", "base", "clipboard-bridge", "install-scripts"))
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, e := range scripts {
		data, err := os.ReadFile(filepath.Join("..", "..", "library", "base", "clipboard-bridge", "install-scripts", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "<<'JSON'") {
			t.Errorf("%s still writes a managed-settings drop-in", e.Name())
		}
		removed = removed || strings.Contains(text, "rm -f /etc/claude-code/managed-settings.d/50-vibe-clipboard.json")
	}
	if !removed {
		t.Error("no install step takes the old drop-in away")
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
	// Pasted last, A is what Ctrl-V in the agent's terminal gives.
	if current := clipboardCurrent(t, base); current != 1 {
		t.Errorf("current item %d, want A's (1)", current)
	}
	runClipboardHook(t, base, "UserPromptSubmit")
	if code, dup := paste("image A"); code != http.StatusCreated || dup {
		t.Errorf("A after it was sent: %d, duplicate %v", code, dup)
	}
}

// clipboardCurrent is the id of the clipboard's current item: the one
// Ctrl-V in the agent's terminal attaches.
func clipboardCurrent(t *testing.T, base string) int {
	t.Helper()
	res, err := http.Get(base + "/api/items")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		Current int `json:"current"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got.Current
}

// A browser types a file by its extension, and a name like .zarro-defaults has none: it sends no type, and a text file was kept as
// binary (seen 2026-11-23). Content with no type of its own is looked at instead.
func TestAFileSentWithoutAUsefulTypeIsTextWhenItReadsAsText(t *testing.T) {
	base, _ := clipboardServer(t)
	post := func(body, contentType string) (kind, typ, path string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, base+"/api/items", strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("X-Filename", ".zarro-defaults")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var got struct{ Kind, Type, Path string }
		json.NewDecoder(res.Body).Decode(&got)
		return got.Kind, got.Type, got.Path
	}
	if kind, typ, path := post("# defaults\nFOO=\"this is foo – ok\"\n", "application/octet-stream"); kind != "text" || typ != "text/plain" || !strings.HasSuffix(path, ".txt") {
		t.Errorf("a text file sent as octet-stream: kind %q, type %q, at %s; want text, text/plain, .txt", kind, typ, path)
	}
	if kind, typ, _ := post("FOO=1\n", ""); kind != "text" || typ != "text/plain" {
		t.Errorf("a text file sent without a type: kind %q, type %q", kind, typ)
	}
	if kind, typ, _ := post("PK\x03\x04\x00\x00binary", "application/octet-stream"); kind != "file" || typ != "application/octet-stream" {
		t.Errorf("binary content: kind %q, type %q; want file, application/octet-stream", kind, typ)
	}
	if kind, _, _ := post("\xff\xfe not UTF-8", "application/octet-stream"); kind != "file" {
		t.Errorf("content that isn't UTF-8 is kept as a file, got kind %q", kind)
	}
	if _, typ, _ := post("%PDF-1.7 text-looking", "application/pdf"); typ != "application/pdf" {
		t.Errorf("a type the browser did give is kept, got %q", typ)
	}
}
