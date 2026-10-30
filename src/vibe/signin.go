package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vibe/internal/companion"
	"vibe/internal/sbxrun"
	"vibe/internal/signin"
)

// signInProbes are the three questions the Sign-in card asks of the machine, apart so that they can be answered in tests.
type signInProbes struct {
	// sbx is what `sbx ls` printed, and its error.
	sbx func(ctx context.Context) (string, error)
	// running is whether the sandbox is running, and whether that could be said.
	running func(ctx context.Context) (running, ok bool)
	// claude is the sandbox's credential mode and what `claude auth status` printed, as the agent.
	claude func(ctx context.Context) (mode, status string, err error)
}

// signInFor is what the companion page asks for to fill its Sign-in card.
func signInFor(name, agent string) func(ctx context.Context) signin.Status {
	probes := signInProbes{
		sbx:     func(ctx context.Context) (string, error) { return sbxrun.Probe(ctx, "ls") },
		running: func(ctx context.Context) (bool, bool) { return sbxrun.Running(ctx, name) },
		claude: func(ctx context.Context) (string, string, error) {
			// The container's own environment, which a login shell as the agent (asAgent) starts without.
			mode, _ := sbxrun.ExecStdout(ctx, name, "printenv", "SBX_CRED_ANTHROPIC_MODE")
			status, err := sbxrun.ExecStdout(ctx, name, asAgent("claude auth status 2>/dev/null; exit 0")...)
			return strings.TrimSpace(mode), status, err
		},
	}
	return func(ctx context.Context) signin.Status { return checkSignIn(ctx, name, agent, probes) }
}

// checkSignIn reads sbx's sign-in, then the agent's: nothing can be asked of a sandbox until sbx is signed in, nor of
// one that isn't running, so then the agent's is said to be unknown, and why.
func checkSignIn(ctx context.Context, name, agent string, p signInProbes) signin.Status {
	out, err := p.sbx(ctx)
	docker := signin.Docker(out, err, ctx.Err() != nil)
	status := signin.Status{Agent: agent, Docker: docker}
	if agent != "claude" {
		status.AgentPart = signin.Other(agent)
		return status
	}
	status.AgentPart = signin.Claude("", "", false)
	switch running, ok := p.running(ctx); {
	case docker.State != signin.SignedIn:
		status.AgentPart.Detail = "it can be checked once sbx is signed in"
	case !ok || !running:
		status.AgentPart.Detail = "the sandbox isn't running, so it can't be asked"
	default:
		mode, said, err := p.claude(ctx)
		status.AgentPart = signin.Claude(mode, said, err == nil)
	}
	return status
}

// signInStarters are how a sign-in is started, apart so that tests can answer for them.
type signInStarters struct {
	running    func(ctx context.Context) (running, ok bool)
	login      func() (companion.LoginProcess, error)
	agentLogin func() (companion.LoginProcess, error)
}

// signInStartFor is what the Sign-in card's buttons start: sbx's own sign-in, or the agent's in the sandbox.
func signInStartFor(name, agent string) func(kind string) (companion.LoginProcess, error) {
	starters := signInStarters{
		running: func(ctx context.Context) (bool, bool) { return sbxrun.Running(ctx, name) },
		login: func() (companion.LoginProcess, error) {
			p, err := sbxrun.StartLogin()
			if err != nil {
				return nil, err
			}
			return p, nil
		},
		agentLogin: func() (companion.LoginProcess, error) {
			p, err := sbxrun.StartExec(name, asAgent("claude auth login")...)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
	}
	return func(kind string) (companion.LoginProcess, error) {
		ctx, cancel := context.WithTimeout(context.Background(), loginCheckWithin)
		defer cancel()
		return startSignIn(ctx, kind, agent, starters)
	}
}

// startSignIn starts the sign-in of kind: the agent's needs a sandbox that is running, and an agent whose sign-in vibe
// knows how to drive.
func startSignIn(ctx context.Context, kind, agent string, s signInStarters) (companion.LoginProcess, error) {
	switch kind {
	case "docker":
		return s.login()
	case "agent":
		if agent != "claude" {
			return nil, fmt.Errorf("vibe can't sign %s in yet: sign it in by hand", agent)
		}
		if running, ok := s.running(ctx); !ok || !running {
			return nil, errors.New("the sandbox isn't running: start it first, then sign in")
		}
		return s.agentLogin()
	}
	return nil, fmt.Errorf("no sign-in of kind %q", kind)
}
