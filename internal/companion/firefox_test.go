package companion

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vibe/internal/policy"
	"vibe/internal/review"
)

// TestThePageInFirefox runs uitest/firefox.test.js: the page in a real
// Firefox, for what jsdom can't show, served for a repository where a.txt's
// lines 20-22 are changed, and main.go's line 4. It needs Playwright's Firefox (npm ci, then npx
// playwright install firefox, in uitest): without it, it's skipped saying
// so, but never in CI on Linux, where it's installed and skipping would
// pass it unseen.
func TestThePageInFirefox(t *testing.T) {
	need := func(why string) {
		t.Helper()
		how := why + ": in internal/companion/uitest, run npm ci, then npx playwright install firefox"
		if os.Getenv("CI") != "" && runtime.GOOS == "linux" {
			t.Fatal(how)
		}
		t.Skip(how)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		need("node isn't installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		need("git isn't installed")
	}
	check := exec.Command(node, "-e", "const fs = require('fs'); process.exit(fs.existsSync(require('playwright').firefox.executablePath()) ? 0 : 1)")
	check.Dir = "uitest"
	if check.Run() != nil {
		need("Playwright's Firefox isn't installed")
	}

	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	write := func() { os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644) }
	write()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc f() int {\n\treturn 1\n}\n"), 0o644)
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "start")
	for i := 19; i <= 21; i++ {
		lines[i] += " changed"
	}
	write()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc f() int {\n\treturn 2\n}\n"), 0o644)
	store := review.NewStore(t.TempDir())
	if _, err := store.Do(review.Request{Op: "start"}, review.User); err != nil {
		t.Fatal(err)
	}

	// A token shaped as vibe makes them: the page takes nothing else from
	// its address.
	pageToken := strings.Repeat("0123456789abcdef", 4)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: New(Config{Sandbox: "box", Token: pageToken, Port: port,
		Policy:    &policy.Client{Sandbox: "box", Run: (&fakeSbx{}).run},
		ReviewTab: &ReviewTab{Workspace: dir, Store: store}}).Handler()}
	go srv.Serve(ln)
	defer srv.Close()

	cmd := exec.Command(node, "--test", "firefox.test.js")
	cmd.Dir = "uitest"
	cmd.Env = append(os.Environ(), fmt.Sprintf("VIBE_PAGE_URL=http://localhost:%d/#%s", port, pageToken))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test uitest/firefox.test.js failed: %v\n%s", err, out)
	}
}
