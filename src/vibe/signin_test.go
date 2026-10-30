package main

import (
	"context"
	"errors"
	"strings"
	"testing"

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
