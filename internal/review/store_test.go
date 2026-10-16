package review

import (
	"encoding/json"
	"fmt"
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
	// Commenting is the user's too, since 2026-10-16: their own comments,
	// sent to the agent when they choose (see Unsent). So is starting a
	// review, from the page (TestThePageStartsAReview).
	for _, op := range []string{"order", "done"} {
		if _, err := s.Do(Request{Op: op, File: "a.go", Line: 1, Body: "x"}, User); err == nil {
			t.Errorf("the user could %s", op)
		}
	}
}

func refs(r []Ref) string {
	var out []string
	for _, x := range r {
		out = append(out, fmt.Sprintf("%d.%d", x.Thread, x.Comment))
	}
	return strings.Join(out, " ")
}

// The user comments on lines, and replies, before sending any of it: what
// the agent hasn't been given is what's unsent.
func TestTheUsersCommentsWaitUntilSent(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	s.Do(Request{Op: "comment", File: "a.go", Line: 3, Body: "agent's"}, Agent)
	if _, err := s.Do(Request{Op: "comment", File: "b.go", Side: "new", Line: 7, Code: "x := 1", Body: "please fix"}, User); err != nil {
		t.Fatal(err)
	}
	s.Do(Request{Op: "reply", Thread: 1, Body: "why though?"}, User)
	r, _ := s.Load()
	if got := refs(Unsent(r)); got != "1.1 2.0" {
		t.Errorf("unsent: %s, want the reply (1.1) and the new comment (2.0)", got)
	}
	if th := r.Threads[1]; th.Comments[0].Author != User || th.Code != "x := 1" || th.Comments[0].Sent {
		t.Errorf("the user's comment: %+v", th)
	}
	if err := s.MarkSent(Unsent(r)); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Load()
	if got := refs(Unsent(r)); got != "" {
		t.Errorf("unsent after sending: %s", got)
	}
}

// tickingClock moves on a second each time it's read, so every comment is
// said at a time of its own, as the page knows it by.
func tickingClock(s *Store) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { now = now.Add(time.Second); return now }
}

// at is when the i'th comment on thread id was said.
func at(t *testing.T, s *Store, id, i int) time.Time {
	t.Helper()
	r, _ := s.Load()
	th := r.thread(id)
	if th == nil || i >= len(th.Comments) {
		t.Fatalf("no comment %d on thread %d", i, id)
	}
	return th.Comments[i].At
}

func TestAnUnsentCommentCanBeEditedOrDeleted(t *testing.T) {
	s := NewStore(t.TempDir())
	tickingClock(s)
	s.Do(Request{Op: "comment", File: "b.go", Line: 7, Body: "typo"}, User)
	s.Do(Request{Op: "comment", File: "c.go", Line: 1, Body: "never mind"}, User)
	if _, err := s.Do(Request{Op: "edit", Thread: 1, At: at(t, s, 1, 0), Body: "fixed wording"}, User); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Do(Request{Op: "delete", Thread: 2, At: at(t, s, 2, 0)}, User); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Load()
	if len(r.Threads) != 1 || r.Threads[0].Body() != "fixed wording" {
		t.Errorf("threads: %+v", r.Threads)
	}
	// A deleted reply goes; its thread stays.
	s.Do(Request{Op: "reply", Thread: 1, Body: "and another thing"}, User)
	if _, err := s.Do(Request{Op: "delete", Thread: 1, At: at(t, s, 1, 1)}, User); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Load(); len(r.Threads[0].Comments) != 1 {
		t.Errorf("after deleting the reply: %+v", r.Threads[0].Comments)
	}
}

// With the review open in two tabs, one deleting a reply moves the ones
// after it up: the other's edit still lands on the comment it was of.
func TestEditingFindsTheCommentWhereverItHasMoved(t *testing.T) {
	s := NewStore(t.TempDir())
	tickingClock(s)
	s.Do(Request{Op: "comment", File: "a.go", Line: 1, Body: "first"}, User)
	s.Do(Request{Op: "reply", Thread: 1, Body: "second"}, User)
	s.Do(Request{Op: "reply", Thread: 1, Body: "third"}, User)
	third := at(t, s, 1, 2)
	if _, err := s.Do(Request{Op: "delete", Thread: 1, At: at(t, s, 1, 1)}, User); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Do(Request{Op: "edit", Thread: 1, At: third, Body: "third, edited"}, User); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Load()
	var got []string
	for _, c := range r.Threads[0].Comments {
		got = append(got, c.Body)
	}
	if strings.Join(got, "|") != "first|third, edited" {
		t.Errorf("comments: %q", got)
	}
	// One deleted in the other tab is gone, not some other comment.
	if _, err := s.Do(Request{Op: "edit", Thread: 1, At: at(t, s, 1, 0).Add(time.Hour), Body: "x"}, User); err == nil {
		t.Error("edited a comment that isn't there")
	}
}

func TestOnlyTheUsersUnsentCommentsCanBeChanged(t *testing.T) {
	s := NewStore(t.TempDir())
	tickingClock(s)
	s.Do(Request{Op: "comment", File: "a.go", Line: 3, Body: "agent's"}, Agent)
	s.Do(Request{Op: "comment", File: "b.go", Line: 7, Body: "sent already"}, User)
	r, _ := s.Load()
	s.MarkSent(Unsent(r))
	s.Do(Request{Op: "comment", File: "c.go", Line: 1, Body: "mine"}, User)
	s.Do(Request{Op: "reply", Thread: 3, Body: "my reply"}, User)
	for _, tc := range []struct {
		why string
		req Request
	}{
		{"the agent's", Request{Op: "edit", Thread: 1, At: at(t, s, 1, 0), Body: "x"}},
		{"sent", Request{Op: "edit", Thread: 2, At: at(t, s, 2, 0), Body: "x"}},
		{"sent, deleted", Request{Op: "delete", Thread: 2, At: at(t, s, 2, 0)}},
		{"no such comment", Request{Op: "edit", Thread: 3, At: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Body: "x"}},
		{"a comment with replies", Request{Op: "delete", Thread: 3, At: at(t, s, 3, 0)}},
		{"edited to nothing", Request{Op: "edit", Thread: 3, At: at(t, s, 3, 0), Body: "  "}},
	} {
		if _, err := s.Do(tc.req, User); err == nil {
			t.Errorf("%s: allowed", tc.why)
		}
	}
	// Nor can the agent change the user's.
	if _, err := s.Do(Request{Op: "edit", Thread: 3, At: at(t, s, 3, 0), Body: "x"}, Agent); err == nil {
		t.Error("the agent edited the user's comment")
	}
}

// Comments are marked sent as they were when sent, whatever's deleted in
// the meantime.
func TestMarkingSentFollowsTheCommentNotItsPlace(t *testing.T) {
	s := NewStore(t.TempDir())
	now := clockAt(s)
	s.Do(Request{Op: "comment", File: "a.go", Line: 1, Body: "first"}, User)
	*now = now.Add(time.Second)
	s.Do(Request{Op: "reply", Thread: 1, Body: "second"}, User)
	r, _ := s.Load()
	sent := Unsent(r)[1:] // just "second"
	*now = now.Add(time.Second)
	s.Do(Request{Op: "reply", Thread: 1, Body: "third"}, User)
	s.Do(Request{Op: "delete", Thread: 1, At: sent[0].At}, User) // "second" goes; "third" moves up
	s.MarkSent(sent)
	r, _ = s.Load()
	if c := r.Threads[0].Comments[1]; c.Body != "third" || c.Sent {
		t.Errorf("a comment that wasn't sent was marked sent: %+v", c)
	}
}

// The page lights the Review tab when the agent has done something to the
// review since it was last looked at: the store says when that was.
func TestTheStoreSaysWhenTheAgentLastUpdatedTheReview(t *testing.T) {
	s := NewStore(t.TempDir())
	now := clockAt(s)
	r, _ := s.Take()
	if !r.AgentAt.IsZero() {
		t.Errorf("updated before the agent did anything: %v", r.AgentAt)
	}
	drop(t, s, "1.json", Request{Op: "comment", File: "a.go", Line: 1, Body: "x"})
	r, _ = s.Take()
	if !r.AgentAt.Equal(*now) {
		t.Errorf("after the agent's comment: %v, want %v", r.AgentAt, *now)
	}
	at := r.AgentAt
	*now = now.Add(time.Minute)
	s.Do(Request{Op: "reply", Thread: 1, Body: "mine"}, User)
	if r, _ := s.Take(); !r.AgentAt.Equal(at) {
		t.Errorf("the user's reply counted as the agent's update: %v", r.AgentAt)
	}
	drop(t, s, "2.json", Request{Op: "resolve", Thread: 1})
	if r, _ := s.Take(); !r.AgentAt.Equal(*now) {
		t.Errorf("the agent resolving didn't count: %v", r.AgentAt)
	}
}

// A review kept before comments were marked sent: a comment of the user's
// the agent has answered after is one it was given, so it isn't sent again.
func TestAReviewFromBeforeSentCountsAnsweredCommentsAsSent(t *testing.T) {
	dir := t.TempDir()
	old := `{"threads":[{"id":1,"file":"a.go","line":1,"status":"open","comments":[
		{"author":"agent","body":"P3: x","at":"2026-10-15T10:00:00Z"},
		{"author":"user","body":"why?","at":"2026-10-15T10:01:00Z"},
		{"author":"agent","body":"because","at":"2026-10-15T10:02:00Z"},
		{"author":"user","body":"not answered yet","at":"2026-10-15T10:03:00Z"}]}],"lastId":1}`
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewStore(dir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := refs(Unsent(r)); got != "1.3" {
		t.Errorf("unsent: %s, want only the one not answered (1.3)", got)
	}
}

// A request that can't be read is said to be, and taken out of the inbox,
// as a bad one is: left there, every Take would find it again, and count
// it as news from the agent each time.
func TestARequestThatCantBeReadIsSaidToBeAndTakenOut(t *testing.T) {
	s := NewStore(t.TempDir())
	now := clockAt(s)
	s.readFile = func(string) ([]byte, error) { return nil, os.ErrPermission }
	drop(t, s, "1.json", Request{Op: "general", Body: "x"})
	path := filepath.Join(s.Inbox(), "1.json")
	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "1.json") {
		t.Errorf("problems: %q", r.Problems)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("still in the inbox: %v", err)
	}
	at := r.AgentAt
	*now = now.Add(time.Minute)
	if r, _ := s.Take(); !r.AgentAt.Equal(at) {
		t.Errorf("taken as news again: %v, then %v", at, r.AgentAt)
	}
}

// The page starts a review: just the diff, or with the agent asked to
// review it, and of what's not pushed yet as well as a branch or what's
// uncommitted.
func TestThePageStartsAReview(t *testing.T) {
	s := NewStore(t.TempDir())
	now := clockAt(s)
	r, err := s.Do(Request{Op: "start", Ref: "origin/feat", Unpushed: true}, User)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "" || !r.Unpushed || r.Ref != "origin/feat" || !r.Started.Equal(*now) {
		t.Errorf("just the diff: %+v", r)
	}
	r, _ = s.Do(Request{Op: "start", Reviewing: true}, User)
	if r.State != InProgress || r.Unpushed || r.Ref != "" {
		t.Errorf("with the agent asked: %+v", r)
	}
}

func TestAReviewIsClosedAndReopened(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	s.Do(Request{Op: "start"}, User)
	s.Do(Request{Op: "comment", File: "a.go", Line: 1, Body: "x"}, Agent)
	r, err := s.Do(Request{Op: "close", Body: "You closed it."}, User)
	if err != nil {
		t.Fatal(err)
	}
	if r.Closed != "You closed it." || len(r.Threads) != 1 {
		t.Errorf("closed: %+v", r)
	}
	if r, _ := s.Do(Request{Op: "reopen-review"}, User); r.Closed != "" || len(r.Threads) != 1 {
		t.Errorf("reopened: %+v", r)
	}
	if _, err := s.Do(Request{Op: "close"}, Agent); err == nil {
		t.Error("the agent closed the review: that's the user's to do")
	}
	// A review started afresh isn't closed, whatever the last one was.
	s.Do(Request{Op: "close", Body: "x"}, User)
	drop(t, s, "1.json", Request{Op: "start"})
	if r, _ := s.Take(); r.Closed != "" {
		t.Errorf("the agent's new review: %+v", r)
	}
}

// A review started for nothing (the agent was busy) is undone by putting
// the last one back as it was.
func TestTheLastReviewCanBePutBack(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	s.Do(Request{Op: "comment", File: "a.go", Line: 1, Body: "x"}, Agent)
	last, _ := s.Load()
	s.Do(Request{Op: "start", Reviewing: true}, User)
	if err := s.Put(last); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Load(); len(r.Threads) != 1 || r.State != last.State {
		t.Errorf("put back: %+v", r)
	}
}

// A comment that has followed its code keeps its new place, and the line
// it was first made on, however often it moves.
func TestACommentsMoveIsKept(t *testing.T) {
	s := NewStore(t.TempDir())
	clockAt(s)
	s.Do(Request{Op: "comment", File: "a.go", Line: 10, EndLine: 12, Body: "x"}, Agent)
	if err := s.Move([]Move{{Thread: 1, Line: 14}}); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Load()
	if th := r.Threads[0]; th.Line != 14 || th.EndLine != 16 || th.MovedFrom != 10 {
		t.Errorf("moved: %+v", th)
	}
	s.Move([]Move{{Thread: 1, Line: 20}, {Thread: 9, Line: 1}})
	if th, _ := s.Load(); th.Threads[0].Line != 20 || th.Threads[0].MovedFrom != 10 {
		t.Errorf("moved again: %+v", th.Threads[0])
	}
}

// The user ticks files as viewed: kept with the fingerprint of the change
// they saw; unticked, forgotten. The agent doesn't tick them.
func TestFilesAreTickedAsViewed(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Do(Request{Op: "view", File: "a.go", Code: "f1"}, User); err != nil {
		t.Fatal(err)
	}
	s.Do(Request{Op: "view", File: "b.go", Code: "f2"}, User)
	s.Do(Request{Op: "unview", File: "b.go"}, User)
	r, _ := s.Load()
	if len(r.Viewed) != 1 || r.Viewed["a.go"] != "f1" {
		t.Errorf("viewed: %v", r.Viewed)
	}
	if _, err := s.Do(Request{Op: "view", File: "c.go", Code: "f3"}, Agent); err == nil {
		t.Error("the agent ticked a file")
	}
	if _, err := s.Do(Request{Op: "view", File: "c.go"}, User); err == nil {
		t.Error("ticked with no fingerprint of what was seen")
	}
}
