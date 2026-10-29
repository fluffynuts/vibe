package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/library"
	"vibe/internal/profilegen"
)

// The playwright feature's install script is run as it is, with stand-ins for npm, playwright and node on its PATH:
// what it asks them for, when, and what it does when they fail, without the network. The real run, against the real
// downloads, is a different kind of test (see docs/plan-playwright.md).

func playwrightScriptPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "library", "playwright", "install-scripts", "01-install-playwright")
}

const fakeNpm = `#!/bin/sh
echo "npm $*" >> "$FAKE_CALLS"
case "$1 $2" in
  "prefix -g") echo "$FAKE_PREFIX"; exit 0 ;;
  "root -g") echo "$FAKE_PREFIX/lib/node_modules"; exit 0 ;;
esac
if [ "$1" = install ]; then
  if [ -n "$FAKE_NPM_FAILS" ]; then echo "npm ERR! it didn't work" >&2; exit 1; fi
  mkdir -p "$FAKE_PREFIX/bin" "$FAKE_PREFIX/lib/node_modules/playwright"
  cp "$FAKE_PLAYWRIGHT" "$FAKE_PREFIX/bin/playwright"
fi
`

// fakePlaywright records each call with where it was told to put browsers, and spans (when each began and ended, in
// nanoseconds) for the two that take a while, so that a test can see whether they overlapped.
const fakePlaywright = `#!/bin/sh
echo "playwright $* [browsers: $PLAYWRIGHT_BROWSERS_PATH] [skip-validate: $PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS]" >> "$FAKE_CALLS"
now() { date +%s%N; }
case "$1" in
  --version) echo "Version 9.9.9"; exit 0 ;;
  install-deps)
    start=$(now); sleep 0.4; echo "deps $start $(now)" >> "$FAKE_SPANS"
    if [ -n "$FAKE_DEPS_FAIL" ]; then echo "E: apt said no" >&2; exit 1; fi
    ;;
  install)
    start=$(now); sleep 0.4
    shift
    for b in "$@"; do mkdir -p "$PLAYWRIGHT_BROWSERS_PATH/$b-1"; done
    echo "install $start $(now)" >> "$FAKE_SPANS"
    if [ -n "$FAKE_INSTALL_FAIL" ]; then echo "Error: download failed" >&2; exit 1; fi
    ;;
esac
exit 0
`

// With FAKE_ROOT set, the script is root, and the agent is a user called agent: id says so, chown does nothing, and su
// runs what it is given with FAKE_AS saying as whom. Otherwise they are the real ones.
const fakeID = `#!/bin/sh
if [ -n "$FAKE_ROOT" ]; then
  case "$*" in
    "-u") echo 0 ;;
    "-un 1000") echo agent ;;
    *) exec REAL_ID "$@" ;;
  esac
  exit 0
fi
exec REAL_ID "$@"
`

const fakeChown = `#!/bin/sh
if [ -n "$FAKE_ROOT" ]; then echo "chown $*" >> "$FAKE_CALLS"; exit 0; fi
exec REAL_CHOWN "$@"
`

const fakeSu = `#!/bin/sh
echo "su $1 $2 [command: $3]" >> "$FAKE_CALLS"
FAKE_AS="$1" exec sh -c "$3"
`

const fakeNode = `#!/bin/sh
echo "node $* [NODE_PATH: $NODE_PATH] [browsers: $PLAYWRIGHT_BROWSERS_PATH] [as: $FAKE_AS]" >> "$FAKE_CALLS"
if [ -n "$FAKE_LAUNCH_FAILS" ]; then
  echo "browserType.launch: Failed to launch the browser process!" >&2
  i=1; while [ "$i" -le 20 ]; do echo "  - line $i of what the browser said" >&2; i=$((i + 1)); done
  exit 1
fi
echo "chromium 1.2.3, firefox 4.5.6"
`

// playwrightRun is one run of the install script.
type playwrightRun struct {
	log, calls, browsers, prefix string
	spans                        map[string][2]int64
	exit                         error
}

func (r playwrightRun) called(what string) bool { return strings.Contains(r.calls, what) }

// runPlaywrightInstall runs the script with env set for the stand-ins (FAKE_… to make one fail), and without npm
// at all when noNpm.
func runPlaywrightInstall(t *testing.T, noNpm bool, env ...string) playwrightRun {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the script is for a Linux sandbox")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakebin")
	os.MkdirAll(bin, 0o755)
	realID, _ := exec.LookPath("id")
	realChown, _ := exec.LookPath("chown")
	stand := map[string]string{"playwright-source": fakePlaywright, "node": fakeNode, "su": fakeSu,
		"id":    strings.ReplaceAll(fakeID, "REAL_ID", realID),
		"chown": strings.ReplaceAll(fakeChown, "REAL_CHOWN", realChown)}
	if !noNpm {
		stand["npm"] = fakeNpm
	}
	for name, body := range stand {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Only the tools the script and the stand-ins use, so that a real npm, node or playwright is never found.
	for _, tool := range []string{"sh", "date", "chmod", "mkdir", "mktemp", "cat", "sed", "head", "rm", "sleep", "cp", "dirname"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s isn't installed: the script needs it", tool)
		}
		os.Symlink(path, filepath.Join(bin, tool))
	}
	run := playwrightRun{browsers: filepath.Join(dir, "opt", "ms-playwright"), prefix: filepath.Join(dir, "npm-global")}
	run.calls = filepath.Join(dir, "calls")
	spans := filepath.Join(dir, "spans")
	logFile := filepath.Join(dir, "setup.log")
	cmd := exec.Command("sh", playwrightScriptPath(t))
	cmd.Env = append(os.Environ(),
		"PATH="+bin,
		"FAKE_CALLS="+run.calls, "FAKE_SPANS="+spans, "FAKE_PREFIX="+run.prefix, "FAKE_PLAYWRIGHT="+filepath.Join(bin, "playwright-source"),
		"PW_BROWSERS_PATH="+run.browsers, "PW_LOG="+logFile)
	cmd.Env = append(cmd.Env, env...)
	run.exit = cmd.Run()
	calls, _ := os.ReadFile(run.calls)
	run.calls = string(calls)
	log, _ := os.ReadFile(logFile)
	run.log = string(log)
	run.spans = map[string][2]int64{}
	if data, err := os.ReadFile(spans); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var name string
			var a, b int64
			if _, err := fmt.Sscanf(line, "%s %d %d", &name, &a, &b); err == nil {
				run.spans[name] = [2]int64{a, b}
			}
		}
	}
	return run
}

func TestPlaywrightIsInstalledGloballyLatestWithBothBrowsersAndTheirLibraries(t *testing.T) {
	run := runPlaywrightInstall(t, false)
	if run.exit != nil {
		t.Fatalf("the script failed: %v\n%s", run.exit, run.log)
	}
	for _, want := range []string{"npm install -g playwright@latest", "playwright install-deps chromium-headless-shell firefox", "playwright install chromium-headless-shell firefox"} {
		if !run.called(want) {
			t.Errorf("never ran %q; calls:\n%s", want, run.calls)
		}
	}
	for _, browser := range []string{"chromium-headless-shell-1", "firefox-1"} {
		if _, err := os.Stat(filepath.Join(run.browsers, browser)); err != nil {
			t.Errorf("%s isn't in the browsers folder: %v", browser, err)
		}
	}
	if !run.called("[browsers: " + run.browsers + "]") {
		t.Errorf("the browsers weren't asked for in the shared folder:\n%s", run.calls)
	}
	if !strings.Contains(run.log, "installed Playwright 9.9.9") || !strings.Contains(run.log, "chromium 1.2.3") || !strings.Contains(run.log, "firefox 4.5.6") {
		t.Errorf("the log doesn't say what was installed and launched:\n%s\ncalls:\n%s", run.log, run.calls)
	}
}

func TestTheLibrariesAreFetchedWhileTheBrowsersAre(t *testing.T) {
	// apt is by far the slowest part (some 90 s of the 2 minutes), and shares nothing with the downloads.
	run := runPlaywrightInstall(t, false)
	deps, install := run.spans["deps"], run.spans["install"]
	if deps == [2]int64{} || install == [2]int64{} {
		t.Fatalf("spans: %v", run.spans)
	}
	if !(deps[0] < install[1] && install[0] < deps[1]) {
		t.Errorf("libraries %v and browsers %v didn't overlap: they were fetched one after the other", deps, install)
	}
}

func TestEveryoneCanWriteWhereTheBrowsersAreSoAProjectsOwnVersionCanBeAddedByTheAgent(t *testing.T) {
	run := runPlaywrightInstall(t, false)
	info, err := os.Stat(run.browsers)
	if err != nil {
		t.Fatal(err)
	}
	if !ownedBy1000(info) && info.Mode().Perm()&0o022 == 0 {
		t.Errorf("the folder is %v: the agent, not root, couldn't add a revision to it", info.Mode())
	}
	if info.Mode().Perm()&0o005 != 0o005 {
		t.Errorf("the folder is %v: not readable by all", info.Mode())
	}
}

func TestADepsFailureIsSaidAndTheBrowsersAreStillGot(t *testing.T) {
	run := runPlaywrightInstall(t, false, "FAKE_DEPS_FAIL=1")
	if run.exit != nil {
		t.Errorf("a sandbox's setup is not to fail because of it: %v", run.exit)
	}
	if _, err := os.Stat(filepath.Join(run.browsers, "firefox-1")); err != nil {
		t.Errorf("the browsers weren't got for want of libraries: %v", err)
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "libraries") || !strings.Contains(run.log, "apt said no") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestABrowserDownloadFailureIsSaidAndTheLibrariesAreStillGot(t *testing.T) {
	run := runPlaywrightInstall(t, false, "FAKE_INSTALL_FAIL=1")
	if run.exit != nil {
		t.Errorf("a sandbox's setup is not to fail because of it: %v", run.exit)
	}
	if _, ok := run.spans["deps"]; !ok {
		t.Errorf("the libraries weren't got for want of browsers:\n%s", run.calls)
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "browsers") || !strings.Contains(run.log, "download failed") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestAnInstallThatDoesntLaunchIsSaid(t *testing.T) {
	run := runPlaywrightInstall(t, false, "FAKE_LAUNCH_FAILS=1")
	if run.exit != nil {
		t.Errorf("the script failed: %v", run.exit)
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "won't launch") || !strings.Contains(run.log, "Failed to launch the browser process!") {
		t.Errorf("the log doesn't say that, and why:\n%s", run.log)
	}
	// The cause is not in the first line: what the browser itself said follows it.
	if !strings.Contains(run.log, "line 14 of what the browser said") {
		t.Errorf("the log has only the start of what the browser said:\n%s", run.log)
	}
	if strings.Contains(run.log, "line 20 of what the browser said") {
		t.Errorf("the log has all of a long error, which it is not to:\n%s", run.log)
	}
	if strings.Contains(run.log, "installed Playwright") {
		t.Errorf("it said it was installed:\n%s", run.log)
	}
}

func TestNoNpmIsSaidAndNothingIsRun(t *testing.T) {
	run := runPlaywrightInstall(t, true)
	if run.exit != nil {
		t.Errorf("the script failed: %v", run.exit)
	}
	if run.calls != "" {
		t.Errorf("something was run without npm:\n%s", run.calls)
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "npm") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestANpmThatFailsIsSaidAndNothingElseIsRun(t *testing.T) {
	run := runPlaywrightInstall(t, false, "FAKE_NPM_FAILS=1")
	if run.exit != nil {
		t.Errorf("the script failed: %v", run.exit)
	}
	if run.called("playwright install") {
		t.Errorf("browsers were asked for without Playwright:\n%s", run.calls)
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "it didn't work") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestThePlaywrightFeatureIsOfferedSaysItTakesTimeAndComposesWithWhatItNeeds(t *testing.T) {
	lay := layout.New(t.TempDir(), repoRoot(t))
	var found *library.Feature
	for _, f := range library.List(lay.Features(), lay.FeatureDir) {
		if f.Name == "playwright" {
			f := f
			found = &f
		}
	}
	if found == nil || found.Description == "" {
		t.Fatalf("playwright in the library: %+v", found)
	}
	if !strings.Contains(found.Description, "time") {
		t.Errorf("the description doesn't say it adds time to building the sandbox: %q", found.Description)
	}
	dir, err := profilegen.CreateGuided(lay, "p", []string{"playwright"})
	if err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	for _, want := range []string{"cdn.playwright.dev", "playwright.download.prss.microsoft.com", "registry.npmjs.org", "archive.ubuntu.com", "PLAYWRIGHT_BROWSERS_PATH: /opt/ms-playwright"} {
		if !strings.Contains(string(config), want) {
			t.Errorf("config.yaml lacks %s:\n%s", want, config)
		}
	}
	scripts, _ := filepath.Glob(filepath.Join(dir, "install-scripts", "*-install-playwright"))
	if len(scripts) != 1 {
		t.Errorf("install scripts for playwright: %v", scripts)
	}
	kit, _ := os.ReadFile(filepath.Join(dir, "kit-instructions.md"))
	// The full Chromium is a large download an agent would otherwise fetch for a headed run that a headless one does as well.
	for _, want := range []string{"Playwright", "firefox", "headless", "/tmp/sbx-setup.log", "about 190 MB", "Prefer the installed"} {
		if !strings.Contains(string(kit), want) {
			t.Errorf("the agent isn't told %q:\n%s", want, kit)
		}
	}
}

func TestTheScriptNamesTheHostsItIsGivenInTheConfig(t *testing.T) {
	// What the script downloads from and what the sandbox may reach are two files, which would drift.
	config, _ := os.ReadFile(filepath.Join(repoRoot(t), "library", "playwright", "config.yaml"))
	script, _ := os.ReadFile(playwrightScriptPath(t))
	for _, host := range []string{"cdn.playwright.dev"} {
		if !strings.Contains(string(config), host) {
			t.Errorf("%s isn't in config.yaml", host)
		}
	}
	if !strings.Contains(string(script), "PW_BROWSERS_PATH:-/opt/ms-playwright") || !strings.Contains(string(config), "PLAYWRIGHT_BROWSERS_PATH: /opt/ms-playwright") {
		t.Errorf("the folder the script puts browsers in and the one the environment points to differ (%s)", strconv.Quote("/opt/ms-playwright"))
	}
}

func TestTheLaunchCheckRunsAsTheAgentWhenTheScriptIsRootBecauseFirefoxWontRunAsRootInAnotherUsersSession(t *testing.T) {
	// A sandbox's build runs the script as root, with the agent's HOME and XDG_RUNTIME_DIR: Firefox refuses to start
	// so ("Running Nightly as root in a regular user's session is not supported"), though it starts for the agent.
	run := runPlaywrightInstall(t, false, "FAKE_ROOT=1")
	if run.exit != nil {
		t.Fatalf("the script failed: %v\n%s", run.exit, run.log)
	}
	if !run.called("su agent -c") {
		t.Fatalf("the check wasn't run as the agent:\n%s", run.calls)
	}
	if !run.called("[as: agent]") {
		t.Errorf("node wasn't run through su:\n%s", run.calls)
	}
	if !strings.Contains(run.log, "launched headless") {
		t.Errorf("the log doesn't say it launched:\n%s", run.log)
	}
	if !run.called("chown -R agent:") {
		t.Errorf("the browsers folder wasn't given to the agent:\n%s", run.calls)
	}
}

func TestTheLaunchCheckIsRunDirectlyWhenTheScriptIsNotRoot(t *testing.T) {
	run := runPlaywrightInstall(t, false)
	if run.called("su ") {
		t.Errorf("su was used by a script that isn't root, which would ask for a password:\n%s", run.calls)
	}
	if !run.called("[as: ]") {
		t.Errorf("node wasn't run directly:\n%s", run.calls)
	}
}

func TestTheBrowserDownloadDoesNotCheckForLibrariesThatAreBeingInstalledAtTheSameTime(t *testing.T) {
	// It would warn, at the end of every build, that the host is missing what apt is still fetching.
	run := runPlaywrightInstall(t, false)
	if !run.called("playwright install chromium-headless-shell firefox [browsers: " + run.browsers + "] [skip-validate: 1]") {
		t.Errorf("the download validated the host:\n%s", run.calls)
	}
}
