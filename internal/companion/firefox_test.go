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
// needFirefox says (skipping, or failing in CI on Linux) when the browser tests can't run, and gives node's path.
func needFirefox(t *testing.T) string {
	t.Helper()
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
	check := exec.Command(node, "-e", `
let playwright
try { playwright = require('playwright') } catch (e) { console.error(e.message); process.exit(2) }
process.exit(require('fs').existsSync(playwright.firefox.executablePath()) ? 0 : 1)`)
	check.Dir = "uitest"
	out, err := check.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		need("Playwright's Firefox isn't installed")
	} else if err != nil {
		need("Playwright can't be loaded (it needs Node 20 or later, and npm ci in internal/companion/uitest): " + strings.TrimSpace(string(out)) + " " + err.Error())
	}
	return node
}

func TestThePageInFirefox(t *testing.T) {
	node := needFirefox(t)
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

	url := servePage(t, dir, store)

	cmd := exec.Command(node, "--test", "firefox.test.js")
	cmd.Dir = "uitest"
	cmd.Env = append(os.Environ(), "VIBE_PAGE_URL="+url)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test uitest/firefox.test.js failed: %v\n%s", err, out)
	}
}

// servePage serves the page for the repository in dir, whose review is kept in store, and gives its address.
func servePage(t *testing.T, dir string, store *review.Store) string {
	t.Helper()
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
		ReviewTab: &ReviewTab{Workspace: dir, Store: store},
		Converse:  (&fakeAgent{}).converse,
		Choice:    (&choosing{model: "opus", options: claudeOptions}).config()}).Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	return fmt.Sprintf("http://localhost:%d/#%s", port, pageToken)
}

// TestTheCommentsPaneInFirefox runs uitest/commentspane.firefox.test.js: the comments pane's cards in a real Firefox,
// for their layout (a card's state is on a line of its own above a long path, and not squeezed beside it), which
// jsdom can't show. The repository has a file with a long path, with a comment from the agent on it, so that its
// thread waits on the user's answer.
func TestTheCommentsPaneInFirefox(t *testing.T) {
	node := needFirefox(t)
	url := commentsPaneFixture(t)

	cmd := exec.Command(node, "--test", "commentspane.firefox.test.js")
	cmd.Dir = "uitest"
	cmd.Env = append(os.Environ(), "VIBE_PAGE_URL="+url)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test uitest/commentspane.firefox.test.js failed: %v\n%s", err, out)
	}
}

// commentsPaneFixture serves the page for a repository with a long-pathed file the agent has commented on, and a short
// one the user has, and gives its address.
func commentsPaneFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	long := "Yumbi.Web/ApplicationCode/Helpers/Controls/HtmlHelperFormExtensions.cs"
	var lines []string
	for i := 1; i <= 2100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(long)), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func() {
		os.WriteFile(filepath.Join(dir, long), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "short.txt"), []byte("a\nb\nc\nd\n"), 0o644)
	}
	write()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "start")
	for i := 2029; i <= 2031; i++ {
		lines[i] += " changed"
	}
	write()
	os.WriteFile(filepath.Join(dir, "short.txt"), []byte("a\nB\nc\nd\n"), 0o644)

	store := review.NewStore(t.TempDir())
	do := func(req review.Request, who string) {
		t.Helper()
		if _, err := store.Do(req, who); err != nil {
			t.Fatal(err)
		}
	}
	do(review.Request{Op: "start"}, review.User)
	do(review.Request{Op: "comment", File: long, Line: 2030, EndLine: 2032, Body: "P2: Image form controls now resize through a blocking HTTP POST while the view renders, followed by EnsureSuccessStatusCode(). If the service is slow the whole page waits."}, review.Agent)
	do(review.Request{Op: "comment", File: "short.txt", Line: 2, Body: "a short one, nothing waiting on you"}, review.User)
	return servePage(t, dir, store)
}
