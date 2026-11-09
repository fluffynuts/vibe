package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The page's tests need jsdom and Playwright's Firefox. A run that skipped them said ok, and went unnoticed (a full
// `go test ./...` that took 25 s, not the 85 s with the real-Firefox test in it): so they fail when what they need is
// missing, saying how to get it, and nothing in them can skip.
// filesRunningNode are the Go test files in dir that run node, which is how a test of the page runs: found, not listed, so that
// a new one is held to the same rule without anyone remembering to add it.
func filesRunningNode(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `exec.LookPath("node")`) {
			found = append(found, filepath.Base(file))
		}
	}
	return found
}

// pageTestFiles are the Go files that run the page's tests, in jsdom or in Firefox.
func pageTestFiles(t *testing.T) []string {
	t.Helper()
	return filesRunningNode(t, filepath.Join(repoRoot(t), "internal", "companion"))
}

func TestThePageTestFilesAreFoundByWhatTheyDoNotListedByName(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"runs_test.go":    `node, err := exec.LookPath("node")`,
		"another_test.go": "// runs node:\n" + `exec.LookPath("node")`,
		"quiet_test.go":   `exec.LookPath("git")`,
		"runs.go":         `exec.LookPath("node")`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(filesRunningNode(t, dir), ","); got != "another_test.go,runs_test.go" {
		t.Errorf("found %q, want the two test files that run node", got)
	}
	// And the real ones are among what the guard below covers.
	real := strings.Join(pageTestFiles(t), ",")
	for _, want := range []string{"review_ui_test.go", "firefox_test.go", "emoji_test.go", "markdown_test.go", "manage_ui_test.go"} {
		if !strings.Contains(real, want) {
			t.Errorf("%s isn't among the page test files found: %s", want, real)
		}
	}
}

func TestThePageTestsNeverSkipForWantOfWhatTheyNeed(t *testing.T) {
	for _, name := range pageTestFiles(t) {
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
	for _, name := range pageTestFiles(t) {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "companion", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "getTestDeps") && !strings.Contains(string(data), "nodeMissing(") {
			t.Errorf("%s doesn't get its message from the shared helper, which says to run test-deps", name)
		}
	}
}

// testDepsTree is a folder with the three scripts in it and the uitest folder they work in, and a bin folder of stand-ins for npm and
// npx that say what they were run with and where. Windows is left out: the stand-ins are shell scripts.
func testDepsTree(t *testing.T) (dir, bin, calls string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins for npm and npx are shell scripts")
	}
	dir = t.TempDir()
	for _, name := range []string{"Makefile", "make.sh", "make.ps1"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "companion", "uitest"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin = t.TempDir()
	calls = filepath.Join(bin, "calls")
	stand := "#!/bin/sh\necho \"$(basename \"$0\") $* [in $(pwd -P)]\" >>\"$FAKE_CALLS\"\n[ -n \"$FAKE_NPM_FAILS\" ] && [ \"$(basename \"$0\")\" = npm ] && exit 1\nexit 0\n"
	for _, tool := range []string{"npm", "npx"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(stand), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, bin, calls
}

type testDepsScript struct {
	name    string
	command []string
	tool    string
}

func testDepsScripts() []testDepsScript {
	return []testDepsScript{
		{"make.sh", []string{"bash", "./make.sh", "test-deps"}, "bash"},
		{"Makefile", []string{"make", "test-deps"}, "make"},
		{"make.ps1", []string{"pwsh", "-NoProfile", "-File", "./make.ps1", "test-deps"}, "pwsh"},
	}
}

func runTestDeps(t *testing.T, s testDepsScript, extraEnv ...string) (calls []string, uitest string, out string, err error) {
	t.Helper()
	if _, lookErr := exec.LookPath(s.tool); lookErr != nil {
		t.Skipf("%s isn't installed", s.tool)
	}
	dir, bin, callsFile := testDepsTree(t)
	cmd := exec.Command(s.command[0], s.command[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_CALLS="+callsFile)
	cmd.Env = append(cmd.Env, extraEnv...)
	combined, err := cmd.CombinedOutput()
	data, _ := os.ReadFile(callsFile)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	real, evalErr := filepath.EvalSymlinks(dir)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	return calls, filepath.Join(real, "internal", "companion", "uitest"), string(combined), err
}

// Each script is run, not read: its target has to do the two things, in the folder the page's tests look in, whatever the folder
// it is run from.
func TestEveryBuildScriptRunsNpmCiThenInstallsFirefoxInTheUitestFolder(t *testing.T) {
	for _, s := range testDepsScripts() {
		t.Run(s.name, func(t *testing.T) {
			calls, uitest, out, err := runTestDeps(t, s)
			if err != nil {
				t.Fatalf("test-deps failed: %v\n%s", err, out)
			}
			want := []string{"npm ci [in " + uitest + "]", "npx playwright install firefox [in " + uitest + "]"}
			if strings.Join(calls, "\n") != strings.Join(want, "\n") {
				t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

func TestAFailingNpmCiStopsTestDepsBeforeFirefoxIsFetchedAndFails(t *testing.T) {
	for _, s := range testDepsScripts() {
		t.Run(s.name, func(t *testing.T) {
			calls, _, out, err := runTestDeps(t, s, "FAKE_NPM_FAILS=1")
			if err == nil {
				t.Errorf("test-deps passed with npm ci failing:\n%s", out)
			}
			for _, call := range calls {
				if strings.HasPrefix(call, "npx") {
					t.Errorf("Firefox was fetched after npm ci failed: %v", calls)
				}
			}
		})
	}
}
