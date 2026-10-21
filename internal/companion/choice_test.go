package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// choosing is the model and effort kept for the page, and the agent's lists.
type choosing struct {
	mu       sync.Mutex
	model    string
	effort   string
	setErr   error
	options  Options
	optsErr  error
	asked    int
	woke     int
	setCalls [][2]string
}

func (c *choosing) config() *Choice {
	return &Choice{
		Get: func() (string, string, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.model, c.effort, nil
		},
		Set: func(model, effort string) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.setCalls = append(c.setCalls, [2]string{model, effort})
			if c.setErr != nil {
				return c.setErr
			}
			c.model, c.effort = model, effort
			return nil
		},
		Options: func(ctx context.Context) (Options, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.asked++
			return c.options, c.optsErr
		},
	}
}

var claudeOptions = Options{
	Models: []string{"sonnet", "opus", "haiku"}, OtherModels: []string{"best", "opus[1m]"},
	Efforts: []string{"low", "medium", "high"}, Current: "Opus 5.5 (default)",
}

func choiceServer(t *testing.T, c *choosing) *Server {
	t.Helper()
	s := newServer(&fakeSbx{})
	s.cfg.Converse = (&fakeAgent{}).converse
	s.cfg.Choice = c.config()
	s.cfg.Wake = func(ctx context.Context, say func(string)) error {
		c.mu.Lock()
		c.woke++
		c.mu.Unlock()
		return nil
	}
	return s
}

type choiceState struct {
	Model   string   `json:"model"`
	Effort  string   `json:"effort"`
	Running string   `json:"running"`
	Session bool     `json:"session"`
	Options *Options `json:"options"`
	Error   string   `json:"error"`
}

func readChoice(t *testing.T, s *Server) choiceState {
	t.Helper()
	rec := do(t, s, req{path: "/api/conversation/choice", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET choice: %d %s", rec.Code, rec.Body)
	}
	var got choiceState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestThePageIsToldWhatIsChosen(t *testing.T) {
	s := choiceServer(t, &choosing{model: "opus", effort: "high"})
	if got := readChoice(t, s); got.Model != "opus" || got.Effort != "high" {
		t.Errorf("got %+v", got)
	}
}

func TestThePageIsToldWhichModelTheAgentSaysItIsRunning(t *testing.T) {
	s := choiceServer(t, &choosing{})
	if got := readChoice(t, s); got.Running != "" {
		t.Errorf("before any turn: running = %q", got.Running)
	}
	s.convo.line([]byte(`{"type":"system","subtype":"init","model":"claude-haiku-5-5","session_id":"c403b0d5-2ae0-42d6-8d1f-8590da103c98"}`))
	got := readChoice(t, s)
	if got.Running != "claude-haiku-5-5" || !got.Session {
		t.Errorf("got %+v, want running claude-haiku-5-5 and a session", got)
	}
}

func TestWhatTheAgentSaidItRanSurvivesARestartOfTheConversation(t *testing.T) {
	file := t.TempDir() + "/c.json"
	first := &conversation{}
	if err := first.load(file); err != nil {
		t.Fatal(err)
	}
	first.line([]byte(`{"type":"system","subtype":"init","model":"claude-haiku-5-5"}`))
	first.close()
	again := &conversation{}
	if err := again.load(file); err != nil {
		t.Fatal(err)
	}
	if got := again.ranModel(); got != "claude-haiku-5-5" {
		t.Errorf("ran = %q", got)
	}
}

func TestChoosingKeepsTheModelAndEffort(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s := choiceServer(t, c)
	rec := do(t, s, post("/api/conversation/choice", `{"model":"sonnet","effort":""}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if c.model != "sonnet" || c.effort != "" {
		t.Errorf("kept %q, %q", c.model, c.effort)
	}
	if got := readChoice(t, s); got.Model != "sonnet" {
		t.Errorf("then read %+v", got)
	}
}

func TestChangingTheModelResetsTheEffortToTheDefault(t *testing.T) {
	c := &choosing{model: "opus", effort: "high", options: claudeOptions}
	s := choiceServer(t, c)
	rec := do(t, s, post("/api/conversation/choice", `{"model":"sonnet","effort":"high"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if c.model != "sonnet" || c.effort != "" {
		t.Errorf("kept %q, %q; want sonnet and the default effort", c.model, c.effort)
	}
}

func TestChangingOnlyTheEffortKeepsTheModel(t *testing.T) {
	c := &choosing{model: "opus", effort: "high", options: claudeOptions}
	s := choiceServer(t, c)
	if rec := do(t, s, post("/api/conversation/choice", `{"model":"opus","effort":"low"}`)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if c.model != "opus" || c.effort != "low" {
		t.Errorf("kept %q, %q", c.model, c.effort)
	}
}

func TestAModelThatCouldBeShellIsRefusedAndNothingIsKept(t *testing.T) {
	c := &choosing{model: "opus"}
	s := choiceServer(t, c)
	for _, model := range []string{"x; rm -rf /", "$(id)", "a b", "-x", "a'b"} {
		body, _ := json.Marshal(map[string]string{"model": model})
		rec := do(t, s, post("/api/conversation/choice", string(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: %d %s", model, rec.Code, rec.Body)
		}
	}
	if len(c.setCalls) != 0 || c.model != "opus" {
		t.Errorf("something was kept: %v, %q", c.setCalls, c.model)
	}
}

func TestAnEffortTheAgentDoesNotTakeIsRefusedWithWhatItDoesTake(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s := choiceServer(t, c)
	if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("options: %d %s", rec.Code, rec.Body)
	}
	rec := do(t, s, post("/api/conversation/choice", `{"model":"","effort":"ultracode"}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "low, medium, high") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if len(c.setCalls) != 0 {
		t.Errorf("kept %v", c.setCalls)
	}
}

func TestAnEffortIsStillCheckedForShapeBeforeTheAgentsListIsKnown(t *testing.T) {
	c := &choosing{}
	s := choiceServer(t, c)
	if rec := do(t, s, post("/api/conversation/choice", `{"effort":"high"}`)); rec.Code != http.StatusOK {
		t.Errorf("high: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/conversation/choice", `{"effort":"$(id)"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("$(id): %d %s", rec.Code, rec.Body)
	}
}

func TestWhenTheChoiceCantBeKeptThePageIsToldWhy(t *testing.T) {
	s := choiceServer(t, &choosing{setErr: errors.New("profile 'p' isn't there to keep the choice in")})
	rec := do(t, s, post("/api/conversation/choice", `{"model":"opus"}`))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "profile 'p'") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestTheAgentsListsAreAskedForOnceAndKept(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s := choiceServer(t, c)
	for i := 0; i < 2; i++ {
		rec := do(t, s, post("/api/conversation/choice/options", `{}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	if c.asked != 1 {
		t.Errorf("the agent was asked %d times, want 1", c.asked)
	}
	if c.woke == 0 {
		t.Error("the sandbox wasn't woken to be asked")
	}
	got := readChoice(t, s)
	if got.Options == nil || len(got.Options.Models) != 3 || got.Options.Current != "Opus 5.5 (default)" {
		t.Errorf("the page isn't given the lists it was fetched: %+v", got.Options)
	}
}

func TestAFailureToAskForTheListsIsSaidAndNotKeptSoTheNextTryAsksAgain(t *testing.T) {
	c := &choosing{optsErr: errors.New("the agent's answer to /model lists no models")}
	s := choiceServer(t, c)
	rec := do(t, s, post("/api/conversation/choice/options", `{}`))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "lists no models") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	c.optsErr, c.options = nil, claudeOptions
	if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusOK {
		t.Errorf("second try: %d %s", rec.Code, rec.Body)
	}
	if c.asked != 2 {
		t.Errorf("asked %d times, want 2", c.asked)
	}
}

func TestWithNothingToChooseFromTheRoutesAreNotThere(t *testing.T) {
	s := newServer(&fakeSbx{})
	s.cfg.Converse = (&fakeAgent{}).converse
	for _, path := range []string{"/api/conversation/choice", "/api/conversation/choice/options"} {
		method := "POST"
		if path == "/api/conversation/choice" {
			method = "GET"
		}
		rec := do(t, s, req{method: method, path: path, body: "{}", headers: authed(map[string]string{"Content-Type": "application/json"})})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", method, path, rec.Code)
		}
	}
}

func TestThePageIsToldItCanChooseOnlyWhenItCan(t *testing.T) {
	s := choiceServer(t, &choosing{})
	if !statusFlag(t, s, "choice") {
		t.Error("choice is off though there is something to choose")
	}
	s.cfg.Choice = nil
	if statusFlag(t, s, "choice") {
		t.Error("choice is on with nothing to choose")
	}
}

func statusFlag(t *testing.T, s *Server, key string) bool {
	t.Helper()
	rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	on, _ := body[key].(bool)
	return on
}

func TestANewConversationHasNotRunAnythingYet(t *testing.T) {
	s := choiceServer(t, &choosing{})
	s.convo.line([]byte(`{"type":"system","subtype":"init","model":"claude-haiku-5-5","session_id":"c403b0d5-2ae0-42d6-8d1f-8590da103c98"}`))
	if !s.convo.afresh("", nil) {
		t.Fatal("couldn't begin afresh")
	}
	if got := readChoice(t, s); got.Running != "" || got.Session {
		t.Errorf("got %+v, want nothing run and no session", got)
	}
}
