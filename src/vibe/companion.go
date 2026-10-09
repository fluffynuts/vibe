package main

import (
	"fmt"
	"strings"

	"vibe/internal/browser"
	"vibe/internal/cliargs"
	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/policy"
	"vibe/internal/portalloc"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// What vibe does about the companion page: "open" serves it and opens it in
// the browser, "serve" serves it and prints the URL, "off" doesn't serve it.
const (
	companionOpen  = "open"
	companionServe = "serve"
	companionOff   = "off"
)

// companionBasePort is where the search for a companion port starts: well
// clear of the service ports (5390, 5391 and the ones a profile counts up
// through from them). Once a sandbox has one, it keeps it, so the page's
// URL (and a bookmark of it) is the same at every start.
const (
	companionBasePort = 10000
	companionPortSpan = 60
)

// companionMode settles what to do from -N and the companion setting. An
// unset setting means "open"; one that isn't a mode is an error, since
// guessing which way a typo meant would either open pages the user turned
// off or hide ones they asked for.
func companionMode(noCompanion bool, setting string) (string, error) {
	if noCompanion {
		return companionOff, nil
	}
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "", companionOpen:
		return companionOpen, nil
	case companionServe:
		return companionServe, nil
	case companionOff:
		return companionOff, nil
	}
	return "", fmt.Errorf("companion: %q is not one of open, serve, off", setting)
}

// chooseCompanionPort picks the host port for a sandbox's companion page.
// The one the sandbox had last time is kept, if it's free — or if what
// holds it is this sandbox's own page, served by another vibe attached to
// it, in which case served is true and there is nothing to start. Otherwise
// it is the first port from companionBasePort that no other sandbox
// publishes or serves a page on, and nothing is listening on.
func chooseCompanionPort(instances []state.Instance, name string, inUse func(int) bool, isOurs func(int) bool) (port int, served bool, err error) {
	taken := map[int]bool{}
	remembered := 0
	for _, inst := range instances {
		if inst.Name == name {
			remembered = inst.CompanionPort
		} else if inst.CompanionPort != 0 {
			taken[inst.CompanionPort] = true
		}
		for _, rec := range inst.Publish {
			taken[rec.HostPort] = true
		}
	}
	if remembered != 0 && !taken[remembered] {
		if !inUse(remembered) {
			return remembered, false, nil
		}
		if isOurs(remembered) {
			return remembered, true, nil
		}
	}
	for p := companionBasePort; p < companionBasePort+companionPortSpan; p++ {
		if !taken[p] && !inUse(p) {
			return p, false, nil
		}
	}
	return 0, false, fmt.Errorf("no free port found in %d-%d", companionBasePort, companionBasePort+companionPortSpan-1)
}

// clipboardPageURL is where the sandbox's clipboard page can be reached on
// this machine, or "" when its profile doesn't have one. It is built the
// way the URL written into the sandbox is: from the host port sbx says the
// mapping has right now, the recorded one standing in while sbx can't say.
func clipboardPageURL(vibeHome, name string) string {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found {
		return ""
	}
	live, _ := sbxrun.PublishedPorts(name)
	for _, u := range publishedURLs(inst.Publish, live) {
		if u.name == "clipboard" {
			return u.url()
		}
	}
	return ""
}

// startCompanion serves the companion page for the sandbox, as the user's
// settings say, and returns what stops it. It never stops the session
// starting: whatever goes wrong is reported, and the session goes on
// without the page.
func startCompanion(lay layout.Layout, args cliargs.Args, name string) (stop func()) {
	stop = func() {}
	vibeHome := lay.Home
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop
	}
	setting := ""
	if found && lay.ProfileExists(inst.Profile) {
		if merged, err := loadSettings(lay, inst.Profile); err == nil {
			setting = merged.Companion
		}
	}
	mode, err := companionMode(args.NoCompanion, setting)
	if err != nil {
		note("%v — not serving the companion page", err)
		return stop
	}
	if mode == companionOff {
		return stop
	}

	token, err := state.CompanionToken(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop
	}
	instances, err := state.List(vibeHome)
	if err != nil {
		note("companion: %v", err)
		return stop
	}
	port, served, err := chooseCompanionPort(instances, name, portalloc.InUse,
		func(p int) bool { return companion.Running(p, name) })
	if err != nil {
		note("companion: %v", err)
		return stop
	}
	if found && inst.CompanionPort != port {
		inst.CompanionPort = port
		if err := state.Save(vibeHome, inst); err != nil {
			note("companion: could not remember its port: %v", err)
		}
	}
	url := fmt.Sprintf("http://localhost:%d/#%s", port, token)
	if served {
		note("companion page: %s (already being served by another vibe for this sandbox)", url)
		return stop
	}

	srv, err := companion.Start(companion.Config{
		Sandbox:      name,
		Token:        token,
		Port:         port,
		Policy:       &policy.Client{Sandbox: name},
		ClipboardURL: func() string { return clipboardPageURL(vibeHome, name) },
	})
	if err != nil {
		note("companion: could not serve the page on port %d: %v", port, err)
		return stop
	}
	note("companion page: %s", srv.URL())
	if mode == companionOpen && !browser.Open(srv.URL()) {
		note("companion: no browser to open it in from here — open the URL above yourself")
	}
	return func() { srv.Close() }
}
