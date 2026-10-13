package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeInstructor is an Admin with the instructions editors.
type fakeInstructor struct {
	fakeAdmin
	saved     []AdminSave
	conflict  bool
	optimised chan context.Context
}

func (f *fakeInstructor) Instructions() AdminInstructions {
	return AdminInstructions{Agent: "claude", Applies: true, CanOptimise: true,
		Files: []AdminInstructionsFile{{Which: "universal", Text: "x", Version: "v1", Editable: true}}}
}

func (f *fakeInstructor) SaveInstructions(save AdminSave) (AdminSaved, error) {
	f.saved = append(f.saved, save)
	if f.conflict && save.Resolve == "" {
		return AdminSaved{Conflict: true, DiskText: "theirs", CanMerge: true}, nil
	}
	return AdminSaved{Saved: true, Version: "v2", Updated: []string{"box"}}, nil
}

func (f *fakeInstructor) Optimise(ctx context.Context, which, text string) (string, error) {
	if f.optimised != nil {
		f.optimised <- ctx
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "tidier " + text, nil
}

func TestTheEditorsSaveAndSayWhenTheFileChangedOnDisk(t *testing.T) {
	f := &fakeInstructor{conflict: true}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, Admin: f})
	if rec := do(t, s, req{path: "/api/admin/instructions", headers: authed(nil)}); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"canOptimise":true`) {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	rec := do(t, s, post("/api/admin/instructions", `{"which":"universal","text":"mine","base":"x","version":"v1"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"diskText":"theirs"`) {
		t.Errorf("conflict: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, s, post("/api/admin/instructions", `{"which":"universal","text":"mine","base":"x","version":"v1","resolve":"merge"}`))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"updated":["box"]`) {
		t.Errorf("merge: %d %s", rec.Code, rec.Body)
	}
	if code := do(t, s, post("/api/admin/instructions", `{"which":"../etc/passwd","text":"x"}`)).Code; code != http.StatusBadRequest {
		t.Errorf("an unknown file: %d", code)
	}
}

func TestOptimiseAnswersWithTheAgentsVersion(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, Admin: &fakeInstructor{}})
	rec := do(t, s, post("/api/admin/instructions/optimise", `{"which":"project","text":"rules"}`))
	var got struct {
		Text string `json:"text"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got.Text != "tidier rules" {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestCancellingOptimiseStopsTheAgent(t *testing.T) {
	f := &fakeInstructor{optimised: make(chan context.Context, 1)}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, Admin: f})
	r := post("/api/admin/instructions/optimise", `{"which":"project","text":"rules"}`)
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	hr.Host = "localhost:" + itoa(testPort)
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hr = hr.WithContext(ctx)
	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(httptest.NewRecorder(), hr); close(done) }()
	agentCtx := <-f.optimised
	cancel() // the page's Cancel button aborts its request
	select {
	case <-agentCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the agent's context didn't end with the request")
	}
	<-done
}

func TestAnAdminWithoutEditorsHasNoInstructionsEndpoints(t *testing.T) {
	s := adminServer(&fakeAdmin{}, nil)
	if code := do(t, s, req{path: "/api/admin/instructions", headers: authed(nil)}).Code; code != http.StatusNotFound {
		t.Errorf("GET: %d", code)
	}
}
