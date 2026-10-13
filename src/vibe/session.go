package main

import (
	"sync/atomic"

	"path/filepath"
	"strings"

	"os"
	"runtime"

	"vibe/internal/cliargs"
	"vibe/internal/clockskew"
	"vibe/internal/instructions"
	"vibe/internal/layout"
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
		if !args.Force {
			note("type vibe to get started")
		}
		return nil
	}
	checkGitTokenExpiry(vibeHome, args, name)
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
		case companionUp && (session.HoldAsked() || stoppedUnderSession(name, code, reached.Load())):
			// Stopped by 'vibe --stop' (which says so) or by sbx: the
			// page's Admin tab can start it again.
			hold.begin()
			resume = hold.waiting()
			note("sandbox '%s' was stopped — the companion page's Admin tab can start it again;", name)
		}
		if resume == nil {
			break
		}
		note("  this terminal attaches again once the sandbox is back (Ctrl-C ends vibe)")
		if !awaitResume(resume, session.StopRequested()) {
			left = true
			break
		}
		note("attaching to '%s' again", name)
	}
	stopCompanion()
	if err != nil {
		return err
	}
	if left {
		// The sandbox is stopped or half-rebuilt: there are no memories
		// to copy out of it.
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

// stoppedUnderSession says a session that ended with code ended because its
// sandbox was stopped (or removed) from outside it — by sbx itself, say —
// rather than the agent being quit (which ends without an error) or sbx
// failing to start it (when it was never reached).
func stoppedUnderSession(name string, code int, reached bool) bool {
	if code == 0 || !reached {
		return false
	}
	statuses, err := sbxrun.List()
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
			used = append(used, displayPath(f.path))
			continue
		}
		note("tip: instructions to the agent for %s go in %s (empty so far; the companion page's Admin tab edits it too)",
			f.forWhom, displayPath(f.path))
	}
	if len(used) > 0 {
		note("agent instructions: %s", strings.Join(used, ", "))
	}
}
