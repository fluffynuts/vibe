package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"vibe/internal/policy"
)

const token = "0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeSbx struct {
	mu    sync.Mutex
	calls [][]string
	out   map[string]string
	err   error
}

func (f *fakeSbx) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	if f.err != nil {
		return nil, f.err
	}
	// policy log / policy ls ... --type X: pick by verb and kind.
	key := args[1]
	if key == "ls" {
		key = "ls-" + args[len(args)-1]
	}
	if out, ok := f.out[key]; ok {
		return []byte(out), nil
	}
	return []byte("[]"), nil
}

func (f *fakeSbx) called() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.calls...)
}

const testPort = 5350

func newServer(sbx *fakeSbx) *Server {
	return New(Config{
		Sandbox:      "vibe",
		Token:        token,
		Port:         testPort,
		Policy:       &policy.Client{Sandbox: "vibe", Run: sbx.run},
		ClipboardURL: func() string { return "http://localhost:5390" },
	})
}

type req struct {
	method  string
	path    string
	host    string
	headers map[string]string
	body    string
}

func do(t *testing.T, s *Server, r req) *httptest.ResponseRecorder {
	t.Helper()
	if r.method == "" {
		r.method = "GET"
	}
	if r.host == "" {
		r.host = fmt.Sprintf("localhost:%d", testPort)
	}
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	hr.Host = r.host
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, hr)
	return w
}

func authed(h map[string]string) map[string]string {
	out := map[string]string{"X-Vibe-Token": token}
	for k, v := range h {
		out[k] = v
	}
	return out
}

func post(path, body string) req {
	return req{method: "POST", path: path, body: body,
		headers: authed(map[string]string{"Content-Type": "application/json"})}
}

func TestPageAndItsFilesAreServedWithoutAToken(t *testing.T) {
	s := newServer(&fakeSbx{})
	for path, want := range map[string]string{"/": "text/html", "/app.js": "javascript", "/app.css": "text/css"} {
		w := do(t, s, req{path: path})
		if w.Code != 200 {
			t.Errorf("%s: status %d", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, want) {
			t.Errorf("%s: content type %q", path, ct)
		}
	}
	if w := do(t, s, req{path: "/nothing"}); w.Code != 404 {
		t.Errorf("unknown path: status %d", w.Code)
	}
	if w := do(t, s, req{path: "/ui/app.js"}); w.Code != 404 {
		t.Errorf("embedded dir is reachable by its path: status %d", w.Code)
	}
}

func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	s := newServer(&fakeSbx{})
	for _, path := range []string{"/", "/api/ping", "/api/state"} {
		h := do(t, s, req{path: path}).Header()
		for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "X-Frame-Options", "Cache-Control"} {
			if h.Get(name) == "" {
				t.Errorf("%s has no %s", path, name)
			}
		}
		if h.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s allows cross-origin reads", path)
		}
	}
	csp := do(t, s, req{path: "/"}).Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP %q allows inline script", csp)
	}
}

func TestOtherHostsAreRefused(t *testing.T) {
	s := newServer(&fakeSbx{})
	for _, host := range []string{
		"evil.example.com", "evil.example.com:5350", "host.docker.internal:5350", "localhost", "localhost:5351",
		"127.0.0.1.evil.example:5350", "localhost.evil.example:5350",
	} {
		for _, path := range []string{"/", "/api/ping"} {
			w := do(t, s, req{path: path, host: host})
			if w.Code != http.StatusMisdirectedRequest {
				t.Errorf("Host %q %s: status %d, want 421", host, path, w.Code)
			}
		}
	}
	for _, host := range []string{"localhost:5350", "127.0.0.1:5350", "[::1]:5350", "LOCALHOST:5350"} {
		if w := do(t, s, req{path: "/api/ping", host: host}); w.Code != 200 {
			t.Errorf("Host %q: status %d, want 200", host, w.Code)
		}
	}
}

func TestAPIAsksForTheToken(t *testing.T) {
	sbx := &fakeSbx{}
	s := newServer(sbx)
	for _, path := range []string{"/api/state", "/api/log", "/api/rules", "/api/fs-rules"} {
		for name, headers := range map[string]map[string]string{
			"none":  nil,
			"wrong": {"X-Vibe-Token": strings.Repeat("0", len(token))},
			"short": {"X-Vibe-Token": token[:10]},
			"empty": {"X-Vibe-Token": ""},
		} {
			if w := do(t, s, req{path: path, headers: headers}); w.Code != http.StatusUnauthorized {
				t.Errorf("%s with %s token: status %d, want 401", path, name, w.Code)
			}
		}
	}
	w := do(t, s, req{method: "POST", path: "/api/allow", body: `{"host":"a.example.com"}`,
		headers: map[string]string{"Content-Type": "application/json"}})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("allow without a token: status %d, want 401", w.Code)
	}
	if calls := sbx.called(); len(calls) != 0 {
		t.Errorf("a request with no token ran sbx: %v", calls)
	}
}

func TestChangesNeedPOSTJSONAndSameOrigin(t *testing.T) {
	s := newServer(&fakeSbx{})
	good := `{"host":"a.example.com"}`

	if w := do(t, s, req{method: "GET", path: "/api/allow", headers: authed(nil)}); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET allow: status %d, want 405", w.Code)
	}
	refused := map[string]req{
		"form post": {method: "POST", path: "/api/allow", body: good, headers: authed(map[string]string{"Content-Type": "application/x-www-form-urlencoded"})},
		"text post": {method: "POST", path: "/api/allow", body: good, headers: authed(map[string]string{"Content-Type": "text/plain"})},
		"no type":   {method: "POST", path: "/api/allow", body: good, headers: authed(nil)},
		"other origin": {method: "POST", path: "/api/allow", body: good,
			headers: authed(map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example.com"})},
		"null origin": {method: "POST", path: "/api/allow", body: good,
			headers: authed(map[string]string{"Content-Type": "application/json", "Origin": "null"})},
		"cross-site": {method: "POST", path: "/api/allow", body: good,
			headers: authed(map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"})},
	}
	for name, r := range refused {
		if w := do(t, s, r); w.Code < 400 {
			t.Errorf("%s: status %d, want a refusal", name, w.Code)
		}
	}

	ok := post("/api/allow", good)
	ok.headers["Origin"] = fmt.Sprintf("http://localhost:%d", testPort)
	ok.headers["Sec-Fetch-Site"] = "same-origin"
	if w := do(t, s, ok); w.Code != 200 {
		t.Errorf("a proper same-origin post: status %d: %s", w.Code, w.Body)
	}
}

func TestRefusedChangesNeverRunSbx(t *testing.T) {
	sbx := &fakeSbx{}
	s := newServer(sbx)
	for _, body := range []string{
		`{"host":"*"}`, `{"host":"**"}`, `{"host":"-rf"}`, `{"host":"a.com,b.com"}`, `{"host":""}`, `{}`, `not json`,
		`{"host":"a.com; rm -rf /"}`,
	} {
		w := do(t, s, post("/api/allow", body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("allow %s: status %d, want 400", body, w.Code)
		}
		w = do(t, s, post("/api/deny", body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("deny %s: status %d, want 400", body, w.Code)
		}
	}
	for _, body := range []string{`{"id":"--all"}`, `{"id":""}`, `{}`, `{"id":"a b"}`} {
		if w := do(t, s, post("/api/remove", body)); w.Code != http.StatusBadRequest {
			t.Errorf("remove %s: status %d, want 400", body, w.Code)
		}
	}
	if calls := sbx.called(); len(calls) != 0 {
		t.Errorf("sbx ran: %v", calls)
	}
}

func TestChangesRunTheScopedSbxCommandAndSayWhich(t *testing.T) {
	sbx := &fakeSbx{}
	s := newServer(sbx)

	w := do(t, s, post("/api/allow", `{"host":"api.example.com"}`))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var got struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if !got.OK || got.Command != "sbx policy allow network --sandbox vibe api.example.com" {
		t.Errorf("response = %+v", got)
	}
	do(t, s, post("/api/deny", `{"host":"ads.example.com"}`))
	do(t, s, post("/api/remove", `{"id":"r-1"}`))
	want := [][]string{
		{"policy", "allow", "network", "--sandbox", "vibe", "api.example.com"},
		{"policy", "deny", "network", "--sandbox", "vibe", "ads.example.com"},
		{"policy", "rm", "network", "--sandbox", "vibe", "--id", "r-1"},
	}
	if !reflect.DeepEqual(sbx.called(), want) {
		t.Errorf("calls = %v, want %v", sbx.called(), want)
	}
}

func TestSbxFailuresAreReportedNotHidden(t *testing.T) {
	sbx := &fakeSbx{err: policy.ErrNotSignedIn}
	s := newServer(sbx)
	w := do(t, s, req{path: "/api/log", headers: authed(nil)})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status %d", w.Code)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "not-signed-in" || !strings.Contains(body["error"], "sbx login") {
		t.Errorf("body = %v", body)
	}
}

func TestReadsReturnWhatSbxSaid(t *testing.T) {
	sbx := &fakeSbx{out: map[string]string{
		"log":           `[{"resource":"github.com","decision":"allowed","count":3,"last_seen":"2026-10-09T12:00:00Z"}]`,
		"ls-network":    `[{"rule_id":"r-1","resources":["github.com"],"decision":"allow","source":"local"}]`,
		"ls-filesystem": `[{"rule_id":"f-1","resources":["/work"],"decision":"allow","source":"org"}]`,
	}}
	s := newServer(sbx)

	var log policy.Log
	json.Unmarshal(do(t, s, req{path: "/api/log", headers: authed(nil)}).Body.Bytes(), &log)
	if len(log.Entries) != 1 || log.Entries[0].Host != "github.com" || log.Entries[0].Hits != 3 {
		t.Errorf("log = %+v", log)
	}
	var rules policy.Rules
	json.Unmarshal(do(t, s, req{path: "/api/rules", headers: authed(nil)}).Body.Bytes(), &rules)
	if len(rules.Rules) != 1 || !rules.Rules[0].Removable {
		t.Errorf("rules = %+v", rules)
	}
	var fs policy.Rules
	json.Unmarshal(do(t, s, req{path: "/api/fs-rules", headers: authed(nil)}).Body.Bytes(), &fs)
	if len(fs.Rules) != 1 || fs.Rules[0].Resources[0] != "/work" || fs.Rules[0].Removable {
		t.Errorf("fs rules = %+v", fs)
	}
}

func TestStateSaysWhereTheClipboardIs(t *testing.T) {
	s := newServer(&fakeSbx{})
	var got struct {
		Sandbox      string `json:"sandbox"`
		ClipboardURL string `json:"clipboardUrl"`
	}
	json.Unmarshal(do(t, s, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &got)
	if got.Sandbox != "vibe" || got.ClipboardURL != "http://localhost:5390" {
		t.Errorf("state = %+v", got)
	}

	none := New(Config{Sandbox: "vibe", Token: token, Port: testPort})
	got.ClipboardURL = "stale"
	json.Unmarshal(do(t, none, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &got)
	if got.ClipboardURL != "" {
		t.Errorf("a sandbox with no clipboard page was given %q", got.ClipboardURL)
	}
}

func TestSeveralReadsShareOneSbxCall(t *testing.T) {
	sbx := &fakeSbx{}
	s := newServer(sbx)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			do(t, s, req{path: "/api/log", headers: authed(nil)})
		}()
	}
	wg.Wait()
	// one for the log, one for the rules that say which blocked hosts are
	// allowed now
	if n := len(sbx.called()); n != 2 {
		t.Errorf("sbx ran %d times for 8 simultaneous reads, want 2", n)
	}
}

func TestAChangeForgetsTheCachedReads(t *testing.T) {
	sbx := &fakeSbx{}
	s := newServer(sbx)
	do(t, s, req{path: "/api/log", headers: authed(nil)})
	do(t, s, post("/api/allow", `{"host":"a.example.com"}`))
	do(t, s, req{path: "/api/log", headers: authed(nil)})
	var logs int
	for _, c := range sbx.called() {
		if c[1] == "log" {
			logs++
		}
	}
	if logs != 2 {
		t.Errorf("log read %d times, want 2 (before and after the change)", logs)
	}
}

func TestStartListensOnLoopbackAndRunningFindsIt(t *testing.T) {
	s, err := Start(Config{Sandbox: "vibe", Token: token, Port: 0, Policy: &policy.Client{Sandbox: "vibe", Run: (&fakeSbx{}).run}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Port() == 0 {
		t.Fatal("no port")
	}
	if got := s.ln.Addr().String(); !strings.HasPrefix(got, "127.0.0.1:") {
		t.Errorf("listening on %s, want loopback only", got)
	}
	if want := fmt.Sprintf("http://localhost:%d/#%s", s.Port(), token); s.URL() != want {
		t.Errorf("URL = %q, want %q", s.URL(), want)
	}
	if !Running(s.Port(), "vibe") {
		t.Error("Running does not find the page")
	}
	if Running(s.Port(), "another") {
		t.Error("Running took another sandbox's page for this one's")
	}

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/state", s.Port()))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("state without a token: status %d", resp.StatusCode)
	}
}

func TestRunningIsFalseWhereNothingListens(t *testing.T) {
	s, err := Start(Config{Sandbox: "vibe", Token: token, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	port := s.Port()
	s.Close()
	if Running(port, "vibe") {
		t.Error("Running found a closed page")
	}
}

func TestStartNeedsAToken(t *testing.T) {
	if _, err := Start(Config{Sandbox: "vibe"}); err == nil {
		t.Error("expected an error with no token")
	}
}

func TestTheClipboardURLIsAskedForRarely(t *testing.T) {
	var asked atomic.Int32
	s := New(Config{Sandbox: "vibe", Token: token, Port: testPort, ClipboardURL: func() string {
		asked.Add(1)
		return "http://localhost:5390"
	}})
	for i := 0; i < 5; i++ {
		do(t, s, req{path: "/api/state", headers: authed(nil)})
	}
	if n := asked.Load(); n != 1 {
		t.Errorf("asked %d times, want 1", n)
	}
}
