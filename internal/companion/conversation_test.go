package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
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
	// linger, when set, keeps a turn going after it has answered, as
	// background work the agent waits on does, until it is closed (or ctx).
	linger chan struct{}
	err    error
}

func (f *fakeAgent) converse(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
	delivered()
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	f.sessions = append(f.sessions, session)
	release, linger := f.release, f.linger
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
	if linger != nil {
		select {
		case <-linger:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
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
	Events  []event `json:"events"`
	Last    int     `json:"last"`
	Running bool    `json:"running"`
	// Background is the agent having answered, but still at work.
	Background bool                   `json:"background"`
	Reset      bool                   `json:"reset"`
	Receipt    map[string]interface{} `json:"receipt"`
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
		if p := poll(t, s, 0); !p.Running && !p.Background {
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

func TestEveryEventSaysWhenItHappened(t *testing.T) {
	s := converseServer(&fakeAgent{})
	before := time.Now().UnixMilli()
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	p := idle(t, s)
	after := time.Now().UnixMilli()
	for _, e := range p.Events {
		if e.At < before || e.At > after {
			t.Errorf("%s event at %d, want between %d and %d", e.Kind, e.At, before, after)
		}
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

// historyServer is converseServer keeping its conversation in file, the way
// Start sets one up.
func historyServer(t *testing.T, f *fakeAgent, file string) *Server {
	t.Helper()
	s := converseServer(f)
	if err := s.convo.load(file); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTheConversationOutlivesTheServer(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.conversation.json")
	f := &fakeAgent{}
	first := historyServer(t, f, file)
	send(t, first, "/api/conversation/send", `{"prompt":"one"}`)
	was := idle(t, first)
	first.Close()

	second := historyServer(t, f, file)
	// A page that was showing the conversation carries on, without being
	// told to begin again.
	if p := poll(t, second, was.Last); p.Reset || len(p.Events) != 0 || p.Last != was.Last {
		t.Errorf("a page that had it all: %+v", p)
	}
	if got := kinds(poll(t, second, 0)); got != "user,agent,agent,done" {
		t.Errorf("a page that had none: %s", got)
	}
	send(t, second, "/api/conversation/send", `{"prompt":"two"}`)
	idle(t, second)
	if f.sessions[1] != "sess-1" {
		t.Errorf("the next turn resumed %q", f.sessions[1])
	}
}

func TestATurnCutShortByVibeStoppingIsSaidToHaveBeen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.conversation.json")
	f := &fakeAgent{release: make(chan struct{})}
	first := historyServer(t, f, file)
	send(t, first, "/api/conversation/send", `{"prompt":"one"}`)
	first.Close() // the turn is still running

	second := historyServer(t, &fakeAgent{}, file)
	p := poll(t, second, 0)
	if p.Running || p.Events[0].Text != "one" || p.Events[len(p.Events)-1].Kind != "error" {
		t.Errorf("after the restart: running=%v %s", p.Running, kinds(p))
	}
	// The first vibe's turn ends, and saves, before the test does.
	close(f.release)
	idle(t, first)
}

func TestBeginningAnewIsKept(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.conversation.json")
	first := historyServer(t, &fakeAgent{}, file)
	send(t, first, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, first)
	send(t, first, "/api/conversation/new", `{}`)

	second := historyServer(t, &fakeAgent{}, file)
	if got := kinds(poll(t, second, 0)); got != "reset" || second.convo.session != "" {
		t.Errorf("after the restart: %s, session %q", got, second.convo.session)
	}
}

// fakeReviews is the sandbox's last diffity link, as a turn leaves it.
type fakeReviews struct {
	mu   sync.Mutex
	link string
}

func (r *fakeReviews) set(link string) { r.mu.Lock(); r.link = link; r.mu.Unlock() }
func (r *fakeReviews) read() string    { r.mu.Lock(); defer r.mu.Unlock(); return r.link }

func reviewServer(f *fakeAgent, r *fakeReviews, file string) *Server {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, Converse: f.converse, LatestReview: r.read, History: file})
	if file != "" {
		s.convo.load(file)
	}
	return s
}

func reviews(p pollResult) []string {
	var links []string
	for _, e := range p.Events {
		if e.Kind == "review" {
			links = append(links, e.Text)
		}
	}
	return links
}

func TestANewReviewIsAnnouncedOnceWhenTheTurnEnds(t *testing.T) {
	r := &fakeReviews{link: "http://localhost:5396/diff"}
	s := reviewServer(&fakeAgent{}, r, "")
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	if got := reviews(idle(t, s)); len(got) != 0 {
		t.Errorf("the link the sandbox already had was announced: %v", got)
	}

	r.set("http://localhost:5396/diff?ref=master")
	send(t, s, "/api/conversation/send", `{"prompt":"review against master"}`)
	p := idle(t, s)
	if got := reviews(p); len(got) != 1 || got[0] != "http://localhost:5396/diff?ref=master" {
		t.Errorf("reviews = %v", got)
	}
	if got := kinds(p); !strings.HasSuffix(got, "agent,agent,review,done") {
		t.Errorf("the review isn't announced as the turn ends: %s", got)
	}

	send(t, s, "/api/conversation/send", `{"prompt":"resolve them"}`)
	if got := reviews(idle(t, s)); len(got) != 1 {
		t.Errorf("the same link was announced again: %v", got)
	}
}

func TestOnlyLocalReviewLinksAreAnnounced(t *testing.T) {
	r := &fakeReviews{}
	s := reviewServer(&fakeAgent{}, r, "")
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	for _, link := range []string{"https://example.com/diff", "http://evil.example/diff", "javascript:alert(1)", "http://user@localhost:5396/diff"} {
		r.set(link)
		send(t, s, "/api/conversation/send", `{"prompt":"again"}`)
		if got := reviews(idle(t, s)); len(got) != 0 {
			t.Errorf("%q was announced: %v", link, got)
		}
	}
}

func TestAnAnnouncedReviewIsKnownAfterARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.conversation.json")
	r := &fakeReviews{}
	f := &fakeAgent{}
	first := reviewServer(f, r, file)
	send(t, first, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, first)
	r.set("http://127.0.0.1:5396/diff")
	send(t, first, "/api/conversation/send", `{"prompt":"review"}`)
	idle(t, first)
	first.Close()

	// A review made while vibe was down is news to the page, which only
	// knows it if what it was last told survived the restart: otherwise the
	// new link would be taken for what was already there.
	r.set("http://127.0.0.1:5396/diff?ref=master")
	second := reviewServer(f, r, file)
	send(t, second, "/api/conversation/send", `{"prompt":"three"}`)
	got := reviews(idle(t, second))
	if len(got) != 2 || got[1] != "http://127.0.0.1:5396/diff?ref=master" {
		t.Errorf("reviews after a restart = %v, want the one from before and the new one", got)
	}
	send(t, second, "/api/conversation/send", `{"prompt":"four"}`)
	if got := reviews(idle(t, second)); len(got) != 2 {
		t.Errorf("a review was announced again after a restart: %v", got)
	}
}

// answered waits for the agent to have answered, though it may go on.
func answered(t *testing.T, s *Server) pollResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := poll(t, s, 0); !p.Running {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the agent never answered")
	return pollResult{}
}

func TestAnAnsweredTurnLetsThePageGoOnWhileTheAgentWorksInTheBackground(t *testing.T) {
	f := &fakeAgent{linger: make(chan struct{})}
	defer close(f.linger)
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"review it"}`)
	if p := answered(t, s); !p.Background {
		t.Errorf("an answered turn still at work isn't said to be: %+v", p)
	}

	// The next prompt ends what was left running, without a "stopped".
	f.mu.Lock()
	f.linger = nil
	f.mu.Unlock()
	if code, out := send(t, s, "/api/conversation/send", `{"prompt":"next"}`); code != http.StatusAccepted {
		t.Fatalf("send while the agent worked in the background: %d %v", code, out)
	}
	p := idle(t, s)
	if got := kinds(p); got != "user,agent,agent,done,user,agent,agent,done" {
		t.Errorf("events = %s", got)
	}
	for _, e := range p.Events {
		if e.Kind == "done" && e.Text != "" {
			t.Errorf("a superseded turn was said to be %q", e.Text)
		}
	}
}

func TestAPromptWaitsWhileTheAgentIsStillAnswering(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	if code, _ := send(t, s, "/api/conversation/send", `{"prompt":"two"}`); code != http.StatusConflict {
		t.Errorf("a prompt mid-answer: %d, want 409", code)
	}
	close(f.release)
	idle(t, s)
}

func TestTheAgentAtWorkAgainIsAnsweringAgain(t *testing.T) {
	s := converseServer(&fakeAgent{})
	c := &s.convo
	c.begin("go")
	c.line([]byte(`{"type":"result"}`))
	if _, _, running, background := c.since(0); running || !background {
		t.Errorf("after a result: running %v, background %v", running, background)
	}
	c.line([]byte(`{"type":"assistant"}`))
	if _, _, running, _ := c.since(0); !running {
		t.Error("the agent woken again by its background work isn't answering")
	}
	c.finish(nil, false)
}

func pollCommands(t *testing.T, s *Server, known int) (commands []string, seq int, sent bool) {
	t.Helper()
	rec := do(t, s, req{path: "/api/conversation?after=0&commands=" + itoa(known), headers: authed(nil)})
	var body struct {
		Commands    *[]string `json:"commands"`
		CommandsSeq int       `json:"commandsSeq"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Commands == nil {
		return nil, 0, false
	}
	return *body.Commands, body.CommandsSeq, true
}

func TestThePageLearnsTheSlashCommandsTheAgentHas(t *testing.T) {
	s := converseServer(&fakeAgent{})
	c := &s.convo
	if commands, _, sent := pollCommands(t, s, -1); !sent || len(commands) != 0 {
		t.Errorf("before the agent has started: %v, sent %v", commands, sent)
	}
	c.line([]byte(`{"type":"system","subtype":"init","slash_commands":["diffity-review","compact","/plugin:skill","compact","bad name","<script>"]}`))
	commands, seq, sent := pollCommands(t, s, -1)
	if want := []string{"compact", "diffity-review", "plugin:skill"}; !sent || !slices.Equal(commands, want) {
		t.Errorf("commands = %v (sent %v), want %v", commands, sent, want)
	}
	if _, _, sent := pollCommands(t, s, seq); sent {
		t.Error("a page with the latest was sent them again")
	}
	// The same list again isn't news; a different one is.
	c.line([]byte(`{"type":"system","subtype":"init","slash_commands":["compact","diffity-review","plugin:skill"]}`))
	if _, _, sent := pollCommands(t, s, seq); sent {
		t.Error("an unchanged list was sent again")
	}
	c.line([]byte(`{"type":"system","subtype":"init","slash_commands":["compact"]}`))
	if commands, _, sent := pollCommands(t, s, seq); !sent || !slices.Equal(commands, []string{"compact"}) {
		t.Errorf("after a change: %v, sent %v", commands, sent)
	}
}

func TestTheSlashCommandsOutliveTheServer(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.conversation.json")
	first := historyServer(t, &fakeAgent{}, file)
	first.convo.begin("go")
	first.convo.line([]byte(`{"type":"system","subtype":"init","slash_commands":["diffity-review"]}`))
	first.convo.finish(nil, false)
	first.Close()

	second := historyServer(t, &fakeAgent{}, file)
	if commands, _, sent := pollCommands(t, second, -1); !sent || !slices.Equal(commands, []string{"diffity-review"}) {
		t.Errorf("after a restart: %v, sent %v", commands, sent)
	}
}

func quietOf(t *testing.T, s *Server) int {
	t.Helper()
	rec := do(t, s, req{path: "/api/conversation?after=0", headers: authed(nil)})
	var body struct {
		Quiet int `json:"quietMinutes"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Quiet
}

func TestALongQuietTurnIsSaidToBeQuietButNotStopped(t *testing.T) {
	clock := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s := converseServer(&fakeAgent{})
	c := &s.convo
	c.now = func() time.Time { return clock }
	c.begin("long job")

	clock = clock.Add(59 * time.Minute)
	if q := quietOf(t, s); q != 0 {
		t.Errorf("after 59 quiet minutes: %d", q)
	}
	clock = clock.Add(2 * time.Minute)
	if q := quietOf(t, s); q != 61 {
		t.Errorf("after 61 quiet minutes: %d", q)
	}
	if !c.running {
		t.Error("the turn was stopped")
	}
	// Anything the agent says ends the quiet.
	c.line([]byte(`{"type":"assistant"}`))
	if q := quietOf(t, s); q != 0 {
		t.Errorf("after the agent spoke: %d", q)
	}
	// Answered, with work left in the background: not the agent being quiet.
	c.line([]byte(`{"type":"result"}`))
	clock = clock.Add(3 * time.Hour)
	if q := quietOf(t, s); q != 0 {
		t.Errorf("answered, three hours on: %d", q)
	}
	c.finish(nil, false)
	if q := quietOf(t, s); q != 0 {
		t.Errorf("no turn at all: %d", q)
	}
}
