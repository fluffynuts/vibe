package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/packagereq"
)

type fakeDesk struct {
	mu      sync.Mutex
	pending []packagereq.Pending
	allowed [][]string
	denied  [][]string
	all     int
	polls   int
}

func (f *fakeDesk) Poll(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
}
func (f *fakeDesk) Pending() []packagereq.Pending {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]packagereq.Pending(nil), f.pending...)
}
func (f *fakeDesk) Allow(keys []string) {
	f.mu.Lock()
	f.allowed = append(f.allowed, keys)
	f.mu.Unlock()
}
func (f *fakeDesk) Deny(keys []string) { f.mu.Lock(); f.denied = append(f.denied, keys); f.mu.Unlock() }
func (f *fakeDesk) DenyAll()           { f.mu.Lock(); f.all++; f.mu.Unlock() }

func demoPending() []packagereq.Pending {
	return []packagereq.Pending{{
		Key:     "nuget|Corp.Lib|1.2.3|net8.0",
		Request: packagereq.Request{Ecosystem: "nuget", ID: "Corp.Lib", Version: "1.2.3", Framework: "net8.0"},
		Note:    "new to this project",
	}}
}

func packagesServer(desk *fakeDesk) *Server {
	return New(Config{Sandbox: "box", Token: token, Port: testPort, Packages: desk})
}

func TestThePageListsTheRequestsHeldForTheUser(t *testing.T) {
	s := packagesServer(&fakeDesk{pending: demoPending()})
	rec := do(t, s, req{path: "/api/packages", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got struct {
		Requests []struct {
			Key       string `json:"key"`
			Ecosystem string `json:"ecosystem"`
			ID        string `json:"id"`
			Version   string `json:"version"`
			Framework string `json:"framework"`
			Note      string `json:"note"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Requests) != 1 || got.Requests[0].ID != "Corp.Lib" || got.Requests[0].Note != "new to this project" ||
		got.Requests[0].Key != "nuget|Corp.Lib|1.2.3|net8.0" {
		t.Errorf("requests = %+v", got.Requests)
	}
}

func TestApprovingAndDenyingReachTheDesk(t *testing.T) {
	desk := &fakeDesk{pending: demoPending()}
	s := packagesServer(desk)
	if rec := do(t, s, post("/api/packages/allow", `{"keys":["a","b"]}`)); rec.Code != http.StatusOK {
		t.Fatalf("allow: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/packages/deny", `{"keys":["c"]}`)); rec.Code != http.StatusOK {
		t.Fatalf("deny: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/packages/deny-all", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("deny-all: %d %s", rec.Code, rec.Body)
	}
	if len(desk.allowed) != 1 || strings.Join(desk.allowed[0], ",") != "a,b" || len(desk.denied) != 1 || desk.denied[0][0] != "c" || desk.all != 1 {
		t.Errorf("allowed %v, denied %v, all %d", desk.allowed, desk.denied, desk.all)
	}
}

func TestPackageRequestsAreOnlyTakenFromThePageItself(t *testing.T) {
	desk := &fakeDesk{pending: demoPending()}
	s := packagesServer(desk)
	cases := map[string]req{
		"no token": {method: "POST", path: "/api/packages/allow", body: `{"keys":["a"]}`,
			headers: map[string]string{"Content-Type": "application/json"}},
		"another origin": {method: "POST", path: "/api/packages/allow", body: `{"keys":["a"]}`,
			headers: authed(map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example"})},
		"not json": {method: "POST", path: "/api/packages/allow", body: `keys=a`,
			headers: authed(map[string]string{"Content-Type": "text/plain"})},
	}
	for name, r := range cases {
		if rec := do(t, s, r); rec.Code == http.StatusOK {
			t.Errorf("%s: approved from outside the page", name)
		}
	}
	if len(desk.allowed) != 0 {
		t.Errorf("allowed %v", desk.allowed)
	}
}

func TestAPageWithNoDeskHasNoPackagesTab(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if rec := do(t, s, req{path: "/api/packages", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("status %d", rec.Code)
	}
}

func TestRequestsWaitingDrawAttentionToTheAdminTabAndSayHowMany(t *testing.T) {
	desk := &fakeDesk{}
	s := packagesServer(desk)
	attention := func() []string {
		rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
		var st struct {
			Attention map[string][]string `json:"attention"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		return st.Attention["admin"]
	}
	if got := attention(); len(got) != 0 {
		t.Errorf("nothing waiting, yet: %v", got)
	}
	desk.pending = append(demoPending(), demoPending()[0])
	desk.pending[1].Key = "other"
	got := attention()
	if len(got) != 1 || !strings.Contains(got[0], "2 package requests") {
		t.Errorf("attention = %v", got)
	}
}

func TestTheDeskIsPolledForRequestsUntilTheServerEnds(t *testing.T) {
	desk := &fakeDesk{}
	s := packagesServer(desk)
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() { s.pollPackages(done, time.Millisecond); close(ended) }()
	deadline := time.After(5 * time.Second)
	for {
		desk.mu.Lock()
		n := desk.polls
		desk.mu.Unlock()
		if n >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("polled %d times", n)
		case <-time.After(time.Millisecond):
		}
	}
	close(done)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("still polling after the server ended")
	}
}
