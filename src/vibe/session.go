package main

import (
	"context"
	"sync/atomic"

	"path/filepath"
	"strings"

	"os"
	"runtime"

	"vibe/internal/cliargs"
	"vibe/internal/clockskew"
	"vibe/internal/instructions"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
	"vibe/internal/termtitle"
)

// finish nudges the on-start script (idempotent — safe on every attach, and
// needed because setup.startup does not fire on a sandbox's very first boot,
// before the launcher is on disk) then hands off to `sbx run` in the
// foreground.
func finish(lay layout.Layout, args cliargs.Args, name string) error {
	vibeHome := lay.Home
	if args.NoStart {
		// A rebuild going on to headless has started it, not stopped short.
		if !args.Force && !args.Headless {
			note("type vibe to get started")
		}
		return nil
	}
	if args.Headless {
		return headless(lay, args, name)
	}
	checkGitTokenExpiry(vibeHome, args, name)
	checkSandboxGitAuthor(lay, args, name)
	remindOfInstructions(lay, name)
	stopCompanion, companionUp, cancelThePage := startCompanion(lay, args, name)
	// 'vibe --cancel' from another terminal ends what the page has under way.
	setPageCancel(cancelThePage)
	defer setPageCancel(nil)
	noteUpgrades := checkUpgradesInBackground()
	sessionMu.Lock()
	inSession = true
	sessionMu.Unlock()
	// The companion page's Admin tab can stop or rebuild the sandbox under
	// the session: then the session ends, but vibe (and the page) carries
	// on, attaching again once the sandbox is back.
	var code int
	var err error
	left := false
	for {
		// Set once the sandbox has answered during this attach: a session
		// ending after that is the sandbox going away under it, where one
		// ending before never had a sandbox to lose.
		var reached atomic.Bool
		go nudgeOnStart(lay, name, func() { reached.Store(true) })
		setAttached(true)
		code, err = sbxrun.Run(name, session.StopRequested())
		setAttached(false)
		if err != nil || stopAsked() {
			break
		}
		resume := hold.waiting()
		switch {
		case resume != nil:
			note("the sandbox was stopped or is being rebuilt from the companion page, which carries on;")
		case companionUp && stoppedUnderSession(name, code, reached.Load()):
			// Stopped by sbx (as one left idle) or with sbx directly: the
			// page's Admin tab can start it again.
			stoppedOnPurpose.Store(true)
			hold.begin()
			resume = hold.waiting()
			note("sandbox '%s' was stopped — the companion page's Admin tab can start it again;", name)
		case headlessHere != nil && stoppedUnderSession(name, code, reached.Load()):
			// The page is the headless vibe's, so this terminal has no
			// hold to wait in.
			note("sandbox '%s' was stopped — the companion page's Admin tab can start it again, then 'vibe' here attaches to it", name)
			left = true
		}
		if resume == nil {
			break
		}
		note("  this terminal attaches again once the sandbox is back (Ctrl-C ends vibe)")
		if !awaitResume(resume, session.StopRequested()) {
			// A rebuild under way leaves no sandbox fit to wake.
			if stoppedOnPurpose.Load() {
				backUpWhileStopped(vibeHome, name)
			}
			left = true
			break
		}
		note("attaching to '%s' again", name)
	}
	stopCompanion()
	noteKeptRules(vibeHome, name)
	if err != nil {
		return err
	}
	if left {
		// Any memories to copy out of the sandbox have been already.
		termtitle.Restore()
		session.Release()
		os.Exit(0)
	}
	stopped := false
	if stopAsked() {
		stopped = true
		note("session ended: asked to stop by another vibe for this folder")
	}
	// A session we ended ourselves exits non-zero, but its sandbox is fine
	// to copy from; any other non-zero exit means sbx failed, and the
	// sandbox is likely unreachable.
	if code == 0 || stopped {
		saveMemoriesOnExit(vibeHome, name)
	} else {
		note("cannot copy memories: sbx exited with code %d", code)
		note("%s", sbxFailureHint)
		warnIfClockIsOff()
		if len(runningProcesses(runtime.GOOS, virtualBoxProcesses)) > 0 {
			note("%s", virtualBoxHint)
		}
	}
	noteUpgrades()
	termtitle.Restore()
	session.Release()
	os.Exit(code)
	return nil
}

// backUpWhileStopped backs up the agent's memories from a sandbox stopped
// under its session (by sbx, as one left idle, or from the page) as vibe
// ends: they are still in it, and the host's copy is what a rebuild or a
// delete keeps. It is started for the backup, then stopped again.
func backUpWhileStopped(vibeHome, name string) {
	note("waking sandbox '%s' to back up its agent's memories, then stopping it again", name)
	session.Report("waking its sandbox, to back up its agent's memories")
	if !startToBackUp(context.Background(), name) {
		note("sandbox '%s' didn't start, so its agent's memories weren't backed up: they're still in it, and are backed up when a session in it next ends", name)
		return
	}
	backUpMemories(vibeHome, name)
	session.Report("stopping its sandbox again")
	if err := stopAfterBackUp(context.Background(), name); err != nil {
		note("couldn't stop sandbox '%s' again: %s — 'vibe --stop' stops it", name, oneLine(err.Error()))
	}
}

// What backUpWhileStopped and backUpAsHeadlessEnds ask of sbx and the
// memory store: variables for tests.
var (
	startToBackUp    = sbxrun.Start
	backUpMemories   = saveMemoriesOnExit
	stopAfterBackUp  = sbxrun.StopQuiet
	sandboxReachable = sbxrun.Reachable
	sandboxRunning   = func(name string) (running, known bool) { return sbxrun.Running(context.Background(), name) }
)

// stoppedUnderSession says a session that ended with code ended because its
// sandbox was stopped (or removed) from outside it — by sbx itself, say —
// rather than the agent being quit (which ends without an error) or sbx
// failing to start it (when it was never reached).
func stoppedUnderSession(name string, code int, reached bool) bool {
	if code == 0 || !reached {
		return false
	}
	statuses, err := sbxrun.List(context.Background())
	if err != nil {
		return false
	}
	for _, st := range statuses {
		if st.Name == name {
			return !st.Running
		}
	}
	return true
}

// warnIfClockIsOff, run when sbx has failed, says so if this machine's clock
// is off the real time: sbx's sign-in to Docker then fails with only "token
// has invalid claims: token is expired" to go on. A check that can't be made
// says nothing — it's only ever a hint.
func warnIfClockIsOff() {
	skew, err := clockskew.Skew()
	if err != nil {
		return
	}
	if w := clockskew.Warning(skew); w != "" {
		note("WARNING: %s", w)
	}
}

// remindOfInstructions says, as the session starts, where the user's
// instructions to the agent are: a tip for each that is still blank, so the
// files are found, and one short line once they have something in them.
func remindOfInstructions(lay layout.Layout, name string) {
	type file struct{ path, forWhom string }
	files := []file{{lay.HomePath(instructions.File), "every sandbox"}}
	if inst, found, err := state.Load(lay.Home, name); err == nil && found {
		files = append(files, file{filepath.Join(lay.ProfileDir(inst.Profile), instructions.File), "this profile's sandboxes"})
	}
	var used []string
	for _, f := range files {
		if f.path == "" {
			continue
		}
		if data, err := os.ReadFile(f.path); err == nil && strings.TrimSpace(string(data)) != "" {
			used = append(used, pathresolve.Tilde(f.path))
			continue
		}
		note("tip: instructions to the agent for %s go in %s (empty so far; the companion page's Admin tab edits it too)",
			f.forWhom, pathresolve.Tilde(f.path))
	}
	if len(used) > 0 {
		note("agent instructions: %s", strings.Join(used, ", "))
	}
}
