package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vibe/internal/companion"
	"vibe/internal/signin"
)

type fakeSignIn struct {
	sbxOut     string
	sbxErr     error
	running    bool
	runningOK  bool
	mode       string
	status     string
	statusErr  error
	agentAsked bool
}

func (f *fakeSignIn) check(agent string) signin.Status {
	return checkSignIn(context.Background(), "box", agent, signInProbes{
		sbx:     func(context.Context) (string, error) { return f.sbxOut, f.sbxErr },
		running: func(context.Context) (bool, bool) { return f.running, f.runningOK },
		claude: func(context.Context) (string, string, error) {
			f.agentAsked = true
			return f.mode, f.status, f.statusErr
		},
	})
}

func TestBothAreReadWhenSbxIsSignedInAndTheSandboxRuns(t *testing.T) {
	f := &fakeSignIn{sbxOut: "[]", running: true, runningOK: true, mode: "none",
		status: `{"loggedIn": true, "email": "me@x.org"}`}
	got := f.check("claude")
	if got.Agent != "claude" || got.Docker.State != signin.SignedIn || got.AgentPart.State != signin.SignedIn {
		t.Errorf("got %+v", got)
	}
	if !strings.Contains(got.AgentPart.Detail, "me@x.org") {
		t.Errorf("detail = %q", got.AgentPart.Detail)
	}
}

// Nothing can be asked of a sandbox through sbx that isn't signed in, so the agent isn't asked, and isn't called signed
// out for it.
func TestTheAgentIsNotAskedWhileSbxIsNotSignedIn(t *testing.T) {
	f := &fakeSignIn{sbxOut: "error: not signed in to Docker", sbxErr: errors.New("exit 1"), running: true, runningOK: true}
	got := f.check("claude")
	if got.Docker.State != signin.SignedOut {
		t.Errorf("docker = %q", got.Docker.State)
	}
	if f.agentAsked || got.AgentPart.State != signin.Unknown || !strings.Contains(got.AgentPart.Detail, "sbx") {
		t.Errorf("asked %v, agent %+v", f.agentAsked, got.AgentPart)
	}
	if len(got.AgentPart.Manual) == 0 {
		t.Error("the agent's own sign-in has no manual steps")
	}
}

func TestTheAgentIsNotAskedWhileTheSandboxIsStopped(t *testing.T) {
	f := &fakeSignIn{sbxOut: "[]", running: false, runningOK: true}
	got := f.check("claude")
	if f.agentAsked || got.AgentPart.State != signin.Unknown || !strings.Contains(got.AgentPart.Detail, "running") {
		t.Errorf("asked %v, agent %+v", f.agentAsked, got.AgentPart)
	}
}

func TestAnAgentThatCouldNotBeAskedIsUnknown(t *testing.T) {
	f := &fakeSignIn{sbxOut: "[]", running: true, runningOK: true, statusErr: errors.New("boom")}
	if got := f.check("claude"); got.AgentPart.State != signin.Unknown {
		t.Errorf("agent %+v", got.AgentPart)
	}
}

func TestAnotherAgentIsSaidNotToBeCheckableAndIsNotAsked(t *testing.T) {
	f := &fakeSignIn{sbxOut: "[]", running: true, runningOK: true}
	got := f.check("cursor")
	if f.agentAsked || got.AgentPart.State != signin.Unsupported || got.Docker.State != signin.SignedIn {
		t.Errorf("asked %v, got %+v", f.agentAsked, got)
	}
}

type fakeStarters struct {
	running   bool
	runningOK bool
	started   []string
	err       error
}

func (f *fakeStarters) starters() signInStarters {
	return signInStarters{
		running: func(context.Context) (bool, bool) { return f.running, f.runningOK },
		login:   func() (companion.LoginProcess, error) { f.started = append(f.started, "sbx login"); return nil, f.err },
		agentLogin: func() (companion.LoginProcess, error) {
			f.started = append(f.started, "claude auth login in the sandbox")
			return nil, f.err
		},
	}
}

func TestSigningInToDockerRunsSbxLogin(t *testing.T) {
	f := &fakeStarters{}
	if _, err := startSignIn(context.Background(), "docker", "claude", f.starters()); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 1 || f.started[0] != "sbx login" {
		t.Errorf("started %v", f.started)
	}
}

func TestSigningInClaudeRunsItsLoginInTheSandboxWhenItRuns(t *testing.T) {
	f := &fakeStarters{running: true, runningOK: true}
	if _, err := startSignIn(context.Background(), "agent", "claude", f.starters()); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 1 || !strings.Contains(f.started[0], "claude auth login") {
		t.Errorf("started %v", f.started)
	}
}

func TestSigningTheAgentInNeedsTheSandboxRunningAndSaysSo(t *testing.T) {
	f := &fakeStarters{running: false, runningOK: true}
	_, err := startSignIn(context.Background(), "agent", "claude", f.starters())
	if err == nil || !strings.Contains(err.Error(), "isn't running") || len(f.started) != 0 {
		t.Errorf("err = %v, started %v", err, f.started)
	}
}

func TestAnotherAgentCannotBeSignedInYetAndSaysSo(t *testing.T) {
	f := &fakeStarters{running: true, runningOK: true}
	_, err := startSignIn(context.Background(), "agent", "cursor", f.starters())
	if err == nil || !strings.Contains(err.Error(), "cursor") || len(f.started) != 0 {
		t.Errorf("err = %v, started %v", err, f.started)
	}
}
