package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAdmin is the vibe side of the Admin tab: it records what it was asked
// to do, and does it when release is closed (at once when it's nil).
type fakeAdmin struct {
	mu       sync.Mutex
	done     []AdminAction
	statuses int
	checks   []bool
	release  chan struct{}
	err      error
}

func (f *fakeAdmin) Status() AdminStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses++
	return AdminStatus{Exists: true, Running: true, Profile: "p", Info: "name      box\n"}
}

func (f *fakeAdmin) Upgrades(fresh bool) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, fresh)
	return []string{"vibe 1.2.3"}
}

func (f *fakeAdmin) Do(ctx context.Context, action AdminAction, log func(string)) error {
	log("doing " + action.Kind)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.done = append(f.done, action)
	f.mu.Unlock()
	log("done")
	return f.err
}

func adminServer(f *fakeAdmin, agent *fakeAgent) *Server {
	cfg := Config{Sandbox: "box", Token: token, Port: testPort, Admin: f}
	if agent != nil {
		cfg.Converse = agent.converse
	}
	return New(cfg)
}

type adminResult struct {
	Status   AdminStatus `json:"status"`
	Job      *adminJob   `json:"job"`
	Upgrades struct {
		Found    []string `json:"found"`
		Checked  bool     `json:"checked"`
		Checking bool     `json:"checking"`
	} `json:"upgrades"`
}

func adminPoll(t *testing.T, s *Server) adminResult {
	t.Helper()
	rec := do(t, s, req{path: "/api/admin", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin: %d %s", rec.Code, rec.Body)
	}
	var r adminResult
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// adminDone waits for the action under way to end.
func adminDone(t *testing.T, s *Server) adminResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := adminPoll(t, s); r.Job != nil && !r.Job.Running {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the action never ended")
	return adminResult{}
}

func TestWithoutAdminThereIsNoAdminTab(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if code := do(t, s, req{path: "/api/admin", headers: authed(nil)}).Code; code != http.StatusNotFound {
		t.Errorf("GET /api/admin: %d", code)
	}
	if code := do(t, s, post("/api/admin/do", `{"kind":"stop"}`)).Code; code != http.StatusNotFound {
		t.Errorf("POST /api/admin/do: %d", code)
	}
	var state struct {
		Admin bool `json:"admin"`
	}
	json.Unmarshal(do(t, s, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &state)
	if state.Admin {
		t.Error("state says there is an admin tab")
	}
}

func TestAdminIsProtectedLikeEveryChange(t *testing.T) {
	s := adminServer(&fakeAdmin{}, nil)
	if code := do(t, s, req{path: "/api/admin"}).Code; code != http.StatusUnauthorized {
		t.Errorf("status without a token: %d", code)
	}
	noType := req{method: "POST", path: "/api/admin/do", body: `{"kind":"stop"}`, headers: authed(nil)}
	if code := do(t, s, noType).Code; code == http.StatusAccepted {
		t.Error("an action that isn't JSON was taken")
	}
	other := post("/api/admin/do", `{"kind":"stop"}`)
	other.headers["Origin"] = "http://evil.example"
	if code := do(t, s, other).Code; code == http.StatusAccepted {
		t.Error("an action from another origin was taken")
	}
	if code := do(t, s, post("/api/admin/do", `{"kind":"rm -rf"}`)).Code; code != http.StatusBadRequest {
		t.Errorf("an unknown action: %d", code)
	}
}

func TestAnActionRunsInTheBackgroundAndSaysWhatItDid(t *testing.T) {
	f := &fakeAdmin{}
	s := adminServer(f, nil)
	if code := do(t, s, post("/api/admin/do", `{"kind":"recompose","features":["mysql","dashboard"]}`)).Code; code != http.StatusAccepted {
		t.Fatalf("recompose: %d", code)
	}
	r := adminDone(t, s)
	if r.Job.Kind != "recompose" || r.Job.Error != "" || strings.Join(r.Job.Lines, "|") != "doing recompose|done" {
		t.Errorf("job = %+v", r.Job)
	}
	if len(f.done) != 1 || strings.Join(f.done[0].Features, ",") != "mysql,dashboard" {
		t.Errorf("done = %+v", f.done)
	}
	if !r.Status.Running || r.Status.Profile != "p" {
		t.Errorf("status = %+v", r.Status)
	}
}

func TestAFailedActionSaysWhy(t *testing.T) {
	s := adminServer(&fakeAdmin{err: errors.New("sbx said no")}, nil)
	do(t, s, post("/api/admin/do", `{"kind":"start"}`))
	if r := adminDone(t, s); r.Job.Error != "sbx said no" {
		t.Errorf("job = %+v", r.Job)
	}
}

func TestOnlyOneActionAtATime(t *testing.T) {
	f := &fakeAdmin{release: make(chan struct{})}
	s := adminServer(f, nil)
	do(t, s, post("/api/admin/do", `{"kind":"reinit"}`))
	if code := do(t, s, post("/api/admin/do", `{"kind":"stop"}`)).Code; code != http.StatusConflict {
		t.Errorf("a second action while the first runs: %d", code)
	}
	close(f.release)
	adminDone(t, s)
	if code := do(t, s, post("/api/admin/do", `{"kind":"stop"}`)).Code; code != http.StatusAccepted {
		t.Errorf("the next action: %d", code)
	}
}

func TestTheSandboxIsNotTakenFromUnderAnAnsweringAgent(t *testing.T) {
	agent := &fakeAgent{release: make(chan struct{})}
	f := &fakeAdmin{}
	s := adminServer(f, agent)
	send(t, s, "/api/conversation/send", `{"prompt":"work"}`)
	for _, kind := range []string{"stop", "reinit", "recompose", "restore"} {
		if code := do(t, s, post("/api/admin/do", `{"kind":"`+kind+`"}`)).Code; code != http.StatusConflict {
			t.Errorf("%s while the agent answers: %d", kind, code)
		}
	}
	// Nothing taken away: these can go ahead.
	if code := do(t, s, post("/api/admin/do", `{"kind":"memories"}`)).Code; code != http.StatusAccepted {
		t.Errorf("memories while the agent answers: %d", code)
	}
	close(agent.release)
	idle(t, s)
}

func TestWhatTheAgentLeftRunningEndsQuietlyForAnAction(t *testing.T) {
	agent := &fakeAgent{linger: make(chan struct{})}
	defer close(agent.linger)
	s := adminServer(&fakeAdmin{}, agent)
	send(t, s, "/api/conversation/send", `{"prompt":"review"}`)
	answered(t, s)
	if code := do(t, s, post("/api/admin/do", `{"kind":"stop"}`)).Code; code != http.StatusAccepted {
		t.Fatalf("stop after the agent answered: %d", code)
	}
	adminDone(t, s)
	p := idle(t, s)
	if last := p.Events[len(p.Events)-1]; last.Kind != "done" || last.Text != "" {
		t.Errorf("the turn ended with %+v", last)
	}
}

func TestUpgradesAreCheckedOnRequest(t *testing.T) {
	f := &fakeAdmin{}
	s := adminServer(f, nil)
	if r := adminPoll(t, s); r.Upgrades.Checked {
		t.Error("checked before anything asked")
	}
	if code := do(t, s, post("/api/admin/upgrades", `{}`)).Code; code != http.StatusAccepted {
		t.Fatalf("check: %d", code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		r := adminPoll(t, s)
		if r.Upgrades.Checked {
			if strings.Join(r.Upgrades.Found, ",") != "vibe 1.2.3" {
				t.Errorf("found %v", r.Upgrades.Found)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the check never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(f.checks) != 1 || !f.checks[0] {
		t.Errorf("checks = %v, want one fresh one", f.checks)
	}
}

func TestTheStatusIsSharedBetweenQuickPolls(t *testing.T) {
	f := &fakeAdmin{}
	s := adminServer(f, nil)
	adminPoll(t, s)
	adminPoll(t, s)
	if f.statuses != 1 {
		t.Errorf("status asked %d times, want 1", f.statuses)
	}
}

func TestAnActionThatRunsFarTooLongSaysSoAndKeepsWaiting(t *testing.T) {
	was := adminSlow
	adminSlow = map[string]time.Duration{AdminReInit: 20 * time.Millisecond}
	defer func() { adminSlow = was }()
	f := &fakeAdmin{release: make(chan struct{})}
	s := adminServer(f, nil)
	do(t, s, post("/api/admin/do", `{"kind":"reinit"}`))

	deadline := time.Now().Add(2 * time.Second)
	var r adminResult
	for r = adminPoll(t, s); r.Job.Warning == "" && time.Now().Before(deadline); r = adminPoll(t, s) {
		time.Sleep(5 * time.Millisecond)
	}
	if !r.Job.Running || !strings.Contains(r.Job.Warning, "sbx may be stuck") || !strings.Contains(strings.Join(r.Job.Lines, "\n"), "sbx ls") {
		t.Fatalf("job = %+v", r.Job)
	}
	close(f.release) // it finishes after all: nothing was stopped
	if r := adminDone(t, s); r.Job.Error != "" || len(f.done) != 1 {
		t.Errorf("job = %+v, done = %v", r.Job, f.done)
	}
}

func TestAnActionInGoodTimeHasNoWarning(t *testing.T) {
	s := adminServer(&fakeAdmin{}, nil)
	do(t, s, post("/api/admin/do", `{"kind":"start"}`))
	if r := adminDone(t, s); r.Job.Warning != "" {
		t.Errorf("warning = %q", r.Job.Warning)
	}
}

func TestCancelEndsTheActionUnderWay(t *testing.T) {
	f := &fakeAdmin{release: make(chan struct{})}
	s := adminServer(f, nil)
	do(t, s, post("/api/admin/do", `{"kind":"reinit"}`))
	rec := do(t, s, post("/api/admin/cancel", `{}`))
	if !strings.Contains(rec.Body.String(), `"cancelled":true`) {
		t.Fatalf("cancel: %s", rec.Body)
	}
	r := adminDone(t, s)
	if !r.Job.Cancelled || r.Job.Error == "" || len(f.done) != 0 {
		t.Errorf("job = %+v, done = %v", r.Job, f.done)
	}
	if rec := do(t, s, post("/api/admin/cancel", `{}`)); !strings.Contains(rec.Body.String(), `"cancelled":false`) {
		t.Errorf("cancelling with nothing under way: %s", rec.Body)
	}
}

func TestCancelEndsTheTurnAndTheActionAndSaysWhich(t *testing.T) {
	agent := &fakeAgent{release: make(chan struct{})}
	defer close(agent.release)
	f := &fakeAdmin{release: make(chan struct{})}
	s := adminServer(f, agent)
	if got := s.Cancel(); len(got) != 0 {
		t.Errorf("with nothing under way: %v", got)
	}
	send(t, s, "/api/conversation/send", `{"prompt":"work"}`)
	do(t, s, post("/api/admin/do", `{"kind":"memories"}`))
	got := strings.Join(s.Cancel(), ", ")
	if got != "the agent's turn, the Admin tab's memory save" {
		t.Errorf("ended %q", got)
	}
	idle(t, s)
	if r := adminDone(t, s); !r.Job.Cancelled {
		t.Errorf("job = %+v", r.Job)
	}
}

// A tab drawing attention says why, so its dot is never a puzzle.
func TestTheAdminTabSaysWhyItWantsAttention(t *testing.T) {
	attention := func(s *Server) map[string][]string {
		t.Helper()
		rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
		var body struct {
			Attention map[string][]string `json:"attention"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Attention
	}
	s := adminServer(&fakeAdmin{}, nil)
	if got := attention(s); len(got) != 0 {
		t.Errorf("before any upgrade was found: %v", got)
	}
	s.admin.mu.Lock()
	s.admin.upgrades = []string{"vibe 1.2.3", "Docker SBX 0.48.0 → 0.49.0"}
	s.admin.mu.Unlock()
	got := attention(s)["admin"]
	if len(got) != 1 || !strings.Contains(got[0], "vibe 1.2.3") || !strings.Contains(got[0], "Docker SBX 0.48.0 → 0.49.0") {
		t.Errorf("the Admin tab's reasons: %v", got)
	}
}
