package companion

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// The page polls /api/state every couple of seconds and a slow answer holds
// one of the browser's few connections to vibe: what the sandbox (sbx) or the
// repository is asked for must never be waited on once there's an answer to
// give, however slow the machine gets. An old answer is given while a new one
// is fetched.

type slowThing struct {
	name   string
	config func(release chan struct{}, calls *atomic.Int32) Config
	// read is the part of the state answer that comes from the thing.
	read func(state map[string]json.RawMessage) string
	// first and later are what it read before and after it was let go.
	first, later string
}

func slowThings() []slowThing {
	base := func() Config { return Config{Sandbox: "box", Token: token, Port: testPort} }
	return []slowThing{
		{
			name: "the agent's theme",
			config: func(release chan struct{}, calls *atomic.Int32) Config {
				c := base()
				c.ThemeColor = func() string {
					if calls.Add(1) == 1 {
						return "#111111"
					}
					<-release
					return "#222222"
				}
				return c
			},
			read: func(st map[string]json.RawMessage) string {
				var s string
				json.Unmarshal(st["themeColor"], &s)
				return s
			},
			first: "#111111", later: "#222222",
		},
		{
			name: "the folder's git status",
			config: func(release chan struct{}, calls *atomic.Int32) Config {
				c := base()
				c.Workspace = "/work"
				c.Git = func(string) *GitInfo {
					if calls.Add(1) == 1 {
						return &GitInfo{Branch: "first"}
					}
					<-release
					return &GitInfo{Branch: "later"}
				}
				return c
			},
			read: func(st map[string]json.RawMessage) string {
				var g GitInfo
				json.Unmarshal(st["git"], &g)
				return g.Branch
			},
			first: "first", later: "later",
		},
		{
			name: "why the clipboard page isn't up",
			config: func(release chan struct{}, calls *atomic.Int32) Config {
				c := base()
				c.ClipboardURL = func() string { return "http://localhost:5390" }
				c.ClipboardReady = func(string) bool { return false }
				c.ClipboardTrouble = func() string {
					if calls.Add(1) == 1 {
						return "first trouble"
					}
					<-release
					return "later trouble"
				}
				return c
			},
			read: func(st map[string]json.RawMessage) string {
				var s string
				json.Unmarshal(st["clipboardTrouble"], &s)
				return s
			},
			first: "first trouble", later: "later trouble",
		},
	}
}

func stateOf(t *testing.T, s *Server) (map[string]json.RawMessage, time.Duration) {
	t.Helper()
	began := time.Now()
	rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
	took := time.Since(began)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/state: %d %s", rec.Code, rec.Body)
	}
	var st map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st, took
}

// expire makes what the server kept of key old enough to be fetched again.
func expire(s *Server, key string) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.entries[key].at = time.Now().Add(-time.Hour)
}

func TestTheStateIsNotHeldUpByWhatIsSlowToRefresh(t *testing.T) {
	for _, thing := range slowThings() {
		t.Run(thing.name, func(t *testing.T) {
			release := make(chan struct{})
			var calls atomic.Int32
			s := New(thing.config(release, &calls))
			defer close(release)

			if st, _ := stateOf(t, s); thing.read(st) != thing.first {
				t.Fatalf("the first answer is what's read: %q", thing.read(st))
			}
			for key := range s.cache.entries {
				expire(s, key)
			}
			// Many pages' polls at once, as a slow answer lets them pile up.
			results := make(chan string, 8)
			for i := 0; i < 8; i++ {
				go func() {
					st, took := stateOf(t, s)
					if took > 2*time.Second {
						results <- "took " + took.String()
						return
					}
					results <- thing.read(st)
				}()
			}
			for i := 0; i < 8; i++ {
				select {
				case got := <-results:
					if got != thing.first {
						t.Errorf("a poll while the refresh was slow got %q, want the old answer %q", got, thing.first)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("a state call is still waiting on the refresh")
				}
			}
			// The refresh runs in the background: it has begun when it's been
			// asked for, and more polls must not begin more of them.
			deadline := time.Now().Add(10 * time.Second)
			for calls.Load() < 2 {
				if time.Now().After(deadline) {
					t.Fatal("the stale answer was never refreshed")
				}
				time.Sleep(time.Millisecond)
			}
			for i := 0; i < 8; i++ {
				stateOf(t, s)
			}
			if n := calls.Load(); n != 2 {
				t.Errorf("read %d times, want 2: one refresh, shared by all the polls", n)
			}
		})
	}
}

func TestWhatWasRefreshedInTheBackgroundIsGivenNext(t *testing.T) {
	for _, thing := range slowThings() {
		t.Run(thing.name, func(t *testing.T) {
			release := make(chan struct{})
			var calls atomic.Int32
			s := New(thing.config(release, &calls))
			stateOf(t, s)
			for key := range s.cache.entries {
				expire(s, key)
			}
			// The poll that finds it old is answered at once, not when the
			// refresh is done.
			answered := make(chan struct{})
			go func() { stateOf(t, s); close(answered) }()
			select {
			case <-answered:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("the poll waited on the refresh")
			}
			close(release)
			// The refresh ends when it ends: the next answers show it.
			deadline := time.Now().Add(10 * time.Second)
			for {
				st, _ := stateOf(t, s)
				if thing.read(st) == thing.later {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("still %q after the refresh was let go", thing.read(st))
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}
