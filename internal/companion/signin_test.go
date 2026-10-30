package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"vibe/internal/signin"
)

func signInServer(check func(ctx context.Context) signin.Status) *Server {
	s := newServer(&fakeSbx{})
	s.cfg.SignIn = check
	return s
}

func TestTheSignInIsWhatTheHostFindsOnAskingAfresh(t *testing.T) {
	asked := 0
	s := signInServer(func(context.Context) signin.Status {
		asked++
		return signin.Status{
			Agent:     "claude",
			Docker:    signin.Part{State: signin.SignedIn, Manual: []string{"sbx login"}},
			AgentPart: signin.Part{State: signin.SignedOut, Manual: []string{"claude auth login"}},
		}
	})
	for i := 1; i <= 2; i++ {
		rec := do(t, s, req{path: "/api/signin", headers: authed(nil)})
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var got struct {
			Agent  string `json:"agent"`
			Docker struct {
				State  string   `json:"state"`
				Manual []string `json:"manual"`
			} `json:"docker"`
			AgentSignIn struct {
				State string `json:"state"`
			} `json:"agentSignIn"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Agent != "claude" || got.Docker.State != "signed-in" || got.AgentSignIn.State != "signed-out" || len(got.Docker.Manual) != 1 {
			t.Errorf("call %d: %s", i, rec.Body)
		}
		if asked != i {
			t.Errorf("after %d calls the host was asked %d times: the Check now button wants an answer from now", i, asked)
		}
	}
}

func TestTheSignInIsGivenAWhileToAnswerAndNoLonger(t *testing.T) {
	var deadlineSet bool
	s := signInServer(func(ctx context.Context) signin.Status {
		_, deadlineSet = ctx.Deadline()
		return signin.Status{}
	})
	do(t, s, req{path: "/api/signin", headers: authed(nil)})
	if !deadlineSet {
		t.Error("asking the host has no time limit: a stalled sbx would hold the request for good")
	}
}

func TestTheSignInIsRefusedWithoutTheToken(t *testing.T) {
	s := signInServer(func(context.Context) signin.Status { return signin.Status{} })
	if rec := do(t, s, req{path: "/api/signin"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d", rec.Code)
	}
}

func TestAServerWithNoSignInSaysSoAndTheStateDoesNotOfferIt(t *testing.T) {
	s := newServer(&fakeSbx{})
	if rec := do(t, s, req{path: "/api/signin", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("status %d", rec.Code)
	}
	if got := pageState(t, s)["signin"]; got != false {
		t.Errorf("state signin = %v", got)
	}
	if got := pageState(t, signInServer(func(context.Context) signin.Status { return signin.Status{} }))["signin"]; got != true {
		t.Errorf("state signin = %v", got)
	}
}
