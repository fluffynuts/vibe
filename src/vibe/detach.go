package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// headlessChildEnv marks the copy of vibe that --headless leaves running in
// the background: it says it's ready on its stdout, and logs everything else.
const headlessChildEnv = "VIBE_HEADLESS_DETACHED"

// readyPrefix starts the line the background vibe sends once its page is
// up and its sandbox started, ahead of the page's URL.
const readyPrefix = "vibe-headless-ready "

// headlessReadyWithin is a backstop on waiting for the background vibe: its
// sandbox's start and start-up normally take seconds, a rebuild minutes.
// Past it, it's left to carry on, and said so.
const headlessReadyWithin = 5 * time.Minute

// detachedChild says this is the background copy --headless started.
func detachedChild() bool {
	return os.Getenv(headlessChildEnv) == "1"
}

// headlessLog is where a background headless vibe for sandbox name logs.
func headlessLog(vibeHome, name string) string {
	return filepath.Join(vibeHome, "logs", name+".log")
}

// announceHeadlessReady tells the vibe that started this one in the
// background that its page (at url) is up, and lets it go; from then on,
// what would go to stdout goes to the log. Not a background vibe, it does
// nothing.
func announceHeadlessReady(url string) {
	if !detachedChild() {
		return
	}
	fmt.Fprintln(os.Stdout, readyPrefix+url)
	os.Stdout.Close()
	os.Stdout = os.Stderr
}

// noSandboxForHeadless says what to do when there's no sandbox to start.
func noSandboxForHeadless(name, target string) error {
	return fmt.Errorf("there's no sandbox '%s' for %s yet, and --headless only starts one that exists: run 'vibe' there once first, to create it (it asks a few things), then --headless", name, target)
}

// companionURL is where sandbox name's companion page is, as it was last
// served; "" when that isn't known.
func companionURL(vibeHome, name string) string {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found || inst.CompanionPort == 0 {
		return ""
	}
	token, err := state.CompanionToken(vibeHome, name)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d/#%s", inst.CompanionPort, token)
}

// detachHeadless is --headless without --foreground. What needs the
// terminal is done here first (another vibe in the folder, the countdown to
// rebuilding an out-of-date sandbox); then a copy of vibe is started in the
// background to be the headless session, logging to ~/.vibe/logs, and this
// waits for it to say its page is up, prints where, and returns the
// terminal. One already running headless for the folder is simply pointed
// at.
func detachHeadless(lay layout.Layout, args cliargs.Args, name, target string) error {
	vibeHome := lay.Home
	if !sbxrun.Exists(name) {
		return noSandboxForHeadless(name, target)
	}
	for _, c := range running.Claims(vibeHome, target) {
		if c.Headless {
			note("vibe is already running headless for %s (PID %d) — its page: %s", target, c.PID, companionURL(vibeHome, name))
			note("  its log: %s; 'vibe --stop %s' stops it", displayPath(headlessLog(vibeHome, name)), target)
			return nil
		}
	}
	if err := guardFolder(vibeHome, target, args.Force, false); err != nil {
		return err
	}
	if !args.NoAutoUpdate {
		if err := autoUpdate(lay, args, name, target); err != nil {
			session.Release()
			return err
		}
	}
	// The background vibe takes the folder itself.
	session.Release()
	session = nil

	logPath := headlessLog(vibeHome, name)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("couldn't start a log for the background vibe: %w", err)
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "=== vibe --headless for %s, started %s ===\n", target, time.Now().Format("2006-01-02 15:04:05"))

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	childArgs := []string{"--headless", "--no-auto-update"}
	if args.Name != "" {
		childArgs = append(childArgs, "--name", args.Name)
	}
	if args.Force {
		childArgs = append(childArgs, "--force")
	}
	childArgs = append(childArgs, "--", target)
	cmd := exec.Command(exe, childArgs...)
	cmd.Env = append(os.Environ(), headlessChildEnv+"=1")
	cmd.Stderr = logFile
	detach(cmd)
	ready, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't start vibe in the background: %w", err)
	}
	pid := cmd.Process.Pid
	// It runs on its own from here: nothing waits for it to exit.
	defer cmd.Process.Release()

	status := newStatusLine()
	status.show("starting vibe for sandbox '%s' in the background (log: %s)", name, displayPath(logPath))
	url, err := awaitHeadlessReady(ready, headlessReadyWithin)
	switch {
	case errors.Is(err, errStillStarting):
		status.done("the background vibe (PID %d) hasn't said its page is up after %s — it carries on; the log says how it's going: %s", pid, headlessReadyWithin, displayPath(logPath))
		return nil
	case err != nil:
		status.done("the background vibe ended before its page was up; the end of its log (%s):", displayPath(logPath))
		for _, line := range lastLines(logPath, 12) {
			note("  %s", line)
		}
		return fmt.Errorf("--headless didn't start")
	}
	status.done("sandbox '%s' is running in the background (vibe PID %d)", name, pid)
	note("  its page: %s", url)
	note("  its log: %s; 'vibe --stop %s' stops it", displayPath(logPath), target)
	note("  'vibe --info %s' says how to reach the page from another machine", target)
	return nil
}

var (
	errStillStarting = errors.New("still starting")
	errEndedEarly    = errors.New("ended before it was ready")
)

// awaitHeadlessReady waits for the background vibe to say, on r, that its
// page is up, and returns the page's URL; errEndedEarly when r ends first,
// errStillStarting when within passes first.
func awaitHeadlessReady(r io.Reader, within time.Duration) (string, error) {
	found := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if url, ok := strings.CutPrefix(scanner.Text(), readyPrefix); ok {
				found <- url
				return
			}
		}
		close(found)
	}()
	select {
	case url, ok := <-found:
		if !ok {
			return "", errEndedEarly
		}
		return url, nil
	case <-time.After(within):
		return "", errStillStarting
	}
}

// lastLines is the last n lines of the file at path.
func lastLines(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"(couldn't read it: " + err.Error() + ")"}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
