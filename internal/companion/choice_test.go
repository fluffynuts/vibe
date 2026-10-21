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
	// what was kept of the lists from an earlier run, and what was kept since
	cached      *Options
	cachedAt    time.Time
	remembered  []Options
	rememberErr error
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
		Cached: func() (Options, time.Time, bool) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.cached == nil {
				return Options{}, time.Time{}, false
			}
			return *c.cached, c.cachedAt, true
		},
		Remember: func(o Options) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.remembered = append(c.remembered, o)
			return c.rememberErr
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
	Model        string   `json:"model"`
	Effort       string   `json:"effort"`
	Running      string   `json:"running"`
	Session      bool     `json:"session"`
	Options      *Options `json:"options"`
	OptionsAt    int64    `json:"optionsAt"`
	OptionsFresh bool     `json:"optionsFresh"`
	Error        string   `json:"error"`
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
	if c.woke != 0 {
		t.Error("the sandbox was woken though the page didn't ask for that")
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

func TestTheSandboxIsWokenToBeAskedOnlyWhenThePageSaysSo(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s := choiceServer(t, c)
	if rec := do(t, s, post("/api/conversation/choice/options", `{"wake":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if c.woke != 1 {
		t.Errorf("woken %d times, want 1", c.woke)
	}
}

func TestASandboxThatCantBeWokenIsSaidAndNotAsked(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s := choiceServer(t, c)
	s.cfg.Wake = func(ctx context.Context, say func(string)) error { return errors.New("sbx run failed") }
	rec := do(t, s, post("/api/conversation/choice/options", `{"wake":true}`))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "sbx run failed") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if c.asked != 0 {
		t.Errorf("asked %d times", c.asked)
	}
}

var oldOptions = Options{Models: []string{"sonnet", "opus"}, Efforts: []string{"low", "high"}, Current: "Sonnet 4.5 (default)"}

func TestListsFromAnEarlierRunAreGivenAtOnceWithoutAskingTheAgent(t *testing.T) {
	long := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	c := &choosing{cached: &oldOptions, cachedAt: long, options: claudeOptions}
	s := choiceServer(t, c)
	got := readChoice(t, s)
	if got.Options == nil || len(got.Options.Models) != 2 || got.Options.Current != "Sonnet 4.5 (default)" {
		t.Fatalf("the page isn't given the kept lists: %+v", got.Options)
	}
	if got.OptionsFresh {
		t.Error("lists from an earlier run are said to be fresh")
	}
	if got.OptionsAt != long.UnixMilli() {
		t.Errorf("they're from %d, want %d", got.OptionsAt, long.UnixMilli())
	}
	if c.asked != 0 {
		t.Errorf("the agent was asked %d times just to read the page", c.asked)
	}
}

func TestAskingAgainReplacesTheKeptListsAndIsRememberedForNextTime(t *testing.T) {
	c := &choosing{cached: &oldOptions, cachedAt: time.Now().Add(-48 * time.Hour), options: claudeOptions}
	s := choiceServer(t, c)
	rec := do(t, s, post("/api/conversation/choice/options", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := readChoice(t, s)
	if got.Options == nil || len(got.Options.Models) != 3 || !got.OptionsFresh {
		t.Errorf("after asking: %+v fresh %v", got.Options, got.OptionsFresh)
	}
	if len(c.remembered) != 1 || len(c.remembered[0].Models) != 3 {
		t.Errorf("remembered %+v", c.remembered)
	}
	if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusOK || c.asked != 1 {
		t.Errorf("a second ask went to the agent again: asked %d", c.asked)
	}
}

func TestWhenTheAgentCantBeAskedTheKeptListsStayAndTheFailureIsSaid(t *testing.T) {
	c := &choosing{cached: &oldOptions, cachedAt: time.Now().Add(-48 * time.Hour), optsErr: errors.New("sandbox is stopped")}
	s := choiceServer(t, c)
	rec := do(t, s, post("/api/conversation/choice/options", `{}`))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "sandbox is stopped") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	got := readChoice(t, s)
	if got.Options == nil || len(got.Options.Models) != 2 || got.OptionsFresh {
		t.Errorf("the kept lists were lost or called fresh: %+v fresh %v", got.Options, got.OptionsFresh)
	}
	if len(c.remembered) != 0 {
		t.Errorf("a failure was remembered: %+v", c.remembered)
	}
}

func TestAnEffortIsOnlyHeldToTheAgentsListOnceItIsFreshNotToOneFromAnEarlierRun(t *testing.T) {
	c := &choosing{cached: &oldOptions, cachedAt: time.Now().Add(-48 * time.Hour), options: claudeOptions}
	s := choiceServer(t, c)
	if rec := do(t, s, post("/api/conversation/choice", `{"effort":"xhigh"}`)); rec.Code != http.StatusOK {
		t.Errorf("an effort the old lists lack was refused though they may be out of date: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/conversation/choice", `{"effort":"xhigh"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("an effort the agent's own list lacks was kept: %d %s", rec.Code, rec.Body)
	}
}

func TestAFailureToRememberTheListsIsSaidInVibesOutputAndDoesntFailTheRequest(t *testing.T) {
	c := &choosing{options: claudeOptions, rememberErr: errors.New("disk full")}
	s := choiceServer(t, c)
	var said []string
	s.cfg.Log = func(line string) { said = append(said, line) }
	if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(said) != 1 || !strings.Contains(said[0], "disk full") {
		t.Errorf("said %q", said)
	}
}

func TestWithNothingKeptAndNothingAskedThePageHasNoLists(t *testing.T) {
	s := choiceServer(t, &choosing{options: claudeOptions})
	if got := readChoice(t, s); got.Options != nil || got.OptionsFresh {
		t.Errorf("got %+v", got)
	}
}
