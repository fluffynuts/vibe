package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// drop puts a request in the store's inbox, as the agent's command does.
func drop(t *testing.T, s *Store, name string, req Request) {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Inbox(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Inbox(), name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func clockAt(s *Store) *time.Time {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return &now
}

func TestTheAgentsRequestsAreTakenInInOrder(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	drop(t, s, "0001.json", Request{Op: "start", Note: "the login change"})
	drop(t, s, "0002.json", Request{Op: "comment", File: "main.go", Line: 12, EndLine: 14, Body: "P1: this leaks"})
	drop(t, s, "0003.json", Request{Op: "general", Body: "Mostly fine."})
	drop(t, s, "0004.json", Request{Op: "order", Stops: []Stop{{File: "main.go", Line: 12, Body: "start here", Label: "the core"}}})
	drop(t, s, "0005.json", Request{Op: "done"})

	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	if r.Note != "the login change" || r.State != Done {
		t.Errorf("review: note %q, state %q", r.Note, r.State)
	}
	if len(r.Threads) != 2 {
		t.Fatalf("threads: %+v", r.Threads)
	}
	c := r.Threads[0]
	if c.ID != 1 || c.File != "main.go" || c.Line != 12 || c.EndLine != 14 || c.Side != "new" || c.Status != Open ||
		len(c.Comments) != 1 || c.Comments[0].Author != Agent || c.Comments[0].Body != "P1: this leaks" {
		t.Errorf("the comment: %+v", c)
	}
	if g := r.Threads[1]; g.ID != 2 || g.File != "" || g.Comments[0].Body != "Mostly fine." {
		t.Errorf("the general comment: %+v", g)
	}
	if len(r.Order) != 1 || r.Order[0].Label != "the core" {
		t.Errorf("order: %+v", r.Order)
	}
	if left, _ := os.ReadDir(s.Inbox()); len(left) != 0 {
		t.Errorf("the inbox still has %d requests", len(left))
	}
	// Kept, for the next page or vibe.
	again, err := NewStore(s.dir).Load()
	if err != nil || len(again.Threads) != 2 {
		t.Errorf("read back: %+v, %v", again, err)
	}
}

func TestRepliesAndResolvingWorkOnThreadsByID(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	drop(t, s, "1.json", Request{Op: "comment", File: "a.go", Line: 3, Body: "why?"})
	if _, err := s.Take(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Do(Request{Op: "reply", Thread: 1, Body: "because"}, User); err != nil {
		t.Fatal(err)
	}
	drop(t, s, "2.json", Request{Op: "reply", Thread: 1, Body: "fair enough"})
	drop(t, s, "3.json", Request{Op: "resolve", Thread: 1, Body: "left as it is"})
	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	th := r.Threads[0]
	var said []string
	for _, c := range th.Comments {
		said = append(said, c.Author+": "+c.Body)
	}
	if got, want := strings.Join(said, " | "), "agent: why? | user: because | agent: fair enough | agent: left as it is"; got != want {
		t.Errorf("thread:\n%s\nwant:\n%s", got, want)
	}
	if th.Status != Resolved {
		t.Errorf("status %q", th.Status)
	}
	if _, err := s.Do(Request{Op: "reopen", Thread: 1}, User); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Load(); r.Threads[0].Status != Open {
		t.Errorf("reopened: %q", r.Threads[0].Status)
	}
}

func TestABadRequestIsSaidToBeAndTheRestStillTakenIn(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	drop(t, s, "1.json", Request{Op: "reply", Thread: 42, Body: "to nothing"})
	drop(t, s, "2.json", Request{Op: "comment", File: "", Line: 3, Body: "where?"})
	os.WriteFile(filepath.Join(s.Inbox(), "3.json"), []byte("not json"), 0o644)
	drop(t, s, "4.json", Request{Op: "comment", File: "a.go", Line: 3, Body: "fine"})
	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Threads) != 1 || r.Threads[0].Body() != "fine" {
		t.Errorf("threads: %+v", r.Threads)
	}
	if len(r.Problems) != 3 || !strings.Contains(r.Problems[0], "no thread 42") {
		t.Errorf("problems: %q", r.Problems)
	}
	if left, _ := os.ReadDir(s.Inbox()); len(left) != 0 {
		t.Errorf("bad requests left in the inbox: %d", len(left))
	}
}

func TestStartingAReviewAnewKeepsTheLastOne(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	drop(t, s, "1.json", Request{Op: "start", Note: "first"})
	drop(t, s, "2.json", Request{Op: "comment", File: "a.go", Line: 1, Body: "old"})
	s.Take()
	drop(t, s, "3.json", Request{Op: "start", Note: "second"})
	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	if r.Note != "second" || len(r.Threads) != 0 || r.State != InProgress {
		t.Errorf("the new review: %+v", r)
	}
	kept, _ := filepath.Glob(filepath.Join(s.dir, "history", "*.json"))
	if len(kept) != 1 {
		t.Errorf("kept reviews: %v", kept)
	}
}

func TestTheUserCantDoTheAgentsPart(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, op := range []string{"start", "comment", "order", "done"} {
		if _, err := s.Do(Request{Op: op, File: "a.go", Line: 1, Body: "x"}, User); err == nil {
			t.Errorf("the user could %s", op)
		}
	}
}
