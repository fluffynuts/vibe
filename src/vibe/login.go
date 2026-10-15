package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"vibe/internal/sbxrun"
)

// loginCheckWithin bounds asking the sandbox whether its agent is logged in.
const loginCheckWithin = 30 * time.Second

// warnIfAgentLoggedOut says, for a sandbox with no terminal attached, when
// its agent isn't logged in: nobody sees its login screen, and every prompt
// from the page would fail. It's only said when that's certain; a check
// that can't be made says nothing.
func warnIfAgentLoggedOut(name, agent string) {
	if agent != "claude" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginCheckWithin)
	defer cancel()
	// The container's own environment, which a login shell as the agent
	// (asAgent) starts without.
	mode, _ := sbxrun.ExecStdout(ctx, name, "printenv", "SBX_CRED_ANTHROPIC_MODE")
	status, err := sbxrun.ExecStdout(ctx, name, asAgent("claude auth status 2>/dev/null; exit 0")...)
	if agentLoggedOut(strings.TrimSpace(mode), status, err == nil) {
		note("WARNING: sandbox '%s''s agent isn't logged in, so prompts from the page will fail.", name)
		note("  in a terminal, run vibe in its folder and log in there with /login; the page can't show the agent's login")
	}
}

// agentLoggedOut says, from sbx's credential mode and what 'claude auth
// status' printed (ok when it could be asked), that the agent is certainly
// logged out. With a mode other than none, sbx may be handing the agent
// credentials itself, which the status may not know of.
func agentLoggedOut(mode, status string, ok bool) bool {
	if !ok || (mode != "" && mode != "none") {
		return false
	}
	var s struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(status)), &s) != nil || s.LoggedIn == nil {
		return false
	}
	return !*s.LoggedIn
}
