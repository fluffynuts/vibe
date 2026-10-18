package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
	"vibe/internal/termtitle"
)

// startHeadless starts sandbox name, when it isn't running. A variable for
// tests.
var startHeadless = func(ctx context.Context, name string) bool {
	return sbxrun.StartContext(ctx, name)
}

// headless is --headless's session: the sandbox started, its start-up run
// and its companion page served, with no terminal attached to it. It goes on
// until Ctrl-C (or SIGTERM), or another vibe asks it to stop. The page's
// Start, and its rebuilds, bring the sandbox back as they do for a terminal
// session; its start-up is run again each time. When it ends, the agent's
// memories are copied out, and the sandbox is left as it is.
func headless(lay layout.Layout, args cliargs.Args, name string) error {
	vibeHome := lay.Home
	if args.NoCompanion {
		return fmt.Errorf("--headless is the companion page with no terminal: it can't go with -N/--no-companion")
	}
	checkGitTokenExpiry(vibeHome, args, name)
	stopCompanion, serving, cancelThePage := startCompanion(lay, args, name)
	if !serving {
		stopCompanion()
		return fmt.Errorf("--headless is the companion page with no terminal, and the page isn't being served: " +
			"'companion: off' in settings.yaml turns it off, or what's said above is why it couldn't be")
	}
	// A stop request from another vibe is this loop's to act on, so the
	// agent's memories are saved first, rather than the folder lock's,
	// which exits at once.
	sessionMu.Lock()
	inSession = true
	sessionMu.Unlock()
	// So 'vibe --stop' knows to end this vibe too: nobody would see it
	// carry on without its sandbox.
	if inst, found, err := state.Load(vibeHome, name); err == nil && found && session != nil {
		if err := session.MarkHeadless(inst.Target); err != nil {
			note("WARNING: couldn't record this vibe as headless, so 'vibe --stop' will leave it running: %s", err)
		}
	}
	defer stopCompanion()
	setPageCancel(cancelThePage)
	defer setPageCancel(nil)

	note("starting sandbox '%s'", name)
	if !startHeadless(context.Background(), name) {
		return fmt.Errorf("sandbox '%s' didn't start — 'sbx ls' says what state it is in", name)
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	note("sandbox '%s' is running, with no terminal attached: the companion page above is where to work with it", name)
	if detachedChild() {
		note("  'vibe --stop' ends this vibe and stops the sandbox")
	} else {
		note("  Ctrl-C ends vibe and leaves the sandbox running; 'vibe --stop' ends vibe and stops the sandbox")
	}
	announceHeadlessReady(companionURL(vibeHome, name))

	// Held for good: the page's Start (and a rebuild's) releases the hold,
	// which is how this hears the sandbox is back.
	hold.begin()
	asked := false
	for {
		go nudgeOnStart(lay, name, func() {})
		// Each time, as a rebuild can leave it logged out.
		go warnIfAgentLoggedOut(name, recordedAgent(vibeHome, name, "claude"))
		select {
		case <-hold.waiting():
			hold.begin()
			note("sandbox '%s' is back: running its start-up again", name)
			continue
		case <-session.StopRequested():
			asked = true
			note("asked to stop by another vibe for this folder")
		case <-interrupts:
		}
		break
	}
	backUpAsHeadlessEnds(vibeHome, name, asked)
	termtitle.Restore()
	return nil
}

// backUpAsHeadlessEnds backs up the agent's memories as a headless vibe
// ends: from a sandbox sbx has stopped (left idle) by waking it, and
// leaving it stopped (see backUpWhileStopped); from one that's up, leaving
// it running. Asked to stop, it's for whoever asked to say what becomes of
// the sandbox ('vibe --stop' stops it next).
func backUpAsHeadlessEnds(vibeHome, name string, asked bool) {
	if running, known := sandboxRunning(name); known && !running {
		backUpWhileStopped(vibeHome, name)
		return
	}
	if !sandboxReachable(name) {
		return
	}
	backUpMemories(vibeHome, name)
	if !asked {
		note("left sandbox '%s' running — 'vibe --stop' stops it", name)
	}
}
