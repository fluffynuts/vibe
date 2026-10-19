package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/nugetfetch"
)

type fakeSync struct {
	mu      sync.Mutex
	state   nugetfetch.SyncState
	checks  int
	runs    int
	runErr  error
	release chan struct{}
}

func (f *fakeSync) Check() { f.mu.Lock(); f.checks++; f.mu.Unlock() }
func (f *fakeSync) State() nugetfetch.SyncState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}
func (f *fakeSync) Run(ctx context.Context) error {
	f.mu.Lock()
	f.runs++
	f.state.Running = true
	f.mu.Unlock()
	if f.release != nil {
		<-f.release
	}
	f.mu.Lock()
	f.state.Running = false
	f.mu.Unlock()
	return f.runErr
}

var someMissing = nugetfetch.SyncState{
	Count:   3,
	Missing: []nugetfetch.Group{{Framework: "net8.0", Packages: []nugetfetch.Package{{ID: "Dapper", Version: "2.1.86"}, {ID: "Acornima", Version: "1.1.0"}, {ID: "Gone.Pkg", Version: "9.9.9"}}}},
	Checked: time.Now(),
}

func syncServer(f *fakeSync) *Server {
	return New(Config{Sandbox: "box", Token: token, Port: testPort, PackageSync: f})
}

type syncBody struct {
	Count      int `json:"count"`
	Actionable int `json:"actionable"`
	Missing    []struct {
		Framework string `json:"framework"`
		Packages  []struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"packages"`
	} `json:"missing"`
	Running bool `json:"running"`
	Last    *struct {
		Fetched    int    `json:"fetched"`
		Error      string `json:"error"`
		Unresolved []struct {
			ID string `json:"id"`
		} `json:"unresolved"`
	} `json:"last"`
	Skipped  int      `json:"skipped"`
	Problems []string `json:"problems"`
}

func getSync(t *testing.T, s *Server) syncBody {
	t.Helper()
	rec := do(t, s, req{path: "/api/packages/sync", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	var b syncBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestThePageIsToldWhatTheCacheLacksByFramework(t *testing.T) {
	b := getSync(t, syncServer(&fakeSync{state: someMissing}))
	if b.Count != 3 || len(b.Missing) != 1 || b.Missing[0].Framework != "net8.0" || len(b.Missing[0].Packages) != 3 || b.Missing[0].Packages[0].ID != "Dapper" {
		t.Errorf("body = %+v", b)
	}
}

func TestAPageWithNoSyncHasNoSyncEndpoints(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if rec := do(t, s, req{path: "/api/packages/sync", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("status %d", rec.Code)
	}
}

func TestRunningStartsTheFetchAtOnceAndDoesntWaitForIt(t *testing.T) {
	f := &fakeSync{state: someMissing, release: make(chan struct{})}
	s := syncServer(f)
	answered := make(chan int, 1)
	go func() { answered <- do(t, s, post("/api/packages/sync/run", `{}`)).Code }()
	select {
	case code := <-answered:
		if code != http.StatusOK {
			t.Errorf("status %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request waited on the fetch")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !getSync(t, s).Running {
		if time.Now().After(deadline) {
			t.Fatal("never running")
		}
		time.Sleep(time.Millisecond)
	}
	close(f.release)
}

func TestARunWhileOneIsUnderWayIsRefused(t *testing.T) {
	f := &fakeSync{state: someMissing, release: make(chan struct{})}
	f.state.Running = true
	s := syncServer(f)
	if rec := do(t, s, post("/api/packages/sync/run", `{}`)); rec.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", rec.Code)
	}
	close(f.release)
}

func TestARunIsOnlyAskedForFromThePageItself(t *testing.T) {
	f := &fakeSync{state: someMissing}
	s := syncServer(f)
	for name, r := range map[string]req{
		"no token":       {method: "POST", path: "/api/packages/sync/run", body: `{}`, headers: map[string]string{"Content-Type": "application/json"}},
		"another origin": {method: "POST", path: "/api/packages/sync/run", body: `{}`, headers: authed(map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example"})},
	} {
		if rec := do(t, s, r); rec.Code == http.StatusOK {
			t.Errorf("%s: started from outside the page", name)
		}
	}
	if f.runs != 0 {
		t.Errorf("ran %d times", f.runs)
	}
}

func TestWhatIsFixableDrawsAttentionToTheAdminTabButWhatNoFeedHasDoesNot(t *testing.T) {
	attention := func(f *fakeSync) []string {
		rec := do(t, syncServer(f), req{path: "/api/state", headers: authed(nil)})
		var st struct {
			Attention map[string][]string `json:"attention"`
		}
		json.Unmarshal(rec.Body.Bytes(), &st)
		return st.Attention["admin"]
	}
	got := attention(&fakeSync{state: someMissing})
	if len(got) != 1 || !strings.Contains(got[0], "3 packages") || !strings.Contains(got[0], "IDE") {
		t.Errorf("attention = %v", got)
	}
	// Two of the three are all that's missing, and the third is known to be
	// nowhere: nothing to do about it.
	state := someMissing
	state.Last = &nugetfetch.SyncResult{Unresolved: []nugetfetch.Package{{ID: "Gone.Pkg", Version: "9.9.9"}}}
	got = attention(&fakeSync{state: state})
	if len(got) != 1 || !strings.Contains(got[0], "2 packages") {
		t.Errorf("attention = %v", got)
	}
	state.Missing = []nugetfetch.Group{{Framework: "net8.0", Packages: []nugetfetch.Package{{ID: "Gone.Pkg", Version: "9.9.9"}}}}
	state.Count = 1
	if got = attention(&fakeSync{state: state}); len(got) != 0 {
		t.Errorf("attention = %v, want none for what no feed has", got)
	}
	running := someMissing
	running.Running = true
	if got = attention(&fakeSync{state: running}); len(got) != 0 {
		t.Errorf("attention = %v while it's being fetched", got)
	}
}

func TestTheWorkspaceIsLookedAtEverySoOftenUntilTheServerEnds(t *testing.T) {
	f := &fakeSync{}
	s := syncServer(f)
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() { s.pollPackageSync(done, time.Millisecond); close(ended) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		n := f.checks
		f.mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("looked %d times", n)
		}
		time.Sleep(time.Millisecond)
	}
	close(done)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("still looking after the server ended")
	}
}

func TestWhenFetchesAreAllowedAnythingFixableIsFetchedWithoutBeingAsked(t *testing.T) {
	f := &fakeSync{state: someMissing}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, PackageSync: f, PackageSyncAuto: func() bool { return true }})
	done := make(chan struct{})
	go s.pollPackageSync(done, time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		n := f.runs
		f.mu.Unlock()
		if n >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never fetched")
		}
		time.Sleep(time.Millisecond)
	}
	close(done)
}

func TestWhenFetchesNeedApprovalNothingIsFetchedByItself(t *testing.T) {
	f := &fakeSync{state: someMissing}
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, PackageSync: f, PackageSyncAuto: func() bool { return false }})
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() { s.pollPackageSync(done, time.Millisecond); close(ended) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		n := f.checks
		f.mu.Unlock()
		if n >= 20 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never looked")
		}
		time.Sleep(time.Millisecond)
	}
	close(done)
	<-ended
	if f.runs != 0 {
		t.Errorf("fetched %d times unasked", f.runs)
	}
}

// The page marks the card with the same count the Admin tab's dot is drawn
// for: what a fetch could bring, not what no feed has, and none while one runs.
func TestThePageIsToldHowManyOfThePackagesAFetchCouldBring(t *testing.T) {
	if got := getSync(t, syncServer(&fakeSync{state: someMissing})).Actionable; got != 3 {
		t.Errorf("actionable = %d, want 3", got)
	}
	nowhere := someMissing
	nowhere.Last = &nugetfetch.SyncResult{Unresolved: []nugetfetch.Package{{ID: "Gone.Pkg", Version: "9.9.9"}}}
	if got := getSync(t, syncServer(&fakeSync{state: nowhere})).Actionable; got != 2 {
		t.Errorf("actionable = %d, want 2 (one is known to be nowhere)", got)
	}
	running := someMissing
	running.Running = true
	if got := getSync(t, syncServer(&fakeSync{state: running})).Actionable; got != 0 {
		t.Errorf("actionable = %d while it's being fetched, want 0", got)
	}
}

// The Conversation tab says there are packages to fetch without the Admin tab
// being open, so the page's state carries the count.
func TestThePageStateSaysHowManyPackagesAFetchCouldBring(t *testing.T) {
	fetchable := func(f *fakeSync) int {
		rec := do(t, syncServer(f), req{path: "/api/state", headers: authed(nil)})
		var st struct {
			PackagesToFetch int `json:"packagesToFetch"`
		}
		json.Unmarshal(rec.Body.Bytes(), &st)
		return st.PackagesToFetch
	}
	if got := fetchable(&fakeSync{state: someMissing}); got != 3 {
		t.Errorf("packagesToFetch = %d, want 3", got)
	}
	nowhere := someMissing
	nowhere.Last = &nugetfetch.SyncResult{Unresolved: []nugetfetch.Package{{ID: "Gone.Pkg", Version: "9.9.9"}}}
	if got := fetchable(&fakeSync{state: nowhere}); got != 2 {
		t.Errorf("packagesToFetch = %d, want 2", got)
	}
	running := someMissing
	running.Running = true
	if got := fetchable(&fakeSync{state: running}); got != 0 {
		t.Errorf("packagesToFetch = %d while fetching, want 0", got)
	}
}
