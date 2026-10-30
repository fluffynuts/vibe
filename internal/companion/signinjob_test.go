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

	"vibe/internal/signin"
)

// fakeLogin is a sign-in command: it prints what the test says, takes what is sent to it, and ends when told.
type fakeLogin struct {
	lines   chan string
	done    chan error
	mu      sync.Mutex
	sent    []string
	stopped bool
	noInput bool
}

func newFakeLogin() *fakeLogin {
	return &fakeLogin{lines: make(chan string), done: make(chan error, 1)}
}

func (f *fakeLogin) Lines() <-chan string { return f.lines }
func (f *fakeLogin) Done() <-chan error   { return f.done }
func (f *fakeLogin) Send(line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noInput {
		return errors.New("this process takes no input")
	}
	f.sent = append(f.sent, line)
	return nil
}
func (f *fakeLogin) Stop() {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	f.finish(errors.New("signal: killed"))
}
func (f *fakeLogin) say(line string) { f.lines <- line }
func (f *fakeLogin) finish(err error) {
	defer func() { recover() }() // Stop after the end
	close(f.lines)
	f.done <- err
}
func (f *fakeLogin) wasSent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func loginServer(f *fakeLogin, kinds *[]string) *Server {
	s := signInServer(nil)
	s.cfg.SignIn = func(ctx context.Context) signin.Status { return signin.Status{} }
	s.cfg.SignInStart = func(kind string) (LoginProcess, error) {
		if kinds != nil {
			*kinds = append(*kinds, kind)
		}
		return f, nil
	}
	return s
}

type jobBody struct {
	Kind    string   `json:"kind"`
	Running bool     `json:"running"`
	Done    bool     `json:"done"`
	OK      bool     `json:"ok"`
	URL     string   `json:"url"`
	Code    string   `json:"code"`
	Lines   []string `json:"lines"`
	Error   string   `json:"error"`
}

func getJob(t *testing.T, s *Server) jobBody {
	t.Helper()
	rec := do(t, s, req{path: "/api/signin/job", headers: authed(nil)})
	var j jobBody
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	return j
}

func postJSON(t *testing.T, s *Server, path, body string) (int, string) {
	t.Helper()
	rec := do(t, s, req{method: "POST", path: path, body: body, headers: authed(map[string]string{"Content-Type": "application/json"})})
	return rec.Code, rec.Body.String()
}

// waitFor polls the job until ok says it is so: it is made by a goroutine reading the process's lines.
func jobWaitFor(t *testing.T, s *Server, what string, ok func(jobBody) bool) jobBody {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if j := getJob(t, s); ok(j) {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("never saw %s: %+v", what, getJob(t, s))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSigningInRunsTheCommandOfThatKindAndShowsItsAddressAndCode(t *testing.T) {
	f := newFakeLogin()
	var kinds []string
	s := loginServer(f, &kinds)
	if code, body := postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`); code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	if len(kinds) != 1 || kinds[0] != "docker" {
		t.Fatalf("started %v", kinds)
	}
	f.say("Your one-time device confirmation code is: SVSQ-TVDJ")
	f.say("Open this URL to sign in: https://login.docker.com/activate?user_code=SVSQ-TVDJ")
	f.say("Waiting for authentication...")
	j := jobWaitFor(t, s, "the address", func(j jobBody) bool { return j.URL != "" && len(j.Lines) == 3 })
	if j.URL != "https://login.docker.com/activate?user_code=SVSQ-TVDJ" || j.Code != "SVSQ-TVDJ" || !j.Running || j.Kind != "docker" {
		t.Errorf("job = %+v", j)
	}
}

func TestTheFirstAddressIsTheOneShownAndATrailingStopIsNotPartOfIt(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"agent"}`)
	f.say("If your browser doesn't open, visit https://claude.ai/oauth/authorize?code=true&state=abc.")
	f.say("Or see https://example.org/other")
	j := jobWaitFor(t, s, "lines", func(j jobBody) bool { return len(j.Lines) == 2 })
	if j.URL != "https://claude.ai/oauth/authorize?code=true&state=abc" {
		t.Errorf("url = %q", j.URL)
	}
}

func TestACommandThatEndsWell(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	f.say("Signed in as me.")
	f.finish(nil)
	j := jobWaitFor(t, s, "the end", func(j jobBody) bool { return j.Done })
	if !j.OK || j.Running || j.Error != "" {
		t.Errorf("job = %+v", j)
	}
}

func TestACommandThatFailsSaysSoWithWhatItSaid(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	f.say("error: that code has expired")
	f.finish(errors.New("exit status 1"))
	j := jobWaitFor(t, s, "the end", func(j jobBody) bool { return j.Done })
	if j.OK || !strings.Contains(j.Error, "exit status 1") {
		t.Errorf("job = %+v", j)
	}
	if len(j.Lines) != 1 || j.Lines[0] != "error: that code has expired" {
		t.Errorf("lines = %q", j.Lines)
	}
}

func TestOnlyOneSignInRunsAtATime(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	code, body := postJSON(t, s, "/api/signin/start", `{"kind":"agent"}`)
	if code != http.StatusConflict || !strings.Contains(body, "signin-running") {
		t.Errorf("%d %s", code, body)
	}
}

func TestAnotherSignInMayStartOnceTheLastHasEnded(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	f.finish(nil)
	jobWaitFor(t, s, "the end", func(j jobBody) bool { return j.Done })
	g := newFakeLogin()
	s.cfg.SignInStart = func(string) (LoginProcess, error) { return g, nil }
	if code, body := postJSON(t, s, "/api/signin/start", `{"kind":"agent"}`); code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	if j := getJob(t, s); j.Kind != "agent" || j.Done || j.URL != "" {
		t.Errorf("the new job carries the old one: %+v", j)
	}
}

func TestAKindThatIsNeitherIsRefusedAndNothingStarts(t *testing.T) {
	var kinds []string
	s := loginServer(newFakeLogin(), &kinds)
	if code, _ := postJSON(t, s, "/api/signin/start", `{"kind":"rm -rf"}`); code != http.StatusBadRequest {
		t.Errorf("status %d", code)
	}
	if len(kinds) != 0 {
		t.Errorf("started %v", kinds)
	}
}

func TestACommandThatCannotStartIsSaidAndLeavesNoJobRunning(t *testing.T) {
	s := loginServer(nil, nil)
	s.cfg.SignInStart = func(string) (LoginProcess, error) { return nil, errors.New("vibe can't sign cursor in yet") }
	code, body := postJSON(t, s, "/api/signin/start", `{"kind":"agent"}`)
	if code != http.StatusInternalServerError || !strings.Contains(body, "can't sign cursor in") {
		t.Errorf("%d %s", code, body)
	}
	if j := getJob(t, s); j.Running {
		t.Errorf("job = %+v", j)
	}
}

func TestACodeIsGivenToTheCommandAsALineAndNeverKept(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"agent"}`)
	if code, body := postJSON(t, s, "/api/signin/code", `{"code":"abc-123-secret"}`); code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	if got := f.wasSent(); len(got) != 1 || got[0] != "abc-123-secret" {
		t.Errorf("sent %q", got)
	}
	f.say("Login successful")
	body := do(t, s, req{path: "/api/signin/job", headers: authed(nil)}).Body.String()
	if strings.Contains(body, "abc-123-secret") {
		t.Errorf("the code is in what the page is told: %s", body)
	}
}

func TestACodeIsRefusedWhenNothingIsWaitingForOneOrItIsNotOneLine(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	if code, _ := postJSON(t, s, "/api/signin/code", `{"code":"x"}`); code != http.StatusConflict {
		t.Errorf("with no sign-in: status %d", code)
	}
	postJSON(t, s, "/api/signin/start", `{"kind":"agent"}`)
	for _, bad := range []string{`{"code":""}`, `{"code":"two\nlines"}`, `{"code":"` + strings.Repeat("x", 5000) + `"}`} {
		if code, _ := postJSON(t, s, "/api/signin/code", bad); code != http.StatusBadRequest {
			t.Errorf("%.30s...: status %d", bad, code)
		}
	}
	if got := f.wasSent(); len(got) != 0 {
		t.Errorf("sent %q", got)
	}
}

func TestACodeForACommandThatTakesNoneIsRefused(t *testing.T) {
	f := newFakeLogin()
	f.noInput = true
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	if code, _ := postJSON(t, s, "/api/signin/code", `{"code":"x"}`); code != http.StatusConflict {
		t.Errorf("status %d", code)
	}
}

func TestCancellingEndsTheCommandAndTheJobSaysItWasCancelled(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	if code, body := postJSON(t, s, "/api/signin/cancel", `{}`); code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	j := jobWaitFor(t, s, "the end", func(j jobBody) bool { return j.Done })
	if !f.stopped || j.OK || !strings.Contains(strings.ToLower(j.Error), "cancel") {
		t.Errorf("stopped %v, job %+v", f.stopped, j)
	}
}

func TestASignInIsGivenUpOnAfterTheTimeAllowed(t *testing.T) {
	old := signInJobLimit
	signInJobLimit = 30 * time.Millisecond
	defer func() { signInJobLimit = old }()
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	j := jobWaitFor(t, s, "giving up", func(j jobBody) bool { return j.Done })
	if !f.stopped || j.OK || !strings.Contains(j.Error, "waited") {
		t.Errorf("stopped %v, job %+v", f.stopped, j)
	}
}

func TestOnlyTheLastLinesAreKept(t *testing.T) {
	f := newFakeLogin()
	s := loginServer(f, nil)
	postJSON(t, s, "/api/signin/start", `{"kind":"docker"}`)
	for i := 0; i < 100; i++ {
		f.say("line " + strings.Repeat("x", 3000))
	}
	f.say("last")
	j := jobWaitFor(t, s, "the last line", func(j jobBody) bool { return len(j.Lines) > 0 && j.Lines[len(j.Lines)-1] == "last" })
	if len(j.Lines) > 40 {
		t.Errorf("%d lines kept", len(j.Lines))
	}
	for _, l := range j.Lines {
		if len(l) > 600 {
			t.Errorf("a line of %d characters kept", len(l))
		}
	}
}

func TestTheSignInCallsNeedTheTokenAndAHostThatOffersThem(t *testing.T) {
	s := loginServer(newFakeLogin(), nil)
	for _, path := range []string{"/api/signin/start", "/api/signin/code", "/api/signin/cancel"} {
		if rec := do(t, s, req{method: "POST", path: path, body: `{}`, headers: map[string]string{"Content-Type": "application/json"}}); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without the token: %d", path, rec.Code)
		}
	}
	bare := signInServer(func(c context.Context) signin.Status { return signin.Status{} })
	if code, _ := postJSON(t, bare, "/api/signin/start", `{"kind":"docker"}`); code != http.StatusNotFound {
		t.Errorf("a host that offers no sign-in: status %d", code)
	}
}
