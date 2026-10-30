package main

import (
	"fmt"
	"path/filepath"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/settings"
	"vibe/internal/userprefs"
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

// favouriteMode is the mode vibe starts in when nothing decides it ("headless"
// or "terminal"), or "" if none is chosen. The profile's own choice
// (user-preferences.yaml, which isn't part of what makes a profile look edited
// by hand) wins, then the profile's settings.yaml over ~/.vibe/settings.yaml
// (or the bundle's, where there's no ~/.vibe one), so one profile can be
// headless while the others aren't. A wrong value is refused, naming the file
// it is in.
func favouriteMode(lay layout.Layout, profile string) (string, error) {
	basePath := lay.File("settings.yaml")
	base, err := settings.Load(basePath)
	if err != nil {
		return "", err
	}
	profilePath := filepath.Join(lay.ProfileDir(profile), "settings.yaml")
	own, err := settings.Load(profilePath)
	if err != nil {
		return "", err
	}
	prefs, err := userprefs.Load(lay.ProfileDir(profile))
	if err != nil {
		return "", err
	}
	mode, path := base.Mode, basePath
	if own.Mode != "" {
		mode, path = own.Mode, profilePath
	}
	if prefs.Mode != "" {
		mode, path = prefs.Mode, userprefs.Path(lay.ProfileDir(profile))
	}
	switch mode {
	case "", "headless", "terminal":
		return mode, nil
	}
	return "", fmt.Errorf("mode in %s is %q: it must be %q (start without a terminal, serving the companion page) or %q", pathresolve.Tilde(path), mode, "headless", "terminal")
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

// createsFirst says a headless start by the favourite finds no sandbox, and so makes it first. What was typed
// (--headless) never does: it is refused with what to do, since it is the one a script runs with nobody to ask.
func (w modeWanted) createsFirst() bool {
	return w.Favourite == "headless" && !w.Headless && !w.Terminal && !(w.Restart && w.Ended != modeNone) &&
		!w.SandboxExists && !w.Rebuilds && !w.Deleting
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
			return true, "there's no sandbox here yet: making it here, then serving the companion page with no terminal"
		}
		return true, ""
	}
	return false, ""
}

// startMode says whether this vibe starts headless: what was typed, what a
// --restart ended, or the user's favourite, in that order (see
// modeWanted.resolve), saying so when there's something to tell. What isn't
// needed to decide isn't looked at: the settings, and whether there's a
// sandbox, only when nothing typed or restarted has decided. createFirst
// says the sandbox isn't there yet, and is to be made on this terminal
// (which is where its questions are asked) before the headless start.
func startMode(lay layout.Layout, args cliargs.Args, name, target string, ended runMode) (headless, createFirst bool, err error) {
	want := modeWanted{Headless: args.Headless, Terminal: args.Terminal, Restart: args.Restart, Ended: ended,
		Rebuilds: args.Rebuilds(), Deleting: args.Delete}
	decided := args.Headless || args.Terminal || (args.Restart && ended != modeNone) || args.Delete
	if !decided {
		profile, err := resolveReInitProfile(lay.Home, args, name, target)
		if err != nil {
			return false, false, err
		}
		favourite, err := favouriteMode(lay, profile)
		if err != nil {
			return false, false, err
		}
		want.Favourite = favourite
		if favourite == "headless" && !args.Rebuilds() {
			if want.SandboxExists, err = sbxrun.Exists(name); err != nil {
				return false, false, err
			}
		}
	}
	headless, says := want.resolve()
	if says != "" {
		note("%s", says)
	}
	return headless, headless && want.createsFirst(), nil
}
