package companion

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakePower is the vibe side of the power menu: what it would let be done to each sandbox, and a record of
// what it was asked to do.
type fakePower struct {
	mu      sync.Mutex
	checks  map[string]PowerCheck
	started []string
	asked   []string
	err     error
	log     string
	running bool
}

func (f *fakePower) Check(name string) PowerCheck {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name)
	if c, ok := f.checks[name]; ok {
		return c
	}
	return PowerCheck{Sandbox: name, Error: "no such sandbox"}
}

func (f *fakePower) Do(name, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, name+" "+action)
	return f.err
}

func (f *fakePower) Log(name string, from int64) (string, int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name)
	if from > int64(len(f.log)) {
		from = 0
	}
	return f.log[from:], int64(len(f.log)), f.running
}

func idlePower() *fakePower {
	ok := PowerAction{OK: true}
	return &fakePower{checks: map[string]PowerCheck{
		"box":   {Sandbox: "box", Folder: "~/work/box", Actions: map[string]PowerAction{"stop": ok, "restart": ok}},
		"other": {Sandbox: "other", Folder: "~/work/other", Actions: map[string]PowerAction{"stop": ok, "restart": ok}},
		"busy": {Sandbox: "busy", Folder: "~/work/busy", Actions: map[string]PowerAction{
			"stop":    {OK: true, Warns: []string{"the agent is part-way through a turn"}},
			"restart": {OK: false, Why: "a rebuild is under way"},
		}},
	}}
}

func powerServer(f *fakePower) *Server {
	return New(Config{Sandbox: "box", Token: token, Port: testPort, Power: f})
}

func TestWithoutPowerThereIsNothingToStopOrRestart(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	for _, r := range []req{{path: "/api/power", headers: authed(nil)}, post("/api/power", `{"action":"stop"}`), {path: "/api/power/log", headers: authed(nil)}} {
		if code := do(t, s, r).Code; code != http.StatusNotFound {
			t.Errorf("%s %s: %d", r.method, r.path, code)
		}
	}
}

func TestThePowerMenuIsAskedWhatCanBeDoneToThisSandboxByDefaultAndToAnotherByName(t *testing.T) {
	s := powerServer(idlePower())
	for path, want := range map[string]string{"/api/power": "box", "/api/power?sandbox=other": "other"} {
		rec := do(t, s, req{path: path, headers: authed(nil)})
		var got PowerCheck
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if got.Sandbox != want || !got.Actions["stop"].OK {
			t.Errorf("%s: %+v", path, got)
		}
	}
}

func TestAnActionIsStartedOnTheNamedSandboxAndNothingElseIsDone(t *testing.T) {
	f := idlePower()
	s := powerServer(f)
	rec := do(t, s, post("/api/power", `{"sandbox":"other","action":"restart"}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(f.started) != 1 || f.started[0] != "other restart" {
		t.Errorf("started %v", f.started)
	}
}

func TestAnActionWithNoSandboxNamedIsOnThisOne(t *testing.T) {
	f := idlePower()
	do(t, powerServer(f), post("/api/power", `{"action":"stop"}`))
	if len(f.started) != 1 || f.started[0] != "box stop" {
		t.Errorf("started %v", f.started)
	}
}

func TestAnActionTheSandboxCantTakeIsRefusedWithWhyAndNotStarted(t *testing.T) {
	f := idlePower()
	rec := do(t, powerServer(f), post("/api/power", `{"sandbox":"busy","action":"restart"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "a rebuild is under way") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if len(f.started) != 0 {
		t.Errorf("started %v", f.started)
	}
}

func TestAnActionThatInterruptsSomethingNeedsToBeConfirmed(t *testing.T) {
	f := idlePower()
	s := powerServer(f)
	rec := do(t, s, post("/api/power", `{"sandbox":"busy","action":"stop"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "part-way through a turn") {
		t.Errorf("unconfirmed: %d %s", rec.Code, rec.Body)
	}
	if len(f.started) != 0 {
		t.Fatalf("started unconfirmed: %v", f.started)
	}
	if rec := do(t, s, post("/api/power", `{"sandbox":"busy","action":"stop","confirmed":true,"warns":["the agent is part-way through a turn"]}`)); rec.Code != http.StatusAccepted {
		t.Errorf("confirmed: %d %s", rec.Code, rec.Body)
	}
	if len(f.started) != 1 {
		t.Errorf("started %v", f.started)
	}
}

func TestOnlyStopRestartAndStartOfASandboxVibeKnowsCanBeAskedFor(t *testing.T) {
	f := idlePower()
	s := powerServer(f)
	for _, body := range []string{`{"action":"format"}`, `{"sandbox":"nobody","action":"stop"}`, `{}`, `nonsense`} {
		if code := do(t, s, post("/api/power", body)).Code; code < 400 || code >= 500 {
			t.Errorf("%s: %d", body, code)
		}
	}
	if len(f.started) != 0 {
		t.Errorf("started %v", f.started)
	}
}

func TestAnActionThatCouldNotBeStartedSaysWhy(t *testing.T) {
	f := idlePower()
	f.err = http.ErrAbortHandler
	rec := do(t, powerServer(f), post("/api/power", `{"action":"stop"}`))
	if rec.Code < 500 || !strings.Contains(rec.Body.String(), "abort") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestWhatTheActionSaidIsGivenFromWhereThePageGotTo(t *testing.T) {
	f := idlePower()
	f.log = "stopping sandbox 'box'\nsandbox stopped\n"
	f.running = true
	s := powerServer(f)
	var got struct {
		Text    string `json:"text"`
		Next    int64  `json:"next"`
		Running bool   `json:"running"`
	}
	rec := do(t, s, req{path: "/api/power/log", headers: authed(nil)})
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Text != f.log || got.Next != int64(len(f.log)) || !got.Running {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	f.log += "bye\n"
	rec = do(t, s, req{path: "/api/power/log?from=" + "39", headers: authed(nil)})
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Text != "bye\n" {
		t.Errorf("from the end of what was seen: %s", rec.Body)
	}
}

func TestEveryRunOfTheServerHasItsOwnBootSoAPageCanTellItWasRestarted(t *testing.T) {
	boot := func() string {
		var st struct {
			Boot string `json:"boot"`
		}
		rec := do(t, powerServer(idlePower()), req{path: "/api/state", headers: authed(nil)})
		json.Unmarshal(rec.Body.Bytes(), &st)
		return st.Boot
	}
	a, b := boot(), boot()
	if a == "" || a == b {
		t.Errorf("boots %q and %q", a, b)
	}
}

func TestAConfirmationOnlyCoversWhatTheUserWasShown(t *testing.T) {
	// The page asked when the agent was idle; by the time of the click it is part-way through a turn.
	f := idlePower()
	s := powerServer(f)
	for name, body := range map[string]string{
		"nothing shown":            `{"sandbox":"busy","action":"stop","confirmed":true}`,
		"something else":           `{"sandbox":"busy","action":"stop","confirmed":true,"warns":["some other thing"]}`,
		"shown, but not confirmed": `{"sandbox":"busy","action":"stop","warns":["the agent is part-way through a turn"]}`,
	} {
		rec := do(t, s, post("/api/power", body))
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "part-way through a turn") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if len(f.started) != 0 {
		t.Errorf("started %v", f.started)
	}
}

func TestASandboxNameThatIsNotAFileNameIsRefusedAndNeverReachesThePowerDesk(t *testing.T) {
	f := idlePower()
	s := powerServer(f)
	for _, name := range []string{"../x", "../../etc/passwd", "a/b", `a\b`, "..", ".hidden", "a b", "x\x00y"} {
		q := url.QueryEscape(name)
		for _, r := range []req{
			{path: "/api/power?sandbox=" + q, headers: authed(nil)},
			{path: "/api/power/log?sandbox=" + q, headers: authed(nil)},
			post("/api/power", `{"sandbox":`+strconv.Quote(name)+`,"action":"stop"}`),
		} {
			if code := do(t, s, r).Code; code != http.StatusBadRequest {
				t.Errorf("%q: %s %s answered %d, want 400", name, r.method, r.path, code)
			}
		}
	}
	if len(f.asked) != 0 || len(f.started) != 0 {
		t.Errorf("the desk was asked %v and told to %v", f.asked, f.started)
	}
}

func TestTheLogSaysWhichRunOfTheServerAnswersItSoAPageCanTellARestartFromAFailure(t *testing.T) {
	s := powerServer(idlePower())
	var st struct {
		Boot string `json:"boot"`
	}
	json.Unmarshal(do(t, s, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &st)
	var lg struct {
		Boot string `json:"boot"`
	}
	json.Unmarshal(do(t, s, req{path: "/api/power/log", headers: authed(nil)}).Body.Bytes(), &lg)
	if st.Boot == "" || lg.Boot != st.Boot {
		t.Errorf("state boot %q, log boot %q", st.Boot, lg.Boot)
	}
}

func TestAnActionTheSandboxDoesNotOfferIsRefusedInPlainWords(t *testing.T) {
	f := idlePower()
	f.checks["bare"] = PowerCheck{Sandbox: "bare", Folder: "~/work/bare"}
	for action, want := range map[string]string{"stop": "can't stop a sandbox from here", "restart": "can't restart a sandbox from here", "start": "can't start a sandbox from here"} {
		rec := do(t, powerServer(f), post("/api/power", `{"sandbox":"bare","action":"`+action+`"}`))
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s, want %q", action, rec.Code, rec.Body, want)
		}
	}
	if len(f.started) != 0 {
		t.Errorf("started %v", f.started)
	}
}
