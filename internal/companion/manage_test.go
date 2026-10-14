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

// fakeManager is the vibe side of the Profiles tab: it refuses to plan for
// ids in refuse, records what it deletes, and deletes when release is
// closed (at once when it's nil).
type fakeManager struct {
	mu      sync.Mutex
	refuse  string
	deleted [][]string
	release chan struct{}
	asked   int
	done    []ManageAction
}

func (f *fakeManager) Inventory() ManageInventory {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	return ManageInventory{Profiles: []ManageProfile{{ID: "profile:p", Name: "p", Trash: true}}}
}

func (f *fakeManager) Plan(ids []string) ([]string, error) {
	for _, id := range ids {
		if id == f.refuse {
			return nil, errors.New(id + " can't be deleted: a vibe is attached to it")
		}
	}
	return []string{"profile 'p': ~/.vibe/profiles/p"}, nil
}

func (f *fakeManager) Delete(ctx context.Context, ids []string, log func(string)) error {
	log("deleting " + strings.Join(ids, ","))
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.deleted = append(f.deleted, ids)
	f.mu.Unlock()
	return nil
}

func (f *fakeManager) CanDo(action ManageAction) error {
	if action.ID == f.refuse {
		return errors.New(action.ID + " can't be rebuilt: a vibe is attached to it")
	}
	return nil
}

func (f *fakeManager) Do(ctx context.Context, action ManageAction, log func(string)) error {
	log(action.Kind + " " + action.ID)
	f.mu.Lock()
	f.done = append(f.done, action)
	f.mu.Unlock()
	return nil
}

type manageResult struct {
	Inventory ManageInventory `json:"inventory"`
	Job       *adminJob       `json:"job"`
}

func managePoll(t *testing.T, s *Server) manageResult {
	t.Helper()
	rec := do(t, s, req{path: "/api/manage", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/manage: %d %s", rec.Code, rec.Body)
	}
	var r manageResult
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func manageDone(t *testing.T, s *Server) manageResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := managePoll(t, s); r.Job != nil && !r.Job.Running {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the delete never ended")
	return manageResult{}
}

func TestWithoutAManagerThereIsNoProfilesTab(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if code := do(t, s, req{path: "/api/manage", headers: authed(nil)}).Code; code != http.StatusNotFound {
		t.Errorf("GET /api/manage: %d", code)
	}
	if code := do(t, s, post("/api/manage/delete", `{"ids":["profile:p"]}`)).Code; code != http.StatusNotFound {
		t.Errorf("POST /api/manage/delete: %d", code)
	}
}

// A page for no sandbox is the Profiles tab alone.
func TestStateSaysWhichTabsThePageHas(t *testing.T) {
	for _, tc := range []struct {
		cfg               Config
		manage, sandboxes bool
	}{
		{Config{Sandbox: "box", Token: token, Port: testPort, Manage: &fakeManager{}}, true, true},
		{Config{Token: token, Port: testPort, Manage: &fakeManager{}}, true, false},
		{Config{Sandbox: "box", Token: token, Port: testPort}, false, true},
	} {
		var state struct {
			Manage      bool `json:"manage"`
			SandboxTabs bool `json:"sandboxTabs"`
		}
		json.Unmarshal(do(t, New(tc.cfg), req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &state)
		if state.Manage != tc.manage || state.SandboxTabs != tc.sandboxes {
			t.Errorf("sandbox %q, manager %v: state = %+v", tc.cfg.Sandbox, tc.cfg.Manage != nil, state)
		}
	}
}

func TestTheProfilesTabIsProtectedLikeEveryChange(t *testing.T) {
	f := &fakeManager{}
	s := New(Config{Token: token, Port: testPort, Manage: f})
	if code := do(t, s, req{path: "/api/manage"}).Code; code != http.StatusUnauthorized {
		t.Errorf("inventory without a token: %d", code)
	}
	other := post("/api/manage/delete", `{"ids":["profile:p"]}`)
	other.headers["Origin"] = "http://evil.example"
	if code := do(t, s, other).Code; code == http.StatusAccepted {
		t.Error("a delete from another origin was taken")
	}
	if code := do(t, s, post("/api/manage/delete", `{"ids":[]}`)).Code; code != http.StatusBadRequest {
		t.Errorf("a delete picking nothing: %d", code)
	}
	if len(f.deleted) != 0 {
		t.Errorf("deleted %v", f.deleted)
	}
}

func TestThePlanSaysWhatWillGoOrWhyNot(t *testing.T) {
	s := New(Config{Token: token, Port: testPort, Manage: &fakeManager{refuse: "sandbox:busy"}})
	rec := do(t, s, post("/api/manage/plan", `{"ids":["profile:p"]}`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "~/.vibe/profiles/p") {
		t.Errorf("plan: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, s, post("/api/manage/plan", `{"ids":["profile:p","sandbox:busy"]}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "a vibe is attached") {
		t.Errorf("plan refused: %d %s", rec.Code, rec.Body)
	}
}

func TestADeleteRunsInTheBackground(t *testing.T) {
	f := &fakeManager{}
	s := New(Config{Token: token, Port: testPort, Manage: f})
	if code := do(t, s, post("/api/manage/delete", `{"ids":["profile:p","unknown:x"]}`)).Code; code != http.StatusAccepted {
		t.Fatalf("delete: %d", code)
	}
	r := manageDone(t, s)
	if r.Job.Kind != "delete" || r.Job.Error != "" || strings.Join(r.Job.Lines, "|") != "deleting profile:p,unknown:x" {
		t.Errorf("job = %+v", r.Job)
	}
	if len(f.deleted) != 1 || strings.Join(f.deleted[0], ",") != "profile:p,unknown:x" {
		t.Errorf("deleted = %v", f.deleted)
	}
	if len(r.Inventory.Profiles) != 1 {
		t.Errorf("inventory = %+v", r.Inventory)
	}
}

func TestARefusedDeleteNeverStarts(t *testing.T) {
	f := &fakeManager{refuse: "sandbox:busy"}
	s := New(Config{Token: token, Port: testPort, Manage: f})
	rec := do(t, s, post("/api/manage/delete", `{"ids":["sandbox:busy"]}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "a vibe is attached") {
		t.Errorf("delete: %d %s", rec.Code, rec.Body)
	}
	if r := managePoll(t, s); r.Job != nil {
		t.Errorf("a job was started: %+v", r.Job)
	}
}

// The Admin and Profiles tabs share the page's one job at a time.
func TestADeleteWaitsForTheAdminTabsAction(t *testing.T) {
	a := &fakeAdmin{release: make(chan struct{})}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, Admin: a, Manage: &fakeManager{}})
	do(t, s, post("/api/admin/do", `{"kind":"reinit"}`))
	if code := do(t, s, post("/api/manage/delete", `{"ids":["profile:p"]}`)).Code; code != http.StatusConflict {
		t.Errorf("a delete while the Admin tab's rebuild runs: %d", code)
	}
	close(a.release)
	adminDone(t, s)
}

func TestADeleteCanBeCancelledFromThePageOrATerminal(t *testing.T) {
	f := &fakeManager{release: make(chan struct{})}
	s := New(Config{Token: token, Port: testPort, Manage: f})
	do(t, s, post("/api/manage/delete", `{"ids":["profile:p"]}`))
	if ended := s.Cancel(); strings.Join(ended, ",") != "the Profiles tab's delete" {
		t.Errorf("Cancel() = %v", ended)
	}
	if r := manageDone(t, s); !r.Job.Cancelled || len(f.deleted) != 0 {
		t.Errorf("job = %+v, deleted %v", r.Job, f.deleted)
	}
	f.release = make(chan struct{})
	do(t, s, post("/api/manage/delete", `{"ids":["profile:p"]}`))
	if rec := do(t, s, post("/api/manage/cancel", `{}`)); !strings.Contains(rec.Body.String(), `"cancelled":true`) {
		t.Errorf("cancel: %s", rec.Body)
	}
	manageDone(t, s)
}

func TestAnActionRunsAsThePagesJob(t *testing.T) {
	f := &fakeManager{}
	s := New(Config{Token: token, Port: testPort, Manage: f})
	if code := do(t, s, post("/api/manage/do", `{"kind":"rebuild","id":"sandbox:a"}`)).Code; code != http.StatusAccepted {
		t.Fatalf("rebuild: %d", code)
	}
	r := manageDone(t, s)
	if r.Job.Kind != "rebuild" || strings.Join(r.Job.Lines, "|") != "rebuild sandbox:a" || len(f.done) != 1 {
		t.Errorf("job = %+v, done = %+v", r.Job, f.done)
	}
}

func TestAnActionThatCantBeDoneSaysWhyAndNeverStarts(t *testing.T) {
	f := &fakeManager{refuse: "sandbox:busy"}
	s := New(Config{Token: token, Port: testPort, Manage: f})
	rec := do(t, s, post("/api/manage/do", `{"kind":"rebuild","id":"sandbox:busy"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "a vibe is attached") {
		t.Errorf("rebuild: %d %s", rec.Code, rec.Body)
	}
	for _, body := range []string{`{"kind":"rm -rf","id":"sandbox:a"}`, `{"kind":"start"}`} {
		if code := do(t, s, post("/api/manage/do", body)).Code; code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, code)
		}
	}
	if r := managePoll(t, s); r.Job != nil || len(f.done) != 0 {
		t.Errorf("job = %+v, done = %+v", r.Job, f.done)
	}
}
