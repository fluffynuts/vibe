package main

import (
	"fmt"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/settings"
)

// runMode is how a vibe has its sandbox: with a terminal attached, or
// headless, serving the companion page alone.
type runMode int

const (
	modeNone runMode = iota
	modeTerminal
	modeHeadless
)

func (m runMode) String() string {
	switch m {
	case modeTerminal:
		return "terminal"
	case modeHeadless:
		return "headless"
	}
	return "none"
}

// modeOf is the mode the vibes in claims were in: a terminal's if one of them
// had a terminal (that's the session someone was in), else headless, else
// none.
func modeOf(claims []running.Claim) runMode {
	mode := modeNone
	for _, c := range claims {
		switch {
		case c.Work != "":
		case !c.Headless:
			return modeTerminal
		default:
			mode = modeHeadless
		}
	}
	return mode
}

// favouriteMode is the mode the user's settings.yaml says they like vibe to
// start in ("headless" or "terminal"), or "" if it says none. It's the user's
// own preference, so it's read from ~/.vibe/settings.yaml, not a profile's.
func favouriteMode(lay layout.Layout) (string, error) {
	path := lay.File("settings.yaml")
	user, err := settings.Load(path)
	if err != nil {
		return "", err
	}
	switch user.Mode {
	case "", "headless", "terminal":
		return user.Mode, nil
	}
	return "", fmt.Errorf("mode in %s is %q: it must be %q (start without a terminal, serving the companion page) or %q", pathresolve.Tilde(path), user.Mode, "headless", "terminal")
}

// modeWanted is what decides which mode vibe starts in.
type modeWanted struct {
	// Headless and Terminal are what was typed (--headless, --terminal);
	// Restart is --restart, which ended a vibe that was in mode Ended.
	Headless, Terminal, Restart bool
	Ended                       runMode
	// Rebuilds says a rebuild is asked for, which makes the sandbox itself;
	// Deleting that none is started at all.
	Rebuilds, Deleting bool
	// Favourite is favouriteMode, and SandboxExists whether there's a
	// sandbox to start without a terminal to make it.
	Favourite     string
	SandboxExists bool
}

// resolve says whether to start headless, and what the user is to be told,
// if anything. In order: what was typed; for --restart, the mode of the vibe
// it ended (so it doesn't have to be typed again); the favourite in the
// settings; a terminal.
func (w modeWanted) resolve() (headless bool, says string) {
	if w.Deleting {
		return false, ""
	}
	switch {
	case w.Headless:
		if w.Restart && w.Ended == modeHeadless {
			says = `--headless isn't needed with --restart: it starts in the mode it ended, headless, so "vibe --restart" is enough`
		}
		return true, says
	case w.Terminal:
		if w.Restart && w.Ended == modeTerminal {
			says = `--terminal isn't needed with --restart: it starts in the mode it ended, a terminal, so "vibe --restart" is enough`
		}
		return false, says
	case w.Restart && w.Ended != modeNone:
		return w.Ended == modeHeadless, ""
	case w.Favourite == "headless":
		if !w.SandboxExists && !w.Rebuilds {
			return false, `there's no sandbox here yet, and a headless vibe only starts one that exists: starting with a terminal, to make it (the mode setting in settings.yaml applies from the next start)`
		}
		return true, ""
	}
	return false, ""
}

// startMode says whether this vibe starts headless: what was typed, what a
// --restart ended, or the user's favourite, in that order (see
// modeWanted.resolve), saying so when there's something to tell. What isn't
// needed to decide isn't looked at: the settings, and whether there's a
// sandbox, only when nothing typed or restarted has decided.
func startMode(lay layout.Layout, args cliargs.Args, name string, ended runMode) (bool, error) {
	want := modeWanted{Headless: args.Headless, Terminal: args.Terminal, Restart: args.Restart, Ended: ended,
		Rebuilds: args.Rebuilds(), Deleting: args.Delete}
	decided := args.Headless || args.Terminal || (args.Restart && ended != modeNone) || args.Delete
	if !decided {
		favourite, err := favouriteMode(lay)
		if err != nil {
			return false, err
		}
		want.Favourite = favourite
		if favourite == "headless" && !args.Rebuilds() {
			if want.SandboxExists, err = sbxrun.Exists(name); err != nil {
				return false, err
			}
		}
	}
	headless, says := want.resolve()
	if says != "" {
		note("%s", says)
	}
	return headless, nil
}
