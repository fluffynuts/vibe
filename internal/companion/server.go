// Package companion serves the page vibe opens beside a sandbox: tabs for
// the sandbox's clipboard page, its network log and rules, and its
// filesystem rules, so none of that needs the sbx TUI.
//
// The page runs on the host, in the vibe process, and drives the host's own
// sbx. That makes it the one thing in reach of the sandbox that can change
// the sandbox's network rules, so it is built to be unreachable by anything
// but the user's own browser:
//
//   - it listens on 127.0.0.1 only;
//   - it answers only requests whose Host is localhost, 127.0.0.1 or ::1 on
//     its own port, which defeats DNS rebinding — and also anything the
//     sandbox sends through host.docker.internal;
//   - every call to /api (bar the ping that tells two vibes apart) needs the
//     sandbox's secret token in a header, which a page on another site
//     can't add without a CORS preflight this server never allows. The token
//     is only ever in the URL's fragment, which a browser keeps from the
//     server, from logs and from Referer headers;
//   - changes need POST, a JSON body, and a same-origin request;
//   - it never offers a rule that would match every host, and it only ever
//     scopes a rule to this sandbox.
//
// What sbx printed — host names, which the sandbox itself chooses — is only
// ever shown as text, never as markup (see ui/app.js).
package companion

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"vibe/internal/policy"
)

//go:embed ui
var uiFiles embed.FS

// Config is what the page is served for.
type Config struct {
	Sandbox string
	// Token is the secret every /api call must carry.
	Token string
	// Port is the host port to listen on, on 127.0.0.1.
	Port   int
	Policy *policy.Client
	// ClipboardURL is where the sandbox's clipboard page is reachable on the
	// host, or "" when the sandbox has none; it is asked for often, so it
	// should be quick.
	ClipboardURL func() string
}

// Server is a running companion page.
type Server struct {
	cfg   Config
	ln    net.Listener
	http  *http.Server
	ui    fs.FS
	cache *cache
}

// New makes a Server without listening, which is how tests drive Handler.
func New(cfg Config) *Server {
	ui, _ := fs.Sub(uiFiles, "ui")
	return &Server{cfg: cfg, ui: ui, cache: newCache(time.Second)}
}

// Start listens on 127.0.0.1 at cfg.Port and serves in the background.
func Start(cfg Config) (*Server, error) {
	if cfg.Token == "" {
		return nil, errors.New("companion: no token")
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil {
		return nil, err
	}
	s := New(cfg)
	s.ln = ln
	s.cfg.Port = ln.Addr().(*net.TCPAddr).Port
	s.http = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go s.http.Serve(ln)
	return s, nil
}

// Close stops serving.
func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	return s.http.Close()
}

// Port is the port the page is served on.
func (s *Server) Port() int { return s.cfg.Port }

// URL is where the user opens the page: the token rides in the fragment.
func (s *Server) URL() string {
	return fmt.Sprintf("http://localhost:%d/#%s", s.cfg.Port, s.cfg.Token)
}

// Running says whether a companion page for sandbox is already being served
// on port — by another vibe attached to the same sandbox.
func Running(port int, sandbox string) bool {
	client := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		App     string `json:"app"`
		Sandbox string `json:"sandbox"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&body); err != nil {
		return false
	}
	return body.App == "vibe-companion" && body.Sandbox == sandbox
}

// Handler is the whole page: its files and its API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static := http.FileServer(http.FS(s.ui))
	mux.Handle("GET /{$}", static)
	mux.Handle("GET /app.js", static)
	mux.Handle("GET /app.css", static)

	mux.HandleFunc("GET /api/ping", s.ping)
	mux.HandleFunc("GET /api/state", s.authed(s.state))
	mux.HandleFunc("GET /api/log", s.authed(s.reading("log", func() (interface{}, error) { return s.cfg.Policy.ResolvedLog() })))
	mux.HandleFunc("GET /api/rules", s.authed(s.reading("rules", func() (interface{}, error) { return s.cfg.Policy.NetworkRules() })))
	mux.HandleFunc("GET /api/fs-rules", s.authed(s.reading("fs-rules", func() (interface{}, error) { return s.cfg.Policy.FilesystemRules() })))
	mux.HandleFunc("POST /api/allow", s.authed(s.changing(s.allow)))
	mux.HandleFunc("POST /api/deny", s.authed(s.changing(s.deny)))
	mux.HandleFunc("POST /api/remove", s.authed(s.changing(s.remove)))

	return s.guarded(mux)
}

// guarded refuses requests for any other Host, and sets the headers every
// response carries.
func (s *Server) guarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r.Host) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		// frame-src is for the clipboard tab: the sandbox's page, on the
		// host's own loopback.
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; "+
			"connect-src 'self'; frame-src http://localhost:* http://127.0.0.1:*; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostOK(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return false
	}
	if port != strconv.Itoa(s.cfg.Port) {
		return false
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Vibe-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": "this page doesn't have the right token — open the URL vibe printed",
				"code":  "unauthorized",
			})
			return
		}
		next(w, r)
	}
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"app": "vibe-companion", "sandbox": s.cfg.Sandbox})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	clipboard := ""
	if s.cfg.ClipboardURL != nil {
		v, _ := s.cache.get("clipboard-url", 5*time.Second, func() (interface{}, error) {
			return s.cfg.ClipboardURL(), nil
		})
		clipboard, _ = v.(string)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sandbox":      s.cfg.Sandbox,
		"clipboardUrl": clipboard,
	})
}

// reading serves a read of sbx's, briefly cached so several open tabs don't
// each run sbx every couple of seconds.
func (s *Server) reading(key string, read func() (interface{}, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := s.cache.get(key, 0, read)
		if err != nil {
			writeSbxError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

type change func(body []byte) (command []string, err error)

// changing wraps a change to the sandbox's rules: it must be a same-origin
// POST of JSON, and it forgets what was cached, so the page's next read
// shows the result.
func (s *Server) changing(do change) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "expected application/json"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request refused"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unreadable body"})
			return
		}
		command, err := do(body)
		s.cache.clear()
		if err != nil {
			writeSbxError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"command": "sbx " + strings.Join(command, " "),
		})
	}
}

type invalid struct{ error }

func (s *Server) allow(body []byte) ([]string, error) {
	host, err := hostIn(body)
	if err != nil {
		return nil, err
	}
	c := s.cfg.Policy
	return c.AllowCommand(host), c.Allow(host)
}

func (s *Server) deny(body []byte) ([]string, error) {
	host, err := hostIn(body)
	if err != nil {
		return nil, err
	}
	c := s.cfg.Policy
	return c.DenyCommand(host), c.Deny(host)
}

func (s *Server) remove(body []byte) ([]string, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, invalid{errors.New("expected {\"id\": ...}")}
	}
	if err := policy.ValidRuleID(req.ID); err != nil {
		return nil, invalid{err}
	}
	c := s.cfg.Policy
	return c.RemoveCommand(req.ID), c.Remove(req.ID)
}

func hostIn(body []byte) (string, error) {
	var req struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", invalid{errors.New("expected {\"host\": ...}")}
	}
	req.Host = strings.TrimSpace(req.Host)
	if err := policy.ValidHost(req.Host); err != nil {
		return "", invalid{err}
	}
	return req.Host, nil
}

func writeSbxError(w http.ResponseWriter, err error) {
	var bad invalid
	switch {
	case errors.As(err, &bad):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": bad.Error()})
	case errors.Is(err, policy.ErrNotSignedIn):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error(), "code": "not-signed-in"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error(), "code": "sbx"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// cache holds the result of an sbx call for a moment, and shares one call
// among requests that arrive while it runs.
type cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	done chan struct{}
	at   time.Time
	val  interface{}
	err  error
}

func newCache(ttl time.Duration) *cache {
	return &cache{ttl: ttl, entries: map[string]*cacheEntry{}}
}

// get returns key's value if it is younger than ttl (the cache's own, when
// ttl is zero), else makes one with fn. Callers that arrive while fn runs
// wait for that run's answer.
func (c *cache) get(key string, ttl time.Duration, fn func() (interface{}, error)) (interface{}, error) {
	if ttl == 0 {
		ttl = c.ttl
	}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		select {
		case <-e.done:
			if time.Since(e.at) < ttl {
				c.mu.Unlock()
				return e.val, e.err
			}
		default:
			c.mu.Unlock()
			<-e.done
			return e.val, e.err
		}
	}
	e := &cacheEntry{done: make(chan struct{})}
	c.entries[key] = e
	c.mu.Unlock()

	e.val, e.err = fn()
	e.at = time.Now()
	close(e.done)
	return e.val, e.err
}

func (c *cache) clear() {
	c.mu.Lock()
	c.entries = map[string]*cacheEntry{}
	c.mu.Unlock()
}
