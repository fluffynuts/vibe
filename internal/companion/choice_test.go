package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	// model and effort are what the profile holds itself; the global settings
	// under them give what it doesn't. gate holds Options until it's closed.
	globalModel, globalEffort string
	gate                      chan struct{}
	cached                    *Options
	cachedAt                  time.Time
	remembered                []Options
	rememberErr               error
}

func (c *choosing) config() *Choice {
	return &Choice{
		Get: func() (string, string, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			model, effort := c.model, c.effort
			if model == "" {
				model = c.globalModel
			}
			if effort == "" {
				effort = c.globalEffort
			}
			return model, effort, nil
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
			c.asked++
			gate, opts, err := c.gate, c.options, c.optsErr
			c.mu.Unlock()
			if gate != nil {
				select {
				case <-gate:
				case <-ctx.Done():
					return Options{}, ctx.Err()
				}
			}
			return opts, err
		},
		Own: func() (string, string, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.model, c.effort, nil
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
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	Running   string `json:"running"`
	Session   bool   `json:"session"`
	Adopted   string `json:"adopted"`
	Inherited struct {
		Model  bool `json:"model"`
		Effort bool `json:"effort"`
	} `json:"inherited"`
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

func TestThePageIsToldWhenWhatIsChosenComesFromTheGlobalSettingsNotTheProfile(t *testing.T) {
	c := &choosing{globalModel: "opus", effort: "high"}
	got := readChoice(t, choiceServer(t, c))
	if !got.Inherited.Model || got.Inherited.Effort {
		t.Errorf("inherited = %+v, want the model and not the effort", got.Inherited)
	}
}

func TestNothingIsInheritedWhenTheProfileHoldsItAllItself(t *testing.T) {
	got := readChoice(t, choiceServer(t, &choosing{model: "opus", effort: "high"}))
	if got.Inherited.Model || got.Inherited.Effort {
		t.Errorf("inherited = %+v", got.Inherited)
	}
}

func TestNothingIsInheritedWhenNothingIsChosenAtAll(t *testing.T) {
	c := &choosing{}
	if got := readChoice(t, choiceServer(t, c)); got.Inherited.Model || got.Inherited.Effort {
		t.Errorf("inherited = %+v", got.Inherited)
	}
}

// Pinned, as it's easy to take for a bug: Default in the profile can't take away
// what the global settings.yaml sets, so the value stays in force and the page is
// told it comes from there.
func TestChoosingDefaultDoesNotTakeAwayWhatTheGlobalSettingsSetAndSaysWhereItComesFrom(t *testing.T) {
	c := &choosing{model: "sonnet", globalModel: "opus"}
	s := choiceServer(t, c)
	rec := do(t, s, post("/api/conversation/choice", `{"model":"","effort":""}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(c.setCalls) != 1 || c.setCalls[0] != [2]string{"", ""} {
		t.Errorf("the profile was asked to keep %v, want Default", c.setCalls)
	}
	var got choiceState
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Model != "opus" || !got.Inherited.Model {
		t.Errorf("got %+v: the global's opus should still be in force, and said to be inherited", got)
	}
}

func TestTwoPagesAskingAtOnceAskTheAgentOnce(t *testing.T) {
	c := &choosing{options: claudeOptions, gate: make(chan struct{})}
	s := choiceServer(t, c)
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() { codes <- do(t, s, post("/api/conversation/choice/options", `{}`)).Code }()
	}
	waitFor(t, "the agent to be asked", func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.asked >= 1 })
	time.Sleep(50 * time.Millisecond)
	close(c.gate)
	for i := 0; i < 2; i++ {
		if code := <-codes; code != http.StatusOK {
			t.Errorf("a page got %d", code)
		}
	}
	if c.asked != 1 {
		t.Errorf("the agent was asked %d times, want 1", c.asked)
	}
	if len(c.remembered) != 1 {
		t.Errorf("remembered %d times, want 1", len(c.remembered))
	}
}

func TestWhenTheFirstOfTwoAsksFailsTheSecondAsksForItself(t *testing.T) {
	c := &choosing{options: claudeOptions, optsErr: errors.New("sandbox is stopped"), gate: make(chan struct{})}
	s := choiceServer(t, c)
	first := make(chan int, 1)
	go func() { first <- do(t, s, post("/api/conversation/choice/options", `{}`)).Code }()
	waitFor(t, "the first ask", func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.asked == 1 })
	second := make(chan int, 1)
	go func() { second <- do(t, s, post("/api/conversation/choice/options", `{}`)).Code }()
	time.Sleep(50 * time.Millisecond)
	c.mu.Lock()
	c.optsErr = nil
	c.mu.Unlock()
	close(c.gate)
	if code := <-first; code != http.StatusBadGateway {
		t.Errorf("the first got %d, want 502", code)
	}
	if code := <-second; code != http.StatusOK {
		t.Errorf("the second got %d, want it to have asked for itself and been answered", code)
	}
	if c.asked != 2 {
		t.Errorf("asked %d times, want 2", c.asked)
	}
}

func TestAPageThatStopsWaitingIsNotWaitedFor(t *testing.T) {
	c := &choosing{options: claudeOptions, gate: make(chan struct{})}
	s := choiceServer(t, c)
	go do(t, s, post("/api/conversation/choice/options", `{}`))
	waitFor(t, "the first ask", func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.asked == 1 })
	ctx, cancel := context.WithCancel(context.Background())
	r := post("/api/conversation/choice/options", `{}`)
	done := make(chan int, 1)
	go func() {
		hr := httptest.NewRequest("POST", r.path, strings.NewReader(r.body)).WithContext(ctx)
		hr.Host = "localhost:" + strconv.Itoa(testPort)
		for k, v := range r.headers {
			hr.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, hr)
		done <- w.Code
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code < 400 {
			t.Errorf("got %d, want an error for a page that gave up", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a second page that gave up is still being held by the first's ask")
	}
	close(c.gate)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// freshServer is choiceServer with a clock the test moves.
func clockedChoiceServer(t *testing.T, c *choosing) (*Server, *time.Time) {
	t.Helper()
	s := choiceServer(t, c)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	s.clock = func() time.Time { return now }
	return s, &now
}

func TestListsAreAskedForAgainOnceTheyAreTenMinutesOld(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s, now := clockedChoiceServer(t, c)
	ask := func() {
		t.Helper()
		if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	ask()
	if c.asked != 1 {
		t.Fatalf("asked %d times, want 1", c.asked)
	}
	for _, age := range []time.Duration{time.Second, 5 * time.Minute, 10*time.Minute - time.Second} {
		*now = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC).Add(age)
		ask()
		if c.asked != 1 {
			t.Errorf("at %v old, the agent was asked again (%d)", age, c.asked)
		}
	}
	*now = time.Date(2026, 10, 22, 9, 10, 0, 0, time.UTC)
	ask()
	if c.asked != 2 {
		t.Errorf("at ten minutes old it wasn't asked again (%d)", c.asked)
	}
	ask()
	if c.asked != 2 {
		t.Errorf("asked a third time at once (%d)", c.asked)
	}
}

func TestTheNewAnswerReplacesTheOldAndIsRememberedWhoseWhenIsNow(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s, now := clockedChoiceServer(t, c)
	do(t, s, post("/api/conversation/choice/options", `{}`))
	*now = now.Add(11 * time.Minute)
	newer := claudeOptions
	newer.Current = "Sonnet 5.5 (default)"
	newer.Models = append(append([]string{}, claudeOptions.Models...), "fable")
	c.mu.Lock()
	c.options = newer
	c.mu.Unlock()
	do(t, s, post("/api/conversation/choice/options", `{}`))
	got := readChoice(t, s)
	if got.Options == nil || got.Options.Current != "Sonnet 5.5 (default)" || len(got.Options.Models) != 4 {
		t.Errorf("the page isn't given the new lists: %+v", got.Options)
	}
	if got.OptionsAt != now.UnixMilli() || !got.OptionsFresh {
		t.Errorf("at %d (want %d), fresh %v", got.OptionsAt, now.UnixMilli(), got.OptionsFresh)
	}
	if len(c.remembered) != 2 {
		t.Errorf("remembered %d times, want 2", len(c.remembered))
	}
}

func TestListsOlderThanTenMinutesAreNotCalledFreshAndEffortsAreOnlyCheckedForShape(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s, now := clockedChoiceServer(t, c)
	do(t, s, post("/api/conversation/choice/options", `{}`))
	if got := readChoice(t, s); !got.OptionsFresh {
		t.Fatal("not fresh when just asked")
	}
	if rec := do(t, s, post("/api/conversation/choice", `{"effort":"xhigh"}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("a fresh list should hold the effort to it: %d", rec.Code)
	}
	*now = now.Add(10 * time.Minute)
	if got := readChoice(t, s); got.OptionsFresh {
		t.Error("still called fresh after ten minutes")
	}
	if rec := do(t, s, post("/api/conversation/choice", `{"effort":"xhigh"}`)); rec.Code != http.StatusOK {
		t.Errorf("an old list holds an effort to its shape only: %d %s", rec.Code, rec.Body)
	}
}

func TestAReaskThatFailsKeepsTheOldListsAndIsLoggedAndTriedAgainNextTime(t *testing.T) {
	c := &choosing{options: claudeOptions}
	s, now := clockedChoiceServer(t, c)
	var said []string
	s.cfg.Log = func(line string) { said = append(said, line) }
	do(t, s, post("/api/conversation/choice/options", `{}`))
	*now = now.Add(11 * time.Minute)
	c.mu.Lock()
	c.optsErr = errors.New("sandbox is stopped")
	c.mu.Unlock()
	if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusBadGateway {
		t.Fatalf("%d", rec.Code)
	}
	got := readChoice(t, s)
	if got.Options == nil || len(got.Options.Models) != 3 {
		t.Errorf("the old lists were lost: %+v", got.Options)
	}
	if len(said) != 1 || !strings.Contains(said[0], "sandbox is stopped") {
		t.Errorf("said %q, want one line with why", said)
	}
	c.mu.Lock()
	c.optsErr = nil
	c.mu.Unlock()
	if rec := do(t, s, post("/api/conversation/choice/options", `{}`)); rec.Code != http.StatusOK || c.asked != 3 {
		t.Errorf("the next try: %d, asked %d times", rec.Code, c.asked)
	}
}

// The lists as claude gives them when Sonnet is the default.
var sonnetDefault = Options{
	Models: []string{"opus", "sonnet", "haiku"}, OtherModels: []string{"best"}, Efforts: []string{"low", "high"},
	Names:   map[string]string{"opus": "Opus 5.5", "sonnet": "Sonnet 5.5", "haiku": "Haiku 5.5", "best": "Fable 5.1"},
	Current: "Sonnet 5.5 (default)",
}

func adopt(t *testing.T, s *Server) (int, choiceState) {
	t.Helper()
	rec := do(t, s, post("/api/conversation/choice/adopt", `{}`))
	var got choiceState
	json.Unmarshal(rec.Body.Bytes(), &got)
	return rec.Code, got
}

func TestTheListsSayWhichModelIsTheDefault(t *testing.T) {
	got := readChoice(t, choiceServer(t, &choosing{cached: &sonnetDefault, cachedAt: time.Now()}))
	if got.Options == nil || got.Options.DefaultAlias != "sonnet" {
		t.Errorf("default alias = %+v", got.Options)
	}
}

func TestTheModelInUseIsRecordedWhenNothingIsChosenSoALaterChangeOfDefaultCantMoveIt(t *testing.T) {
	c := &choosing{cached: &sonnetDefault, cachedAt: time.Now()}
	s := choiceServer(t, c)
	code, got := adopt(t, s)
	if code != http.StatusOK || got.Model != "sonnet" {
		t.Fatalf("%d, %+v", code, got)
	}
	if len(c.setCalls) != 1 || c.setCalls[0] != [2]string{"sonnet", ""} {
		t.Errorf("kept %v, want sonnet", c.setCalls)
	}
	if got.Adopted != "sonnet" {
		t.Errorf("adopted = %q", got.Adopted)
	}
}

func TestTheModelThatLastRanIsWhatIsRecordedNotTheDefaultWhenTheyDiffer(t *testing.T) {
	c := &choosing{cached: &sonnetDefault, cachedAt: time.Now()}
	s := choiceServer(t, c)
	s.convo.line([]byte(`{"type":"system","subtype":"init","model":"claude-opus-5-5","session_id":"c403b0d5-2ae0-42d6-8d1f-8590da103c98"}`))
	if _, got := adopt(t, s); got.Model != "opus" {
		t.Errorf("recorded %q, want opus: what the last turn ran, and not what claude's default has since become", got.Model)
	}
}

func TestAModelThatRanWhichNoListedOneIsFallsBackToTheDefault(t *testing.T) {
	c := &choosing{cached: &sonnetDefault, cachedAt: time.Now()}
	s := choiceServer(t, c)
	s.convo.line([]byte(`{"type":"system","subtype":"init","model":"claude-mystery-1","session_id":"c403b0d5-2ae0-42d6-8d1f-8590da103c98"}`))
	if _, got := adopt(t, s); got.Model != "sonnet" {
		t.Errorf("recorded %q", got.Model)
	}
}

func TestTheProfilesOwnEffortIsKeptWhenTheModelIsRecorded(t *testing.T) {
	c := &choosing{cached: &sonnetDefault, cachedAt: time.Now(), effort: "high"}
	adopt(t, choiceServer(t, c))
	if len(c.setCalls) != 1 || c.setCalls[0] != [2]string{"sonnet", "high"} {
		t.Errorf("kept %v", c.setCalls)
	}
}

func TestNothingIsRecordedWhenAModelIsAlreadyChosenOrInherited(t *testing.T) {
	for name, c := range map[string]*choosing{
		"chosen":    {cached: &sonnetDefault, cachedAt: time.Now(), model: "opus"},
		"inherited": {cached: &sonnetDefault, cachedAt: time.Now(), globalModel: "haiku"},
	} {
		s := choiceServer(t, c)
		code, got := adopt(t, s)
		if code != http.StatusOK || len(c.setCalls) != 0 || got.Adopted != "" {
			t.Errorf("%s: %d, kept %v, adopted %q", name, code, c.setCalls, got.Adopted)
		}
	}
}

func TestNothingIsRecordedWhenItCantBeToldWhichModelItIs(t *testing.T) {
	noNames := sonnetDefault
	noNames.Names = nil
	for name, c := range map[string]*choosing{
		"no lists known":         {},
		"no names for the lists": {cached: &noNames, cachedAt: time.Now()},
	} {
		code, got := adopt(t, choiceServer(t, c))
		if code != http.StatusOK || len(c.setCalls) != 0 || got.Adopted != "" {
			t.Errorf("%s: %d, kept %v, adopted %q", name, code, c.setCalls, got.Adopted)
		}
	}
}

func TestTheModelIsRecordedOnceNotAtEveryAsk(t *testing.T) {
	c := &choosing{cached: &sonnetDefault, cachedAt: time.Now()}
	s := choiceServer(t, c)
	adopt(t, s)
	adopt(t, s)
	adopt(t, s)
	if len(c.setCalls) != 1 {
		t.Errorf("kept %d times, want once: %v", len(c.setCalls), c.setCalls)
	}
}

func TestAFailureToRecordTheModelIsSaidAndLogged(t *testing.T) {
	c := &choosing{cached: &sonnetDefault, cachedAt: time.Now(), setErr: errors.New("profile 'p' isn't there")}
	s := choiceServer(t, c)
	var said []string
	s.cfg.Log = func(line string) { said = append(said, line) }
	code, _ := adopt(t, s)
	if code != http.StatusInternalServerError || len(said) != 1 || !strings.Contains(said[0], "profile 'p'") {
		t.Errorf("%d, said %q", code, said)
	}
}

func TestListsAskedForNowAreWhatTheModelIsRecordedFromToo(t *testing.T) {
	c := &choosing{options: sonnetDefault}
	s := choiceServer(t, c)
	do(t, s, post("/api/conversation/choice/options", `{}`))
	if _, got := adopt(t, s); got.Model != "sonnet" {
		t.Errorf("recorded %q", got.Model)
	}
}
