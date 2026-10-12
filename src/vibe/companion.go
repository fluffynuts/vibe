package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vibe/internal/browser"
	"vibe/internal/cliargs"
	"vibe/internal/companion"
	"vibe/internal/kitspec"
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

// startClipboardScript starts the clipboard page's server in the sandbox,
// and does nothing when it is already running (see the clipboard-bridge kit).
const startClipboardScript = "/home/agent/.local/bin/start-clipboard"

// startClipboard is the companion page's "start it" for a clipboard page
// that hasn't come up: it runs what on-start would have, and waits for it
// (start-clipboard itself gives the server ten seconds to listen).
func startClipboard(name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"exec", name, "--", startClipboardScript}
	if out, err := sbxrun.ExecCaptureContext(ctx, name, startClipboardScript); err != nil {
		if out = strings.TrimSpace(out); out != "" {
			return args, fmt.Errorf("start-clipboard failed: %s", out)
		}
		return args, fmt.Errorf("start-clipboard failed: %v — see /tmp/clipboard.log in the sandbox", err)
	}
	return args, nil
}

// diffityReviewFile is where, in the sandbox, the diffity kit's diffity-url
// keeps the last link it gave out.
const diffityReviewFile = kitspec.AgentHome + "/.local/state/vibe/diffity-review-url"

// latestReviewFor reads the sandbox's last diffity link for the companion
// page's conversation, or is nil when the sandbox doesn't publish diffity.
func latestReviewFor(name string, inst state.Instance) func() string {
	for _, p := range inst.Publish {
		if p.Name == "diffity" {
			return func() string {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				// No file yet is no review yet: cat's complaint isn't an error.
				out, err := sbxrun.ExecCaptureContext(ctx, name, "cat", diffityReviewFile)
				if err != nil {
					return ""
				}
				return lastLine(out)
			}
		}
	}
	return nil
}

// lastLine is the last line of out that isn't blank. What sbx exec gives
// back has sbx's own stderr mixed in, ahead of the command's output, and
// diffity-url's file is a single line.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// conversationFor is the companion page's way of talking to the sandbox's
// agent, or nil when it has none: only claude can be driven this way so far.
func conversationFor(vibeHome, name string, inst state.Instance) companion.Converser {
	if agent := inst.Agent; agent != "" && agent != "claude" {
		return nil
	}
	return keepingTranscript(converse(name, inst.Target), sandboxTranscripts(name), state.TranscriptPath(vibeHome, name))
}

// startCompanion serves the companion page for the sandbox, as the user's
// settings say, and returns what stops it. It never stops the session
// starting: whatever goes wrong is reported, and the session goes on
// without the page.
func startCompanion(lay layout.Layout, args cliargs.Args, name string) (stop func(), serving bool) {
	stop = func() {}
	vibeHome := lay.Home
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop, false
	}
	setting := ""
	agent := inst.Agent
	if found && lay.ProfileExists(inst.Profile) {
		if merged, err := loadSettings(lay, inst.Profile); err == nil {
			setting = merged.Companion
			if agent == "" {
				agent = agentOf(merged)
			}
		}
	}
	mode, err := companionMode(args.NoCompanion, setting)
	if err != nil {
		note("%v — not serving the companion page", err)
		return stop, false
	}
	if mode == companionOff {
		return stop, false
	}

	token, err := state.CompanionToken(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop, false
	}
	instances, err := state.List(vibeHome)
	if err != nil {
		note("companion: %v", err)
		return stop, false
	}
	port, served, err := chooseCompanionPort(instances, name, portalloc.InUse,
		func(p int) bool { return companion.Running(p, name) })
	if err != nil {
		note("companion: %v", err)
		return stop, false
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
		return stop, false
	}

	srv, err := companion.Start(companion.Config{
		Sandbox:        name,
		Agent:          agent,
		Token:          token,
		Port:           port,
		Policy:         &policy.Client{Sandbox: name},
		ClipboardURL:   func() string { return clipboardPageURL(vibeHome, name) },
		StartClipboard: func() ([]string, error) { return startClipboard(name) },
		Workspace:      inst.Target,
		Converse:       conversationFor(vibeHome, name, inst),
		History:        state.ConversationPath(vibeHome, name),
		LatestReview:   latestReviewFor(name, inst),
		Admin:          adminFor(lay, name, inst.Target),
	})
	if err != nil {
		note("companion: could not serve the page on port %d: %v", port, err)
		return stop, false
	}
	note("companion page: %s", srv.URL())
	if mode == companionOpen && !browser.Open(srv.URL()) {
		note("companion: no browser to open it in from here — open the URL above yourself")
	}
	return func() { srv.Close() }, true
}
