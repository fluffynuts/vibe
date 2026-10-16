package companion

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeClipboard is the sandbox's clipboard page, as far as the companion
// uses it: it keeps what's pasted, says when it's already waiting, and
// lists its items.
type fakeClipboard struct {
	mu     sync.Mutex
	pastes []string // content type, file name, body
	items  []map[string]interface{}
}

func (f *fakeClipboard) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/items":
			body, _ := io.ReadAll(r.Body)
			f.pastes = append(f.pastes, r.Header.Get("Content-Type")+" "+r.Header.Get("X-Filename")+" "+string(body))
			w.Header().Set("Content-Type", "application/json")
			if string(body) == "seen" {
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, `{"duplicate":true,"id":1,"kind":"image"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":2,"kind":"image"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/items":
			json.NewEncoder(w).Encode(map[string]interface{}{"items": f.items})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func pasteReq(body, contentType, name string) req {
	return req{method: "POST", path: "/api/clipboard/paste", body: body,
		headers: authed(map[string]string{"Content-Type": contentType, "X-Filename": name})}
}

// What's pasted on the Review tab goes to the clipboard page, as a paste
// there would: the agent gets it with the next message.
func TestAPasteOnTheReviewTabGoesToTheClipboardPage(t *testing.T) {
	f := &fakeClipboard{}
	srv := f.serve(t)
	s, _ := reviewTabServer(t)
	s.cfg.ClipboardURL = func() string { return srv.URL }
	rec := do(t, s, pasteReq("png bytes", "image/png", "shot.png"))
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"kind":"image"`) {
		t.Errorf("paste: %d %s", rec.Code, rec.Body)
	}
	if len(f.pastes) != 1 || f.pastes[0] != "image/png shot.png png bytes" {
		t.Errorf("the clipboard page got %q", f.pastes)
	}
	// One already waiting is said to be.
	if rec := do(t, s, pasteReq("seen", "image/png", "")); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"duplicate":true`) {
		t.Errorf("again: %d %s", rec.Code, rec.Body)
	}
}

func TestAPasteWithNoClipboardPageSaysWhy(t *testing.T) {
	s, _ := reviewTabServer(t)
	if rec := do(t, s, pasteReq("x", "image/png", "")); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "clipboard") {
		t.Errorf("no clipboard page: %d %s", rec.Code, rec.Body)
	}
	s.cfg.ClipboardURL = func() string { return "http://127.0.0.1:1" }
	if rec := do(t, s, pasteReq("x", "image/png", "")); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "couldn't be reached") {
		t.Errorf("clipboard page down: %d %s", rec.Code, rec.Body)
	}
}

// The Review tab can't show the clipboard page: it says how many pastes are
// waiting to go with the next message.
func TestTheReviewTabIsToldHowManyPastesAreWaiting(t *testing.T) {
	f := &fakeClipboard{items: []map[string]interface{}{
		{"id": 3, "delivered": nil}, {"id": 2, "delivered": nil}, {"id": 1, "delivered": "2026-10-16T10:00:00Z"},
	}}
	srv := f.serve(t)
	s, _ := reviewTabServer(t)
	s.cfg.ClipboardURL = func() string { return srv.URL }
	rec := do(t, s, req{path: "/api/review", headers: authed(nil)})
	var body struct {
		Pastes *int `json:"pastes"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Pastes == nil || *body.Pastes != 2 {
		t.Errorf("pastes: %v (%s)", body.Pastes, rec.Body)
	}
}

// The clipboard page is the sandbox's: a redirect from it isn't followed
// to fetch, from this machine, wherever it says.
func TestAPasteDoesntFollowARedirectFromTheClipboardPage(t *testing.T) {
	var hit sync.Once
	reached := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit.Do(func() { reached = true }) }))
	t.Cleanup(elsewhere.Close)
	clip := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/secret", http.StatusFound)
	}))
	t.Cleanup(clip.Close)
	s, _ := reviewTabServer(t)
	s.cfg.ClipboardURL = func() string { return clip.URL }
	rec := do(t, s, pasteReq("png bytes", "image/png", "shot.png"))
	if reached {
		t.Error("the redirect was followed")
	}
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "redirect") {
		t.Errorf("paste: %d %s", rec.Code, rec.Body)
	}
}

func TestAPasteTooLargeSaysSo(t *testing.T) {
	f := &fakeClipboard{}
	srv := f.serve(t)
	s, _ := reviewTabServer(t)
	s.cfg.ClipboardURL = func() string { return srv.URL }
	rec := do(t, s, pasteReq(strings.Repeat("x", maxPaste+1), "image/png", "huge.png"))
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "50 MB") {
		t.Errorf("paste: %d %s", rec.Code, rec.Body)
	}
}
