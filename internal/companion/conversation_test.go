package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAgent answers each prompt with an assistant line and a result, both
// carrying the session id it was given — or a fresh one.
type fakeAgent struct {
	mu       sync.Mutex
	prompts  []string
	sessions []string
	release  chan struct{} // when set, a turn waits for it (or for ctx)
	err      error
}

func (f *fakeAgent) converse(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
	delivered()
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	f.sessions = append(f.sessions, session)
	release := f.release
	f.mu.Unlock()
	id := session
	if id == "" {
		id = "sess-1"
	}
	onLine([]byte(`{"type":"assistant","session_id":"` + id + `","message":{"content":[{"type":"text","text":"echo: ` + prompt + `"}]}}`))
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	onLine([]byte(`{"type":"result","session_id":"` + id + `"}`))
	return f.err
}

func converseServer(f *fakeAgent) *Server {
	return New(Config{Sandbox: "box", Token: token, Port: testPort, Converse: f.converse})
}

func send(t *testing.T, s *Server, path, body string) (code int, out map[string]interface{}) {
	t.Helper()
	rec := do(t, s, post(path, body))
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

type pollResult struct {
	Events  []event                `json:"events"`
	Last    int                    `json:"last"`
	Running bool                   `json:"running"`
	Reset   bool                   `json:"reset"`
	Receipt map[string]interface{} `json:"receipt"`
}

func poll(t *testing.T, s *Server, after int) (p pollResult) {
	t.Helper()
	rec := do(t, s, req{path: "/api/conversation?after=" + itoa(after), headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("poll: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// idle waits for the turn to end.
func idle(t *testing.T, s *Server) pollResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := poll(t, s, 0); !p.Running {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the turn never ended")
	return pollResult{}
}

func kinds(p pollResult) string {
	var k []string
	for _, e := range p.Events {
		k = append(k, e.Kind)
	}
	return strings.Join(k, ",")
}

func TestAPromptReachesTheAgentAndItsAnswerComesBack(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	if code, _ := send(t, s, "/api/conversation/send", `{"prompt":"hello"}`); code != http.StatusAccepted {
		t.Fatalf("send: %d", code)
	}
	p := idle(t, s)
	if got := kinds(p); got != "user,agent,agent,done" {
		t.Errorf("events = %s", got)
	}
	if p.Events[0].Text != "hello" {
		t.Errorf("the user's event = %+v", p.Events[0])
	}
	if !strings.Contains(string(p.Events[1].Agent), "echo: hello") {
		t.Errorf("the agent's event = %s", p.Events[1].Agent)
	}
}

func TestTheNextPromptResumesTheSessionTheAgentReported(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	send(t, s, "/api/conversation/send", `{"prompt":"two"}`)
	idle(t, s)
	if want := []string{"", "sess-1"}; f.sessions[0] != want[0] || f.sessions[1] != want[1] {
		t.Errorf("sessions = %q, want %q", f.sessions, want)
	}
}

func TestASessionThatIsNotAnIdIsNeverResumed(t *testing.T) {
	var c conversation
	c.line([]byte(`{"type":"system","session_id":"--dangerously-skip-permissions x"}`))
	if c.session != "" {
		t.Errorf("session = %q", c.session)
	}
	c.line([]byte(`{"type":"system","session_id":"abc-123_DEF"}`))
	if c.session != "abc-123_DEF" {
		t.Errorf("session = %q", c.session)
	}
}

func TestOnlyOneTurnRunsAtATime(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	code, body := send(t, s, "/api/conversation/send", `{"prompt":"impatient"}`)
	if code != http.StatusConflict {
		t.Errorf("a second send while running: %d %v", code, body)
	}
	close(f.release)
	idle(t, s)
	if len(f.prompts) != 1 {
		t.Errorf("the agent was asked %q", f.prompts)
	}
}

func TestStoppingEndsTheTurnAndSaysSo(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	if _, body := send(t, s, "/api/conversation/stop", `{}`); body["stopped"] != true {
		t.Errorf("stop = %v", body)
	}
	p := idle(t, s)
	last := p.Events[len(p.Events)-1]
	if last.Kind != "done" || last.Text != "stopped" {
		t.Errorf("last event = %+v", last)
	}
	if _, body := send(t, s, "/api/conversation/stop", `{}`); body["stopped"] != false {
		t.Errorf("stopping nothing = %v", body)
	}
}

func TestAFailedTurnIsReportedAndTheNextOneCanRun(t *testing.T) {
	f := &fakeAgent{err: context.DeadlineExceeded}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"x"}`)
	p := idle(t, s)
	if last := p.Events[len(p.Events)-1]; last.Kind != "error" || last.Text == "" {
		t.Errorf("last event = %+v", last)
	}
	f.err = nil
	if code, _ := send(t, s, "/api/conversation/send", `{"prompt":"y"}`); code != http.StatusAccepted {
		t.Errorf("send after a failure: %d", code)
	}
	idle(t, s)
}

func TestBeginningAnewForgetsTheSessionAndTellsPages(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	seen := idle(t, s).Last
	if code, _ := send(t, s, "/api/conversation/new", `{}`); code != http.StatusOK {
		t.Fatalf("new: %d", code)
	}
	p := poll(t, s, seen)
	if kinds(p) != "reset" {
		t.Errorf("a page that had seen everything gets %q", kinds(p))
	}
	send(t, s, "/api/conversation/send", `{"prompt":"two"}`)
	idle(t, s)
	if f.sessions[1] != "" {
		t.Errorf("the new conversation resumed %q", f.sessions[1])
	}
}

func TestBeginningAnewWaitsForTheAgent(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	if code, _ := send(t, s, "/api/conversation/new", `{}`); code != http.StatusConflict {
		t.Errorf("new while running: %d", code)
	}
	close(f.release)
	idle(t, s)
}

func TestAPageAheadOfTheServerStartsAgain(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	p := poll(t, s, 999)
	if !p.Reset || len(p.Events) == 0 {
		t.Errorf("poll after 999 = %+v", p)
	}
}

func TestPollingOnlyReturnsWhatIsNew(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	all := idle(t, s)
	if p := poll(t, s, all.Last); len(p.Events) != 0 {
		t.Errorf("nothing new, got %s", kinds(p))
	}
	if p := poll(t, s, 1); kinds(p) != "agent,agent,done" {
		t.Errorf("after the first = %s", kinds(p))
	}
}

func TestPromptsAreChecked(t *testing.T) {
	s := converseServer(&fakeAgent{})
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"empty":       {`{"prompt":"   "}`, http.StatusBadRequest},
		"not json":    {`nope`, http.StatusBadRequest},
		"too long":    {`{"prompt":"` + strings.Repeat("a", maxPrompt) + `"}`, http.StatusRequestEntityTooLarge},
		"long enough": {`{"prompt":"` + strings.Repeat("a", maxPrompt/2) + `"}`, http.StatusAccepted},
	} {
		if code, _ := send(t, s, "/api/conversation/send", tc.body); code != tc.want {
			t.Errorf("%s: %d, want %d", name, code, tc.want)
		}
		idle(t, s)
	}
}

func TestTheConversationIsProtectedLikeEveryChange(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	body := `{"prompt":"x"}`
	for name, r := range map[string]req{
		"no token":     {method: "POST", path: "/api/conversation/send", body: body, headers: map[string]string{"Content-Type": "application/json"}},
		"not json":     {method: "POST", path: "/api/conversation/send", body: body, headers: authed(nil)},
		"cross-origin": {method: "POST", path: "/api/conversation/send", body: body, headers: authed(map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example"})},
		"cross-site":   {method: "POST", path: "/api/conversation/send", body: body, headers: authed(map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"})},
	} {
		if rec := do(t, s, r); rec.Code < 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := do(t, s, req{path: "/api/conversation"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("reading without a token: %d", rec.Code)
	}
	if len(f.prompts) != 0 {
		t.Errorf("the agent heard %q", f.prompts)
	}
}

func TestWithoutAConverserThereIsNoConversation(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if code, _ := send(t, s, "/api/conversation/send", `{"prompt":"x"}`); code != http.StatusNotFound {
		t.Errorf("send = %d", code)
	}
	var st map[string]interface{}
	json.Unmarshal(do(t, s, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &st)
	if st["conversation"] != false {
		t.Errorf("state.conversation = %v", st["conversation"])
	}
	with := converseServer(&fakeAgent{})
	json.Unmarshal(do(t, with, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &st)
	if st["conversation"] != true {
		t.Errorf("state.conversation = %v", st["conversation"])
	}
}

func TestOutputThatIsNotJSONOrIsHugeIsReplacedNotShown(t *testing.T) {
	var c conversation
	c.line([]byte("plain text, not json"))
	c.line([]byte(`{"type":"user","x":"` + strings.Repeat("a", maxLine) + `"}`))
	for _, e := range c.events {
		if string(e.Agent) != `{"type":"truncated"}` {
			t.Errorf("kept %.60s", e.Agent)
		}
	}
}

func TestALongConversationKeepsOnlyTheLastEvents(t *testing.T) {
	var c conversation
	for i := 0; i < maxEvents+50; i++ {
		c.line([]byte(`{"type":"assistant"}`))
	}
	if len(c.events) != maxEvents || c.events[len(c.events)-1].Seq != maxEvents+50 {
		t.Errorf("kept %d events, last seq %d", len(c.events), c.events[len(c.events)-1].Seq)
	}
}

func TestAPromptsReceiptAdvancesFromSentToDeliveredToSeen(t *testing.T) {
	var c conversation
	c.begin("hello")
	if got := c.latestReceipt()["state"]; got != receiptSent {
		t.Errorf("after begin: %v", got)
	}
	c.receipt(receiptDelivered)
	if got := c.latestReceipt()["state"]; got != receiptDelivered {
		t.Errorf("after delivery: %v", got)
	}
	c.line([]byte(`{"type":"system","subtype":"init"}`))
	if got := c.latestReceipt()["state"]; got != receiptSeen {
		t.Errorf("after the agent's first line: %v", got)
	}
	// Never backwards, whatever order the news arrives in.
	c.receipt(receiptDelivered)
	if got := c.latestReceipt()["state"]; got != receiptSeen {
		t.Errorf("went back to %v", got)
	}
}

func TestTheReceiptIsInThePollAndOnTheUserEvent(t *testing.T) {
	s := converseServer(&fakeAgent{})
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	p := idle(t, s)
	if p.Events[0].Receipt != receiptSeen {
		t.Errorf("the user event's receipt = %q", p.Events[0].Receipt)
	}
	if p.Receipt["state"] != receiptSeen || int(p.Receipt["seq"].(float64)) != p.Events[0].Seq {
		t.Errorf("the poll's receipt = %v", p.Receipt)
	}
}
