package main

import (
	"os"
	"runtime"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/clockskew"
	"vibe/internal/layout"
	"vibe/internal/sbxrun"
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
	stopCompanion, companionUp := startCompanion(lay, args, name)
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
		go nudgeOnStart(vibeHome, name)
		began := time.Now()
		code, err = sbxrun.Run(name, session.StopRequested())
		if err != nil || stopAsked() {
			break
		}
		resume := hold.waiting()
		switch {
		case resume != nil:
			note("the sandbox was stopped or is being rebuilt from the companion page, which carries on;")
		case stoppedUnderSession(name, code, time.Since(began)) && companionUp:
			// Stopped by 'vibe --stop' or sbx: the page's Admin tab can
			// start it again.
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

// sessionLasted is how long a session must have run for its ending in an
// error to be taken for the sandbox being stopped under it, rather than for
// sbx failing to start it.
const sessionLasted = 10 * time.Second

// stoppedUnderSession says a session that ended with code after lasting
// ran ended because its sandbox was stopped (or removed) from outside: by
// 'vibe --stop', or sbx itself. An agent quit as usual ends without an
// error, and one sbx couldn't start fails at once.
func stoppedUnderSession(name string, code int, ran time.Duration) bool {
	if code == 0 || ran < sessionLasted {
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
