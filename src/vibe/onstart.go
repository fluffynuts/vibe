package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
	"vibe/internal/timezone"
)

func nudgeOnStart(lay layout.Layout, name string, reached func()) {
	vibeHome := lay.Home
	logf := sessionStartLogger(vibeHome, name)
	if !sbxrun.WaitReachable(name, 180) {
		logf("gave up waiting for reachability")
		return
	}
	reached()
	writePublishedURLs(vibeHome, name, logf)
	zone := recordedTimezone(vibeHome, name)
	if err := timezone.Apply(name, zone); err != nil {
		logf("setting the timezone to %s failed: %v", zone, err)
	} else {
		logf("timezone is %s", zone)
	}
	if err := syncGitInstructions(vibeHome, name); err != nil {
		logf("writing the agent's rules for pushing failed: %v", err)
	}
	if supported, err := applyInstructions(lay, name); err != nil {
		logf("giving the agent the user's instructions failed: %v", err)
	} else if supported {
		logf("the agent has the user's instructions")
	}
	// sbx exec runs as the agent user, but on-start is written to run as
	// root, as setup.startup runs it at first boot: its logs in /tmp are
	// root's from then on, and as the agent it dies on the first of them
	// without starting anything. It runs in the foreground — sbx exec has
	// no --detach — which is fine here, off the session's own goroutine:
	// what it starts to keep running (the clipboard server) detaches itself.
	if out, err := sbxrun.ExecCapture(name, "sudo", "-n", "/home/agent/.local/bin/on-start"); err != nil {
		logf("on-start nudge failed: %v: %s", err, strings.TrimSpace(out))
		return
	}
	logf("on-start nudge ran")
}

// sessionStartLog is where what's done to a sandbox at the start of each
// session is logged: in vibe's home, the user's alone, as what's logged
// includes what the sandbox said.
func sessionStartLog(vibeHome string) string {
	return filepath.Join(vibeHome, "logs", "session-start.log")
}

// sessionStartLogger appends a line about sandbox name to sessionStartLog,
// stamped with the time.
func sessionStartLogger(vibeHome, name string) func(format string, a ...interface{}) {
	path := sessionStartLog(vibeHome)
	return func(format string, a ...interface{}) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "[%s] %s: "+format+"\n",
			append([]interface{}{time.Now().Format(time.RFC3339), name}, a...)...)
	}
}

// publishedURLDir is where, inside the sandbox, each published service's
// URL is written at every session start — one file per publish entry,
// named after it (".../published/<name>"), holding the URL the host
// reaches it on (and a second, "unverified: ..." line when sbx couldn't
// confirm it). It is what the sandbox's own helpers read
// first: unlike the urlEnv variable, fixed when the sandbox was created, it
// is checked against sbx's live port mappings every time.
const publishedURLDir = kitspec.AgentHome + "/.local/state/vibe/published"

// writePublishedURLs writes publishedURLDir for a running sandbox. Each URL
// is built from the host port sbx reports the mapping actually has; the
// port vibe recorded at creation is used only when sbx can't say, and the
// log records which it was.
func writePublishedURLs(vibeHome, name string, logf func(string, ...interface{})) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found || len(inst.Publish) == 0 {
		return
	}
	live, err := sbxrun.PublishedPorts(name)
	if err != nil {
		logf("could not read live port mappings, using recorded ones: %v", err)
	}
	for _, u := range publishedURLs(inst.Publish, live) {
		switch {
		case u.unverified:
			logf("%s: sbx reports no mapping for %d; using recorded host port %d", u.name, u.containerPort, u.hostPort)
		case u.hostPort != u.recordedPort:
			logf("%s: sbx maps %d to host port %d, not the recorded %d", u.name, u.containerPort, u.hostPort, u.recordedPort)
		}
		if err := sbxrun.WriteFile(name, publishedURLDir+"/"+u.name, u.fileContent()); err != nil {
			logf("%s: %v", u.name, err)
		}
	}
}

type publishedURL struct {
	name                    string
	containerPort, hostPort int
	recordedPort            int
	unverified              bool
}

func (u publishedURL) url() string {
	return fmt.Sprintf("http://localhost:%d", u.hostPort)
}

// fileContent is the URL on the first line, followed — when sbx couldn't
// confirm the port — by a line saying so, which the sandbox's helpers pass
// on as a warning rather than presenting the URL as certain.
func (u publishedURL) fileContent() string {
	if u.unverified {
		return u.url() + "\nunverified: sbx did not report this mapping; this is the port recorded at creation\n"
	}
	return u.url() + "\n"
}

// publishedURLs picks, for each publish entry, the URL of its first port —
// the one urlEnv names. The recorded host port wins while sbx still lists
// it: sbx adds a mapping of its own, on a fresh ephemeral port, at every
// start, and that one doesn't reach the service. Only when the recorded
// mapping is gone (changed since with `sbx ports --publish`) is the first
// live one used instead.
func publishedURLs(records []state.PublishRecord, live map[int][]int) []publishedURL {
	var urls []publishedURL
	seen := map[string]bool{}
	for _, rec := range records {
		if rec.Name == "" || seen[rec.Name] {
			continue
		}
		seen[rec.Name] = true
		u := publishedURL{name: rec.Name, containerPort: rec.ContainerPort, hostPort: rec.HostPort, recordedPort: rec.HostPort}
		switch hostPorts := live[rec.ContainerPort]; {
		case slices.Contains(hostPorts, rec.HostPort):
		case len(hostPorts) > 0:
			u.hostPort = hostPorts[0]
		default:
			u.unverified = true
		}
		urls = append(urls, u)
	}
	return urls
}
