package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAgent stands in for the agent: each start is a process (fakeProc) that
// answers each prompt with an assistant line and a result, saying of each
// prompt it was queued, started and completed, as claude does.
type fakeAgent struct {
	mu       sync.Mutex
	prompts  []string
	sessions []string // the session each start resumed
	starts   int
	// startGate, when set, holds each start until it is closed: an agent that takes a while to come up, as one through sbx does.
	startGate chan struct{}
	// crash, when closed, makes the processes end by themselves, as an agent that dies does.
	crash chan struct{}
	// ignoreEnd makes the processes carry on when their input is closed: an agent that will not end when asked.
	ignoreEnd bool
	// killedProcs is, for each process started, in order, whether it was killed.
	killedProcs []bool
	// interrupts, ends, kills count what was asked of the processes;
	// stoppedTasks are the background tasks stopped.
	interrupts, ends, kills int
	stoppedTasks            []string
	release                 chan struct{} // when set, a turn waits for it (or an interrupt) before its result
	slowStop                chan struct{} // when set, an interrupted turn ends only once it is closed
	// linger, when set, leaves a background task running after each answer,
	// until it is closed (or the task is stopped, or the process killed).
	linger   chan struct{}
	err      error // what a process fails with, before it answers
	startErr error // what a start fails with
	killErr  error // what Kill says
}

func (f *fakeAgent) start(ctx context.Context, session string, onLine func([]byte)) (Agent, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.mu.Lock()
	f.starts++
	f.sessions = append(f.sessions, session)
	gate := f.startGate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	p := &fakeProc{f: f, session: session, onLine: onLine, prompts: make(chan [2]string, 16), interrupt: make(chan struct{}, 4),
		stopTask: make(chan string, 4), end: make(chan struct{}), kill: make(chan struct{}), done: make(chan struct{})}
	f.mu.Lock()
	p.n = len(f.killedProcs)
	f.killedProcs = append(f.killedProcs, false)
	f.mu.Unlock()
	go p.run()
	return p, nil
}

func (f *fakeAgent) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.prompts...)
}

func (f *fakeAgent) sessionsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sessions...)
}

func (f *fakeAgent) count(n *int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *n
}

type fakeProc struct {
	f         *fakeAgent
	n         int // its place in f.killedProcs
	session   string
	onLine    func([]byte)
	prompts   chan [2]string
	interrupt chan struct{}
	stopTask  chan string
	end, kill chan struct{}
	endOnce   sync.Once
	killOnce  sync.Once
	done      chan struct{}
	err       error
}

func (p *fakeProc) say(line string) { p.onLine([]byte(line)) }

func (p *fakeProc) id() string {
	if p.session == "" {
		return "sess-1"
	}
	return p.session
}

func (p *fakeProc) run() {
	defer close(p.done)
	task := false
	end := p.end
	for {
		select {
		case <-p.kill:
			return
		case <-p.f.crash:
			p.err = errors.New("crashed")
			return
		case <-end:
			if p.f.ignoreEnd {
				end = nil
				continue
			}
			if !task {
				p.err = p.f.err
				return
			}
			// Input closed, a task still running: it waits for it, as claude does.
			select {
			case <-p.kill:
			case <-p.f.linger:
			}
			p.err = p.f.err
			return
		case id := <-p.stopTask:
			if task && id == "t1" {
				task = false
				p.say(`{"type":"system","subtype":"background_tasks_changed","tasks":[]}`)
			}
		case <-p.f.linger:
			if task {
				task = false
				p.say(`{"type":"system","subtype":"background_tasks_changed","tasks":[]}`)
			}
		case pr := <-p.prompts:
			text, id := pr[0], pr[1]
			p.say(`{"type":"command_lifecycle","command_uuid":"` + id + `","state":"queued"}`)
			p.say(`{"type":"command_lifecycle","command_uuid":"` + id + `","state":"started"}`)
			p.say(`{"type":"assistant","session_id":"` + p.id() + `","message":{"content":[{"type":"text","text":"echo: ` + text + `"}]}}`)
			if p.f.err != nil {
				// It fails before it has answered.
				p.err = p.f.err
				return
			}
			stopped := false
			if p.f.release != nil {
				select {
				case <-p.f.release:
				case <-p.interrupt:
					stopped = true
					if p.f.slowStop != nil {
						select {
						case <-p.f.slowStop:
						case <-p.kill:
							return
						}
					}
				case <-p.kill:
					return
				}
			}
			if stopped {
				p.say(`{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":"` + p.id() + `"}`)
				p.say(`{"type":"command_lifecycle","command_uuid":"` + id + `","state":"cancelled"}`)
				continue
			}
			if p.f.linger != nil && !task {
				// The task starts during the turn, as claude reports it: before the result.
				select {
				case <-p.f.linger:
				default:
					task = true
					p.say(`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"t1","task_type":"local_bash","description":"Run the tests"}]}`)
				}
			}
			p.say(`{"type":"result","subtype":"success","num_turns":1,"result":"ok","session_id":"` + p.id() + `"}`)
			p.say(`{"type":"command_lifecycle","command_uuid":"` + id + `","state":"completed"}`)
		}
	}
}

func (p *fakeProc) Prompt(text, id string) error {
	p.f.mu.Lock()
	p.f.prompts = append(p.f.prompts, text)
	p.f.mu.Unlock()
	select {
	case <-p.done:
		return errors.New("the agent has ended")
	case p.prompts <- [2]string{text, id}:
		return nil
	}
}

func (p *fakeProc) Interrupt() error {
	p.f.mu.Lock()
	p.f.interrupts++
	p.f.mu.Unlock()
	select {
	case p.interrupt <- struct{}{}:
	default:
	}
	return nil
}

func (p *fakeProc) StopTask(id string) error {
	p.f.mu.Lock()
	p.f.stoppedTasks = append(p.f.stoppedTasks, id)
	p.f.mu.Unlock()
	p.stopTask <- id
	return nil
}

func (p *fakeProc) End() {
	p.f.mu.Lock()
	p.f.ends++
	p.f.mu.Unlock()
	p.endOnce.Do(func() { close(p.end) })
}

func (p *fakeProc) Kill() error {
	p.f.mu.Lock()
	p.f.kills++
	p.f.killedProcs[p.n] = true
	err := p.f.killErr
	p.f.mu.Unlock()
	p.killOnce.Do(func() { close(p.kill) })
	<-p.done
	return err
}

func (p *fakeProc) Done() <-chan struct{} { return p.done }

func (p *fakeProc) Err() error {
	<-p.done
	return p.err
}

func converseServer(f *fakeAgent) *Server {
	return New(Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.start})
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
	// StopState is "stopping" while a stop is under way.
	StopState string `json:"stopState"`
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

func TestAPromptAfterTheAgentHasEndedResumesTheSessionItReported(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	s.convo.idleAfter = 10 * time.Millisecond
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	waitFor(t, "the idle agent to end", func() bool { return f.count(&f.ends) == 1 })
	send(t, s, "/api/conversation/send", `{"prompt":"two"}`)
	idle(t, s)
	if got, want := f.sessionsSeen(), []string{"", "sess-1"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("sessions = %q, want %q", got, want)
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

func TestAPromptWhileTheAgentAnswersIsGivenToTheSameAgent(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	if code, body := send(t, s, "/api/conversation/send", `{"prompt":"and this"}`); code != http.StatusAccepted {
		t.Fatalf("a second send while the agent answers: %d %v", code, body)
	}
	waitFor(t, "the second prompt to reach the agent", func() bool { return len(f.asked()) == 2 })
	if got := f.asked(); got[1] != "and this" {
		t.Errorf("the agent was given %q", got)
	}
	if n := f.count(&f.starts); n != 1 {
		t.Errorf("%d agents were started: the prompt is for the one answering, as the console does", n)
	}
	close(f.release)
	idle(t, s)
}

func TestStoppingInterruptsTheTurnLeavingTheAgentAndSaysSo(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	waitFor(t, "the agent to be answering", func() bool { return len(f.asked()) == 1 })
	if _, body := send(t, s, "/api/conversation/stop", `{}`); body["stopped"] != true {
		t.Errorf("stop = %v", body)
	}
	p := idle(t, s)
	last := p.Events[len(p.Events)-1]
	if last.Kind != "done" || last.Text != "stopped" {
		t.Errorf("last event = %+v", last)
	}
	if f.count(&f.interrupts) != 1 || f.count(&f.kills) != 0 {
		t.Errorf("interrupts %d, kills %d: a stop interrupts the turn and leaves the agent", f.count(&f.interrupts), f.count(&f.kills))
	}
	if _, body := send(t, s, "/api/conversation/stop", `{}`); body["stopped"] != false {
		t.Errorf("stopping nothing = %v", body)
	}
	close(f.release)
	send(t, s, "/api/conversation/send", `{"prompt":"next"}`)
	idle(t, s)
	if n := f.count(&f.starts); n != 1 {
		t.Errorf("%d agents started: the stopped one answers the next prompt", n)
	}
}

// A stop before the agent has started (it takes a second or two through sbx) leaves nothing to interrupt: the agent being
// started is ended as soon as it is up, the prompt is said not to have reached it, and the next prompt starts another.
func TestStoppingWhileTheAgentStartsEndsItOnceItIsUpWithoutGivingItThePrompt(t *testing.T) {
	gate := make(chan struct{})
	f := &fakeAgent{startGate: gate}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	waitFor(t, "the agent to be starting", func() bool { return f.count(&f.starts) == 1 })
	if _, body := send(t, s, "/api/conversation/stop", `{}`); body["stopped"] != true {
		t.Errorf("stop = %v", body)
	}
	close(gate)
	p := idle(t, s)
	if f.count(&f.kills) != 1 || f.count(&f.interrupts) != 0 {
		t.Errorf("kills %d, interrupts %d: an agent stopped while it started is ended once it is up", f.count(&f.kills), f.count(&f.interrupts))
	}
	if len(f.asked()) != 0 {
		t.Errorf("the stopped prompt was given to the agent: %v", f.asked())
	}
	if p.Events[0].Receipt != receiptUndelivered {
		t.Errorf("the prompt's receipt = %q, want it said not to have reached the agent", p.Events[0].Receipt)
	}
	send(t, s, "/api/conversation/send", `{"prompt":"again"}`)
	idle(t, s)
	if n := f.count(&f.starts); n != 2 {
		t.Errorf("%d agents started: the next prompt starts another", n)
	}
	if got := f.asked(); len(got) != 1 || got[0] != "again" {
		t.Errorf("asked %v", got)
	}
}

// Interrupting a turn takes a moment, during which it is still under way: the
// page must be told a stop is under way, or Stop appears to do nothing.
func TestAStopIsSaidToBeUnderWayUntilTheTurnHasEnded(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{}), slowStop: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	waitFor(t, "the prompt to reach the agent", func() bool { return len(f.asked()) == 1 })
	send(t, s, "/api/conversation/stop", `{}`)
	if p := poll(t, s, 0); !p.Running || p.StopState != "stopping" {
		t.Errorf("while the agent is still being stopped: running %v, stopState %q", p.Running, p.StopState)
	}
	close(f.slowStop)
	if p := idle(t, s); p.StopState != "" {
		t.Errorf("stopState %q after the turn ended", p.StopState)
	}
	close(f.release)
}

func TestAnAgentThatWillNotStopCanBeEndedAtOnce(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{}), slowStop: make(chan struct{})}
	defer close(f.slowStop)
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"stuck"}`)
	waitFor(t, "the agent to say which session it is in", func() bool {
		for _, e := range poll(t, s, 0).Events {
			if e.Kind == "agent" {
				return true
			}
		}
		return false
	})
	send(t, s, "/api/conversation/stop", `{}`)
	if _, body := send(t, s, "/api/conversation/stop", `{"force":true}`); body["stopped"] != true {
		t.Fatalf("force = %v", body)
	}
	p := idle(t, s)
	if last := p.Events[len(p.Events)-1]; last.Kind != "done" || last.Text != "stopped" {
		t.Errorf("last event = %+v", last)
	}
	if f.count(&f.kills) != 1 {
		t.Errorf("kills = %d", f.count(&f.kills))
	}
	close(f.release)
	send(t, s, "/api/conversation/send", `{"prompt":"again"}`)
	idle(t, s)
	if f.count(&f.starts) != 2 || f.sessionsSeen()[1] != "sess-1" {
		t.Errorf("starts %d, sessions %q: the next prompt starts an agent resuming the conversation", f.count(&f.starts), f.sessionsSeen())
	}
}

func TestAForceStopThatCouldNotBeConfirmedIsReported(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{}), killErr: fmt.Errorf("%w: sandbox unreachable", ErrStopUnconfirmed)}
	defer close(f.release)
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	waitFor(t, "the prompt to reach the agent", func() bool { return len(f.asked()) == 1 })
	send(t, s, "/api/conversation/stop", `{"force":true}`)
	p := idle(t, s)
	found := false
	for _, e := range p.Events {
		found = found || (e.Kind == "error" && strings.Contains(e.Text, "sandbox unreachable"))
	}
	if !found {
		t.Errorf("events %s: the unconfirmed stop is not said", kinds(p))
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

// Lines can each be long, and the agent decides how many: what's kept is
// bounded in bytes as well as events, the oldest going first.
func TestALongConversationKeepsNoMoreThanItsBytes(t *testing.T) {
	var c conversation
	big := []byte(`{"type":"assistant","x":"` + strings.Repeat("a", maxLine-100) + `"}`)
	for i := 0; i < 2*maxKept/maxLine; i++ {
		c.line(big)
	}
	kept := 0
	for _, e := range c.events {
		kept += len(e.Text) + len(e.Agent)
	}
	if kept > maxKept {
		t.Errorf("kept %d bytes, past the most, %d", kept, maxKept)
	}
	if last := c.events[len(c.events)-1].Seq; last != c.seq {
		t.Errorf("the last event kept is %d, not the newest, %d", last, c.seq)
	}
}

func TestAPromptsReceiptFollowsWhatTheAgentSaysOfIt(t *testing.T) {
	var c conversation
	seq, _ := c.claim("hello", true)
	c.pending = map[string]int{"p-1": seq}
	state := func() interface{} { return c.latestReceipt()["state"] }
	if got := state(); got != receiptSent {
		t.Errorf("claimed: %v", got)
	}
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-1","state":"queued"}`))
	if got := state(); got != receiptDelivered {
		t.Errorf("queued by the agent: %v", got)
	}
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-1","state":"started"}`))
	if got := state(); got != receiptSeen {
		t.Errorf("started: %v", got)
	}
	// Never backwards, whatever order the news arrives in.
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-1","state":"queued"}`))
	if got := state(); got != receiptSeen {
		t.Errorf("went back to %v", got)
	}
	// A prompt a stop dropped before the agent took it in was never answered.
	other, _ := c.claim("and this", true)
	c.pending["p-2"] = other
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-2","state":"queued"}`))
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-2","state":"cancelled"}`))
	if got := state(); got != receiptUndelivered {
		t.Errorf("cancelled: %v", got)
	}
	for _, e := range c.events {
		if e.Kind == "agent" {
			t.Errorf("what the agent says of a prompt is shown: %s", e.Agent)
		}
	}
}

// A stop cancels the prompt the agent was answering: it was taken in and acted on, so it is not said to be undelivered.
func TestAPromptTheAgentHadTakenInKeepsItsReceiptWhenAStopCancelsIt(t *testing.T) {
	var c conversation
	seq, _ := c.claim("count", true)
	c.pending = map[string]int{"p-1": seq}
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-1","state":"queued"}`))
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-1","state":"started"}`))
	c.line([]byte(`{"type":"command_lifecycle","command_uuid":"p-1","state":"cancelled"}`))
	if got := c.latestReceipt()["state"]; got != receiptSeen {
		t.Errorf("a prompt the agent was answering, stopped: %v", got)
	}
	if len(c.pending) != 0 {
		t.Errorf("still waiting to hear of %v", c.pending)
	}
}

// An interrupted turn ends with a result the agent marks as an error: it is the user's stop, and the page is told so rather than
// showing a failure.
func TestTheResultOfAStoppedTurnIsMarkedAsTheStop(t *testing.T) {
	f := &fakeAgent{release: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	waitFor(t, "the prompt to reach the agent", func() bool { return len(f.asked()) == 1 })
	send(t, s, "/api/conversation/stop", `{}`)
	p := idle(t, s)
	var result *event
	for i := range p.Events {
		if e := &p.Events[i]; e.Kind == "agent" && strings.Contains(string(e.Agent), `"type":"result"`) {
			result = e
		}
	}
	if result == nil || result.Text != "stopped" {
		t.Fatalf("the stopped turn's result = %+v, want it marked stopped", result)
	}
	if p.Events[0].Receipt != receiptSeen {
		t.Errorf("the stopped prompt's receipt = %q: the agent had taken it in", p.Events[0].Receipt)
	}
	close(f.release)
	send(t, s, "/api/conversation/send", `{"prompt":"next"}`)
	p = idle(t, s)
	for _, e := range p.Events {
		if e.Kind == "agent" && strings.Contains(string(e.Agent), `"result":"ok"`) && e.Text != "" {
			t.Errorf("a turn that was not stopped is marked %q", e.Text)
		}
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

func TestBackgroundWorkKeepsTheAgentAndAPromptMeanwhileGoesToItLeavingTheWorkRunning(t *testing.T) {
	f := &fakeAgent{linger: make(chan struct{})}
	defer close(f.linger)
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"run the tests in the background"}`)
	if p := answered(t, s); !p.Background {
		t.Errorf("an agent with work in the background isn't said to have it: %+v", p)
	}
	if tasks := tasksOf(t, s); len(tasks) != 1 || tasks[0].Description != "Run the tests" {
		t.Errorf("tasks = %+v", tasks)
	}
	if code, out := send(t, s, "/api/conversation/send", `{"prompt":"meanwhile"}`); code != http.StatusAccepted {
		t.Fatalf("send while the agent worked in the background: %d %v", code, out)
	}
	answered(t, s)
	if f.count(&f.starts) != 1 || f.count(&f.kills) != 0 || len(f.stoppedTasks) != 0 {
		t.Errorf("starts %d, kills %d, tasks stopped %v: the prompt goes to the agent, and its work goes on", f.count(&f.starts), f.count(&f.kills), f.stoppedTasks)
	}
	p := poll(t, s, 0)
	if got := kinds(p); got != "user,agent,agent,done,user,agent,agent,done" {
		t.Errorf("events = %s", got)
	}
	if !p.Background {
		t.Error("the background work isn't said to go on")
	}
}

func tasksOf(t *testing.T, s *Server) []BackgroundTask {
	t.Helper()
	rec := do(t, s, req{path: "/api/conversation?after=0", headers: authed(nil)})
	var body struct {
		Tasks []BackgroundTask `json:"tasks"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Tasks
}

func TestStoppingTheBackgroundWorkLeavesTheAgent(t *testing.T) {
	f := &fakeAgent{linger: make(chan struct{})}
	defer close(f.linger)
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"run the tests in the background"}`)
	answered(t, s)
	if _, body := send(t, s, "/api/conversation/stop", `{"tasks":true}`); body["stopped"] != float64(1) {
		t.Fatalf("stop tasks = %v", body)
	}
	waitFor(t, "the background work to end", func() bool { return len(tasksOf(t, s)) == 0 })
	if f.count(&f.kills) != 0 || f.count(&f.starts) != 1 {
		t.Errorf("kills %d, starts %d: the agent is left", f.count(&f.kills), f.count(&f.starts))
	}
}

func TestAnAgentWithNothingToDoIsEndedAndTheNextPromptResumesTheConversation(t *testing.T) {
	f := &fakeAgent{}
	s := converseServer(f)
	s.convo.idleAfter = 20 * time.Millisecond
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	waitFor(t, "the idle agent to be ended", func() bool { return f.count(&f.ends) == 1 })
	send(t, s, "/api/conversation/send", `{"prompt":"two"}`)
	idle(t, s)
	if got := f.sessionsSeen(); len(got) != 2 || got[0] != "" || got[1] != "sess-1" {
		t.Errorf("sessions started = %q: the second agent resumes the conversation", got)
	}
}

// An idle agent that does not end when its input is closed is killed. But the kill is aimed by the conversation's pid file, which
// names whichever agent started last: once a newer one has started (whose start ended the old one, as a leftover), it is not
// made, or it would end the newer agent instead.
func TestAnIdleAgentThatWillNotEndIsKilled(t *testing.T) {
	f := &fakeAgent{ignoreEnd: true}
	s := converseServer(f)
	s.convo.idleAfter, s.convo.endAfter = 10*time.Millisecond, 30*time.Millisecond
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	waitFor(t, "the idle agent to be asked to end", func() bool { return f.count(&f.ends) == 1 })
	waitFor(t, "the agent that would not end to be killed", func() bool { return f.count(&f.kills) == 1 })
}

func TestAnIdleAgentThatWillNotEndIsNotKilledOnceANewerOneHasStarted(t *testing.T) {
	f := &fakeAgent{ignoreEnd: true}
	s := converseServer(f)
	s.convo.idleAfter, s.convo.endAfter = 10*time.Millisecond, 100*time.Millisecond
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	waitFor(t, "the idle agent to be asked to end", func() bool { return f.count(&f.ends) == 1 })
	send(t, s, "/api/conversation/send", `{"prompt":"two"}`)
	waitFor(t, "a second agent to start", func() bool { return f.count(&f.starts) == 2 })
	time.Sleep(200 * time.Millisecond) // past endAfter: the kill, if made, has been
	f.mu.Lock()
	first := f.killedProcs[0]
	f.mu.Unlock()
	if first {
		t.Error("the first agent was killed after a newer one started: the kill goes by the pid file, which names the newer one")
	}
}

func TestAnAgentWithBackgroundWorkIsNotEndedForBeingIdle(t *testing.T) {
	f := &fakeAgent{linger: make(chan struct{})}
	s := converseServer(f)
	s.convo.idleAfter = 20 * time.Millisecond
	send(t, s, "/api/conversation/send", `{"prompt":"run the tests in the background"}`)
	answered(t, s)
	time.Sleep(100 * time.Millisecond)
	if f.count(&f.ends) != 0 {
		t.Error("an agent with work in the background was ended")
	}
	close(f.linger)
	waitFor(t, "the agent to be ended once its work is done", func() bool { return f.count(&f.ends) == 1 })
}

func TestTheAgentCalledBackByItsBackgroundWorkIsAnsweringAgain(t *testing.T) {
	var c conversation
	agent, _ := (&fakeAgent{}).start(context.Background(), "", func([]byte) {})
	defer agent.Kill()
	c.agent = agent
	c.claim("go", true)
	c.line([]byte(`{"type":"result","num_turns":1,"result":"ok"}`))
	if _, _, running, _ := c.since(0); running {
		t.Error("still answering after its result")
	}
	c.line([]byte(`{"type":"system","subtype":"task_notification","status":"completed"}`))
	c.line([]byte(`{"type":"system","subtype":"init"}`))
	if _, _, running, _ := c.since(0); !running {
		t.Error("the agent called back by its background work isn't answering")
	}
	c.line([]byte(`{"type":"result","num_turns":1,"result":"it finished"}`))
	if _, _, running, _ := c.since(0); running {
		t.Error("still answering after the turn it began by itself")
	}
}

func TestAResumedAgentsEmptyFirstWordEndsNoTurn(t *testing.T) {
	var c conversation
	c.claim("go", true)
	c.line([]byte(`{"type":"system","subtype":"task_notification","status":"stopped","summary":"Background shell command didn't finish before the previous session ended"}`))
	c.line([]byte(`{"type":"result","subtype":"success","num_turns":0,"result":""}`))
	if _, _, running, _ := c.since(0); !running {
		t.Error("the prompt's turn ended before it began")
	}
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
	c.line([]byte(`{"type":"system","subtype":"init","slash_commands":["deploy","compact","/plugin:skill","compact","bad name","<script>"]}`))
	commands, seq, sent := pollCommands(t, s, -1)
	if want := []string{"compact", "deploy", "plugin:skill"}; !sent || !slices.Equal(commands, want) {
		t.Errorf("commands = %v (sent %v), want %v", commands, sent, want)
	}
	if _, _, sent := pollCommands(t, s, seq); sent {
		t.Error("a page with the latest was sent them again")
	}
	// The same list again isn't news; a different one is.
	c.line([]byte(`{"type":"system","subtype":"init","slash_commands":["compact","deploy","plugin:skill"]}`))
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
	first.convo.claim("go", true)
	first.convo.line([]byte(`{"type":"system","subtype":"init","slash_commands":["deploy"]}`))
	first.convo.line([]byte(`{"type":"result","num_turns":1,"result":"ok"}`))
	first.Close()

	second := historyServer(t, &fakeAgent{}, file)
	if commands, _, sent := pollCommands(t, second, -1); !sent || !slices.Equal(commands, []string{"deploy"}) {
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
	c.claim("long job", true)

	clock = clock.Add(59 * time.Minute)
	if q := quietOf(t, s); q != 0 {
		t.Errorf("after 59 quiet minutes: %d", q)
	}
	clock = clock.Add(2 * time.Minute)
	if q := quietOf(t, s); q != 61 {
		t.Errorf("after 61 quiet minutes: %d", q)
	}
	if !c.answering {
		t.Error("the turn was stopped")
	}
	// Anything the agent says ends the quiet.
	c.line([]byte(`{"type":"assistant"}`))
	if q := quietOf(t, s); q != 0 {
		t.Errorf("after the agent spoke: %d", q)
	}
	// Answered, perhaps with work left in the background: not the agent being quiet.
	c.line([]byte(`{"type":"result","num_turns":1,"result":"ok"}`))
	clock = clock.Add(3 * time.Hour)
	if q := quietOf(t, s); q != 0 {
		t.Errorf("answered, three hours on: %d", q)
	}
}

func compactingOf(t *testing.T, s *Server) bool {
	t.Helper()
	rec := do(t, s, req{path: "/api/conversation?after=0", headers: authed(nil)})
	var body struct {
		Compacting bool `json:"compacting"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Compacting
}

// The lines are as claude -p wrote them for a real /compact (trimmed).
func TestThePageIsToldWhileTheAgentCompactsItsContext(t *testing.T) {
	s := converseServer(&fakeAgent{})
	c := &s.convo
	c.claim("a prompt to a full context", true)
	if compactingOf(t, s) {
		t.Error("compacting before the agent said so")
	}
	c.line([]byte(`{"type":"system","subtype":"status","status":"compacting","session_id":"a4f1c427-d374-4814-9a21-221a07f37303"}`))
	if !compactingOf(t, s) {
		t.Error("not compacting once the agent said so")
	}
	c.line([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}`))
	if !compactingOf(t, s) {
		t.Error("another line, during compaction, ended it")
	}
	c.line([]byte(`{"type":"system","subtype":"status","status":null,"compact_result":"success"}`))
	if compactingOf(t, s) {
		t.Error("still compacting after the agent said it was done")
	}
}

func TestATurnThatEndsWhileCompactingIsntStillCompacting(t *testing.T) {
	s := converseServer(&fakeAgent{})
	c := &s.convo
	c.claim("go", true)
	c.line([]byte(`{"type":"system","subtype":"status","status":"compacting"}`))
	c.line([]byte(`{"type":"result","subtype":"error_during_execution","num_turns":1}`))
	if compactingOf(t, s) {
		t.Error("compacting after the turn ended")
	}
	c.claim("again", true)
	if compactingOf(t, s) {
		t.Error("the next turn began compacting")
	}
}

// Before a turn, the sandbox is made ready for it (started, if sbx stopped
// it while idle), saying so on the page while it waits; the turn runs once it
// is, and the saying is taken back, as it's no longer so.
func TestATurnWaitsForTheSandboxToBeReadyAndSaysSoUntilItIs(t *testing.T) {
	f := &fakeAgent{}
	gate := make(chan struct{})
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.start,
		Prepare: func(ctx context.Context, say func(string)) error {
			say("starting the sandbox")
			<-gate
			return nil
		}})
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	waitFor(t, "the note", func() bool { return strings.Contains(kinds(poll(t, s, 0)), "note") })
	seen := poll(t, s, 0)
	if got := kinds(seen); got != "user,note" {
		t.Fatalf("while waiting, events = %s", got)
	}
	if seen.Events[1].Text != "starting the sandbox" {
		t.Errorf("the note = %+v", seen.Events[1])
	}
	noteSeq := seen.Events[1].Seq
	close(gate)
	idle(t, s)

	// A page that had the note is told to take it back.
	later := poll(t, s, seen.Last)
	var retracted []int
	for _, e := range later.Events {
		if e.Kind == "retract" {
			retracted = append(retracted, e.Of)
		}
	}
	if !reflect.DeepEqual(retracted, []int{noteSeq}) {
		t.Errorf("a page that had seen the note is told to retract %v, want [%d]: %s", retracted, noteSeq, kinds(later))
	}
	// A page opened now never has it: nothing to show, nothing to take back.
	if got := kinds(poll(t, s, 0)); got != "user,agent,agent,done" {
		t.Errorf("a page opened after sees %s", got)
	}
}

func TestEveryThingSaidWhileWaitingIsTakenBackOnceTheSandboxIsReady(t *testing.T) {
	f := &fakeAgent{}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.start,
		Prepare: func(ctx context.Context, say func(string)) error {
			say("starting the sandbox")
			say("still starting")
			return nil
		}})
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	idle(t, s)
	for _, e := range poll(t, s, 0).Events {
		if e.Kind == "note" {
			t.Errorf("a note is left: %+v", e)
		}
	}
}

func TestWhatIsSaidWhileWaitingStaysWhenTheSandboxCantBeReadied(t *testing.T) {
	f := &fakeAgent{}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.start,
		Prepare: func(ctx context.Context, say func(string)) error {
			say("starting the sandbox")
			return errors.New("sandbox 'box' didn't start")
		}})
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	p := idle(t, s)
	if got := kinds(p); got != "user,note,error" {
		t.Errorf("events = %s: what it was waiting for explains the error", got)
	}
}

func TestNothingIsRetractedForATurnThatSaidNothing(t *testing.T) {
	f := &fakeAgent{}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.start,
		Prepare: func(context.Context, func(string)) error { return nil }})
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	if got := kinds(idle(t, s)); got != "user,agent,agent,done" {
		t.Errorf("events = %s", got)
	}
}

func TestATakenBackNoteIsNotKeptForTheNextRunOfVibe(t *testing.T) {
	file := filepath.Join(t.TempDir(), "c.json")
	f := &fakeAgent{}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.start, History: file,
		Prepare: func(ctx context.Context, say func(string)) error { say("starting the sandbox"); return nil }})
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	idle(t, s)
	s.Close()
	again := &conversation{}
	if err := again.load(file); err != nil {
		t.Fatal(err)
	}
	events, _, _, _ := again.since(0)
	for _, e := range events {
		if e.Kind == "note" || e.Kind == "retract" {
			t.Errorf("the kept conversation has %+v", e)
		}
	}
}

func TestAPageDoesNotGetATakingBackOfWhatItNeverHad(t *testing.T) {
	c := &conversation{}
	seq := c.noteSaid("starting the sandbox")
	c.retract([]int{seq})
	events, _, _, _ := c.since(0)
	if len(events) != 0 {
		t.Errorf("events = %+v, want none", events)
	}
	events, _, _, _ = c.since(seq)
	if len(events) != 1 || events[0].Kind != "retract" || events[0].Of != seq {
		t.Errorf("a page that had it is told to take it back: %+v", events)
	}
}

func TestATurnTheSandboxCantBeReadiedForIsntRun(t *testing.T) {
	f := &fakeAgent{}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.start,
		Prepare: func(context.Context, func(string)) error {
			return errors.New("sandbox 'box' didn't start — 'sbx ls' says what state it is in")
		}})
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	p := idle(t, s)
	if got := kinds(p); got != "user,error" {
		t.Errorf("events = %s", got)
	}
	if !strings.Contains(p.Events[1].Text, "sbx ls") {
		t.Errorf("the error = %+v", p.Events[1])
	}
	if len(f.prompts) != 0 {
		t.Errorf("the agent was asked anyway: %v", f.prompts)
	}
	// The next prompt can go.
	if code, _ := send(t, s, "/api/conversation/send", `{"prompt":"again"}`); code != http.StatusAccepted {
		t.Errorf("the next prompt: %d", code)
	}
}

// notes is the text of the conversation's notes.
func notes(p pollResult) []string {
	var out []string
	for _, e := range p.Events {
		if e.Kind == "note" {
			out = append(out, e.Text)
		}
	}
	return out
}

// Background work that ends with the agent is said to have, naming it: the status line that named it only clears.
func TestAnAgentThatEndsWithBackgroundWorkSaysWhatWentWithIt(t *testing.T) {
	f := &fakeAgent{linger: make(chan struct{}), crash: make(chan struct{})}
	defer close(f.linger)
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"run the tests in the background"}`)
	answered(t, s)
	close(f.crash)
	waitFor(t, "the agent's end to be said", func() bool { return len(notes(poll(t, s, 0))) > 0 })
	if got := notes(poll(t, s, 0)); len(got) != 1 || !strings.Contains(got[0], `"Run the tests"`) || !strings.Contains(got[0], "background") {
		t.Errorf("notes = %q, want one naming the background work that ended", got)
	}
}

func TestAnAgentThatEndsWithNothingInTheBackgroundSaysNothingOfIt(t *testing.T) {
	f := &fakeAgent{crash: make(chan struct{})}
	s := converseServer(f)
	send(t, s, "/api/conversation/send", `{"prompt":"one"}`)
	idle(t, s)
	close(f.crash)
	waitFor(t, "the agent to end", func() bool { s.convo.mu.Lock(); defer s.convo.mu.Unlock(); return s.convo.agent == nil })
	if got := notes(poll(t, s, 0)); len(got) != 0 {
		t.Errorf("notes = %q", got)
	}
}

func TestVibeStoppingWithBackgroundWorkSaysWhatWentWithItOnce(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.conversation.json")
	f := &fakeAgent{linger: make(chan struct{})}
	defer close(f.linger)
	first := historyServer(t, f, file)
	send(t, first, "/api/conversation/send", `{"prompt":"run the tests in the background"}`)
	answered(t, first)
	first.Close()
	second := historyServer(t, f, file)
	if got := notes(poll(t, second, 0)); len(got) != 1 || !strings.Contains(got[0], `"Run the tests"`) {
		t.Errorf("notes = %q, want one naming the background work vibe's stopping ended", got)
	}
}

// A vibe that dies can say nothing: the next one, finding the work it had in the background, says it.
func TestAVibeThatDiedWithBackgroundWorkHasItSaidByTheNext(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.conversation.json")
	f := &fakeAgent{linger: make(chan struct{})}
	defer close(f.linger)
	first := historyServer(t, f, file)
	send(t, first, "/api/conversation/send", `{"prompt":"run the tests in the background"}`)
	answered(t, first)
	// No Close: it died. (Closed once the test is done with it, before its folder goes.)
	t.Cleanup(func() { first.Close() })
	second := historyServer(t, f, file)
	got := notes(poll(t, second, 0))
	if len(got) != 1 || !strings.Contains(got[0], `"Run the tests"`) || !strings.Contains(got[0], "next") {
		t.Errorf("notes = %q, want one naming the work left running, and that it is ended when the agent next starts", got)
	}
}
