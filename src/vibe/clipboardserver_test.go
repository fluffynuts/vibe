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
