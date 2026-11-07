package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The page's tests need jsdom and Playwright's Firefox. A run that skipped them said ok, and went unnoticed (a full
// `go test ./...` that took 25 s, not the 85 s with the real-Firefox test in it): so they fail when what they need is
// missing, saying how to get it, and nothing in them can skip.
// pageTestFiles are the Go files that run the page's tests, in jsdom or in Firefox.
var pageTestFiles = []string{"review_ui_test.go", "firefox_test.go", "emoji_test.go", "markdown_test.go", "manage_ui_test.go"}

func TestThePageTestsNeverSkipForWantOfWhatTheyNeed(t *testing.T) {
	for _, name := range pageTestFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "companion", name))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "t.Skip") && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d skips: %s\nA test of the page that can't run has to fail, saying how to get what it needs", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// What a failing page test tells the person to run: the one command, which is the same in every script. Each file gets
// its message from the shared helper, which names it.
func TestThePageTestsSayToRunTestDepsWhenWhatTheyNeedIsMissing(t *testing.T) {
	helper, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "companion", "testdeps_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"make test-deps", "./make.sh test-deps", "./make.ps1 test-deps"} {
		if !strings.Contains(string(helper), want) {
			t.Errorf("the shared message doesn't say %q", want)
		}
	}
	for _, name := range pageTestFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "companion", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "getTestDeps") && !strings.Contains(string(data), "nodeMissing(") {
			t.Errorf("%s doesn't get its message from the shared helper, which says to run test-deps", name)
		}
	}
}

// test-deps is in each of the three scripts, and installs the same two things, where the tests look for them.
func TestEveryBuildScriptCanInstallWhatThePageTestsNeed(t *testing.T) {
	for _, name := range []string{"Makefile", "make.sh", "make.ps1"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), name))
		if err != nil {
			t.Fatal(err)
		}
		for what, want := range map[string]string{
			"a test-deps target":                 "test-deps",
			"jsdom, installed from the lockfile": "npm ci",
			"the pinned Playwright's Firefox":    "playwright install firefox",
			"in the folder the tests use":        "internal/companion/uitest",
		} {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s lacks %s (%q)", name, what, want)
			}
		}
	}
}
