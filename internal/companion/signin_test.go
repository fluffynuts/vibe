package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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

// The key option: a key given on the page goes to the host, and is never in anything said back.
func keyServer(set func(ctx context.Context, key string) error) *Server {
	s := signInServer(func(context.Context) signin.Status { return signin.Status{} })
	s.cfg.SignInAPIKey = set
	return s
}

func TestAKeyGivenOnThePageGoesToTheHost(t *testing.T) {
	var got string
	s := keyServer(func(_ context.Context, key string) error { got = key; return nil })
	if code, body := postJSON(t, s, "/api/signin/apikey", `{"key":"sk-ant-abc"}`); code != http.StatusOK || strings.Contains(body, "sk-ant-abc") {
		t.Errorf("%d %s", code, body)
	}
	if got != "sk-ant-abc" {
		t.Errorf("the host was given %q", got)
	}
}

func TestAKeyThatIsEmptyOrNotOneLineOrTooLongIsRefusedAndNotGivenToTheHost(t *testing.T) {
	called := 0
	s := keyServer(func(context.Context, string) error { called++; return nil })
	for _, bad := range []string{`{"key":""}`, `{"key":"  "}`, `{"key":"a\nb"}`, `{"key":"` + strings.Repeat("x", 5000) + `"}`, `{}`, `nonsense`} {
		if code, _ := postJSON(t, s, "/api/signin/apikey", bad); code != http.StatusBadRequest {
			t.Errorf("%.40s: status %d", bad, code)
		}
	}
	if called != 0 {
		t.Errorf("the host was asked %d times", called)
	}
}

func TestAKeyTheHostRefusesIsSaidWithoutTheKeyInIt(t *testing.T) {
	s := keyServer(func(_ context.Context, key string) error { return errors.New("sbx said no to " + key) })
	code, body := postJSON(t, s, "/api/signin/apikey", `{"key":"sk-ant-secret"}`)
	if code != http.StatusInternalServerError || strings.Contains(body, "sk-ant-secret") || !strings.Contains(body, "sbx said no") {
		t.Errorf("%d %s", code, body)
	}
}

func TestTheKeyCallNeedsTheTokenAndAHostThatOffersIt(t *testing.T) {
	s := keyServer(func(context.Context, string) error { return nil })
	if rec := do(t, s, req{method: "POST", path: "/api/signin/apikey", body: `{"key":"k"}`, headers: map[string]string{"Content-Type": "application/json"}}); rec.Code != http.StatusUnauthorized {
		t.Errorf("without the token: %d", rec.Code)
	}
	bare := signInServer(func(context.Context) signin.Status { return signin.Status{} })
	if code, _ := postJSON(t, bare, "/api/signin/apikey", `{"key":"k"}`); code != http.StatusNotFound {
		t.Errorf("a host with no key option: %d", code)
	}
	if got := pageState(t, s)["signinKey"]; got != true {
		t.Errorf("state signinKey = %v", got)
	}
	if got := pageState(t, bare)["signinKey"]; got != false {
		t.Errorf("a host without it: signinKey = %v", got)
	}
}
