package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vibe/internal/review"
)

// reviewTabServer serves the review of a git repository made for the test,
// with one uncommitted change, and gives the store behind it.
func reviewTabServer(t *testing.T) (*Server, *review.Store) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}} {
		gitIn(t, dir, args...)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "start")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	store := review.NewStore(t.TempDir())
	return New(Config{Sandbox: "box", Token: token, Port: testPort, ReviewTab: &ReviewTab{Workspace: dir, Store: store}}), store
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

type reviewResult struct {
	Review       review.Review `json:"review"`
	Diff         []review.File `json:"diff"`
	Error        string        `json:"diffError"`
	QuietMinutes int           `json:"quietMinutes"`
}

func getReview(t *testing.T, s *Server, path string) reviewResult {
	t.Helper()
	rec := do(t, s, req{path: path, headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
	}
	var r reviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheReviewTabGetsTheDiffAndTheAgentsComments(t *testing.T) {
	s, store := reviewTabServer(t)
	os.MkdirAll(store.Inbox(), 0o755)
	os.WriteFile(filepath.Join(store.Inbox(), "1.json"), []byte(`{"op":"comment","file":"a.txt","line":2,"body":"why two?"}`), 0o644)

	r := getReview(t, s, "/api/review?diff=1")
	if r.Error != "" || len(r.Diff) != 1 || r.Diff[0].Path != "a.txt" {
		t.Fatalf("diff: %+v, error %q", r.Diff, r.Error)
	}
	if len(r.Review.Threads) != 1 || r.Review.Threads[0].Body() != "why two?" {
		t.Errorf("threads: %+v", r.Review.Threads)
	}
	// Polls are of the review alone: the diff is worked out when asked for.
	if r := getReview(t, s, "/api/review"); r.Diff != nil {
		t.Error("the diff came with a poll")
	}
}

func TestTheUserRepliesAndResolvesFromTheTab(t *testing.T) {
	s, store := reviewTabServer(t)
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "why two?"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, post("/api/review/do", `{"op":"reply","thread":1,"body":"it's needed"}`)); rec.Code != http.StatusOK {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/review/do", `{"op":"resolve","thread":1}`)); rec.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", rec.Code, rec.Body)
	}
	th := getReview(t, s, "/api/review").Review.Threads[0]
	if th.Status != review.Resolved || len(th.Comments) != 2 || th.Comments[1].Author != review.User {
		t.Errorf("thread: %+v", th)
	}
	// The page comments as the user now (2026-10-16), but finishing the
	// review is still the agent's.
	if rec := do(t, s, post("/api/review/do", `{"op":"done"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("the page doing the agent's part: %d", rec.Code)
	}
}

func TestAPageWithNoReviewHasNoReviewTab(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if rec := do(t, s, req{path: "/api/review", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/review: %d", rec.Code)
	}
}

// Asking the agent about a thread is a turn: the question is kept as the
// user's reply, and the agent is told the thread and how to answer on it.
func TestAskingTheAgentAboutAThreadIsATurn(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{}
	s.cfg.Converse = f.converse
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "P2: why two?"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, post("/api/review/ask", `{"thread":1,"body":"is two not needed for the cache?"}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("ask: %d %s", rec.Code, rec.Body)
	}
	idle(t, s)
	th := getReview(t, s, "/api/review").Review.Threads[0]
	if len(th.Comments) != 2 || th.Comments[1].Author != review.User || th.Comments[1].Body != "is two not needed for the cache?" {
		t.Errorf("the question wasn't kept on the thread: %+v", th.Comments)
	}
	if len(f.prompts) != 1 {
		t.Fatalf("turns: %d", len(f.prompts))
	}
	for _, want := range []string{"#1", "a.txt", "line 2", "P2: why two?", "is two not needed for the cache?", "vibe-review reply --thread 1"} {
		if !strings.Contains(f.prompts[0], want) {
			t.Errorf("the turn's prompt lacks %q:\n%s", want, f.prompts[0])
		}
	}
}

func TestAskingWithoutAQuestionOrWhileTheAgentAnswersIsRefused(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "why?"}, review.Agent)
	if rec := do(t, s, post("/api/review/ask", `{"thread":1,"body":"  "}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("no question: %d", rec.Code)
	}
	if rec := do(t, s, post("/api/review/ask", `{"thread":9,"body":"what?"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("no such thread: %d", rec.Code)
	}
	send(t, s, "/api/conversation/send", `{"prompt":"busy"}`)
	rec := do(t, s, post("/api/review/ask", `{"thread":1,"body":"and now?"}`))
	if rec.Code != http.StatusConflict {
		t.Errorf("while the agent answers: %d", rec.Code)
	}
	close(f.release)
	idle(t, s)
	// Refused, the question isn't left on the thread unasked.
	if th := getReview(t, s, "/api/review").Review.Threads[0]; len(th.Comments) != 1 {
		t.Errorf("thread after refusals: %+v", th.Comments)
	}
}

func TestTheTabGetsAFilesLinesForMoreContext(t *testing.T) {
	s, _ := reviewTabServer(t)
	rec := do(t, s, req{path: "/api/review/file?path=a.txt", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Lines []string `json:"lines"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if strings.Join(body.Lines, "|") != "one|two" {
		t.Errorf("lines: %q", body.Lines)
	}
	if rec := do(t, s, req{path: "/api/review/file?path=../../etc/passwd", headers: authed(nil)}); rec.Code != http.StatusBadRequest {
		t.Errorf("a path out of the folder: %d", rec.Code)
	}
}

// Submitting sends the agent everything of the user's it hasn't seen, as a
// turn, with the earlier comments of a thread for context; then it's sent.
func TestSubmittingSendsTheUsersNewCommentsAsATurn(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{}
	s.cfg.Converse = f.converse
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "P2: why two?"}, review.Agent)
	store.Do(review.Request{Op: "reply", Thread: 1, Body: "because of the cache"}, review.User)
	store.Do(review.Request{Op: "comment", File: "a.txt", Side: "new", Line: 1, Code: "one", Body: "please fix the naming"}, review.User)

	if rec := do(t, s, post("/api/review/submit", `{}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body)
	}
	idle(t, s)
	if len(f.prompts) != 1 {
		t.Fatalf("turns: %d", len(f.prompts))
	}
	p := f.prompts[0]
	for _, want := range []string{
		"#1", "P2: why two?", "because of the cache", // the reply, and what it replies to
		"#2", "a.txt", "line 1", "one", "please fix the naming",
		"vibe-review reply --thread", "vibe-review resolve --thread",
		"Change code only where a comment asks", // the user's rule
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the turn's prompt lacks %q:\n%s", want, p)
		}
	}
	rv := getReview(t, s, "/api/review").Review
	if left := review.Unsent(rv); len(left) != 0 {
		t.Errorf("still unsent after submitting: %v", left)
	}
	if rec := do(t, s, post("/api/review/submit", `{}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("submitting with nothing new: %d", rec.Code)
	}
}

func TestSubmittingWhileTheAgentAnswersLeavesTheCommentsUnsent(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 1, Body: "hmm"}, review.User)
	send(t, s, "/api/conversation/send", `{"prompt":"busy"}`)
	if rec := do(t, s, post("/api/review/submit", `{}`)); rec.Code != http.StatusConflict {
		t.Errorf("while busy: %d", rec.Code)
	}
	close(f.release)
	idle(t, s)
	if rv := getReview(t, s, "/api/review").Review; len(review.Unsent(rv)) != 1 {
		t.Error("a refused submit marked the comment sent")
	}
}

func TestAskingMarksTheQuestionSent(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{}
	s.cfg.Converse = f.converse
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "why?"}, review.Agent)
	do(t, s, post("/api/review/ask", `{"thread":1,"body":"what about the cache?"}`))
	idle(t, s)
	if rv := getReview(t, s, "/api/review").Review; len(review.Unsent(rv)) != 0 {
		t.Error("an asked question is still unsent, so Submit would send it again")
	}
}

// The page's status poll, made from every tab, says when the agent last
// updated the review, so the Review tab can be marked while another is open.
func TestThePagesStatusSaysWhenTheAgentLastUpdatedTheReview(t *testing.T) {
	s, store := reviewTabServer(t)
	reviewAt := func() string {
		t.Helper()
		rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
		var body struct {
			ReviewAt string `json:"reviewAt"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.ReviewAt
	}
	if at := reviewAt(); at != "" {
		t.Errorf("before the agent did anything: %q", at)
	}
	os.MkdirAll(store.Inbox(), 0o755)
	os.WriteFile(filepath.Join(store.Inbox(), "1.json"), []byte(`{"op":"done"}`), 0o644)
	if at := reviewAt(); at == "" {
		t.Error("the agent's update wasn't told")
	}
}

// Ask gives the agent the whole thread, so a reply of the user's written
// there before isn't new to it any more: Submit mustn't send it again.
func TestAskingSendsTheThreadsEarlierReplies(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{}
	s.cfg.Converse = f.converse
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "P2: why two?"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Do(review.Request{Op: "reply", Thread: 1, Body: "first thought"}, review.User); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, post("/api/review/ask", `{"thread":1,"body":"and why?"}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("ask: %d %s", rec.Code, rec.Body)
	}
	rv, _ := store.Load()
	if left := review.Unsent(rv); len(left) != 0 {
		t.Errorf("still unsent after Ask: %+v", left)
	}
}

// While the agent answers an Ask, the tab is told which thread it's on, to
// show it answering there; once the turn has ended, that it isn't any more.
func TestTheTabIsToldWhileTheAgentAnswersAnAsk(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "P2: why two?"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	asking := func() string {
		t.Helper()
		rec := do(t, s, req{path: "/api/review", headers: authed(nil)})
		var body struct {
			Ask *struct {
				Thread    int  `json:"thread"`
				Answering bool `json:"answering"`
			} `json:"ask"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Ask == nil {
			return "none"
		}
		return fmt.Sprintf("#%d answering=%v", body.Ask.Thread, body.Ask.Answering)
	}
	if got := asking(); got != "none" {
		t.Errorf("before asking: %s", got)
	}
	if rec := do(t, s, post("/api/review/ask", `{"thread":1,"body":"why?"}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("ask: %d %s", rec.Code, rec.Body)
	}
	if got := asking(); got != "#1 answering=true" {
		t.Errorf("while answering: %s", got)
	}
	close(f.release)
	idle(t, s)
	if got := asking(); got != "#1 answering=false" {
		t.Errorf("once the turn ended: %s", got)
	}
	// Another prompt and the Ask is over and done with.
	if !s.startTurn("something else") {
		t.Fatal("the next turn didn't start")
	}
	idle(t, s)
	if got := asking(); got != "none" {
		t.Errorf("after another prompt: %s", got)
	}
}

// --- starting and closing reviews from the page ------------------------------------

func TestThePageIsToldWhatReviewsItCanStart(t *testing.T) {
	s, _ := reviewTabServer(t)
	rec := do(t, s, req{path: "/api/review/options", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("options: %d %s", rec.Code, rec.Body)
	}
	var o review.StartOptions
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if !o.Uncommitted || o.DefaultBranch != "" {
		t.Errorf("a change on main: %+v", o)
	}
}

func TestThePageStartsJustTheDiff(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{}
	s.cfg.Converse = f.converse
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`)); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	r, _ := store.Load()
	if r.Started.IsZero() || r.State != "" {
		t.Errorf("review: %+v", r)
	}
	idle(t, s)
	if len(f.prompts) != 0 {
		t.Errorf("the agent was asked: %q", f.prompts)
	}
}

func TestThePageStartsAReviewByTheAgent(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{}
	s.cfg.Converse = f.converse
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	idle(t, s)
	if r, _ := store.Load(); r.State != review.InProgress {
		t.Errorf("review: %+v", r)
	}
	if len(f.prompts) != 1 {
		t.Fatalf("turns: %q", f.prompts)
	}
	for _, want := range []string{"the uncommitted changes", "don't run vibe-review start", "vibe-review done"} {
		if !strings.Contains(f.prompts[0], want) {
			t.Errorf("the prompt lacks %q:\n%s", want, f.prompts[0])
		}
	}
}

func TestABusyAgentStartsNoReview(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	if !s.startTurn("busy") {
		t.Fatal("no first turn")
	}
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusConflict {
		t.Errorf("start while busy: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); !r.Started.IsZero() {
		t.Errorf("started anyway: %+v", r)
	}
}

func TestAReviewAgainstABranchThereIsntIsRefused(t *testing.T) {
	s, _ := reviewTabServer(t)
	rec := do(t, s, post("/api/review/start", `{"kind":"branch"}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "default branch") {
		t.Errorf("start: %d %s", rec.Code, rec.Body)
	}
}

// A review whose diff has nothing left in it is over: it closes itself,
// saying why, and how many comments were still open.
func TestAReviewWithNothingLeftClosesItself(t *testing.T) {
	s, store := reviewTabServer(t)
	do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`))
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "why two?"}, review.Agent)
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed != "" {
		t.Fatalf("closed with changes there: %q", r.Closed)
	}
	gitIn(t, s.cfg.ReviewTab.Workspace, "commit", "-q", "-am", "two")
	r := getReview(t, s, "/api/review?diff=1").Review
	if !strings.Contains(r.Closed, "committed") || !strings.Contains(r.Closed, "1 comment was still open") {
		t.Errorf("closed: %q", r.Closed)
	}
}

// Nothing started: nothing to close.
func TestNoReviewIsntClosed(t *testing.T) {
	s, _ := reviewTabServer(t)
	gitIn(t, s.cfg.ReviewTab.Workspace, "commit", "-q", "-am", "two")
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed != "" {
		t.Errorf("closed: %q", r.Closed)
	}
}

func TestUnpushedCommitsAreReviewedFromThePage(t *testing.T) {
	s, store := reviewTabServer(t)
	dir := s.cfg.ReviewTab.Workspace
	gitIn(t, dir, "branch", "pushed")
	gitIn(t, dir, "branch", "-q", "-u", "pushed")
	gitIn(t, dir, "commit", "-q", "-am", "two")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("not committed\n"), 0o644)
	if rec := do(t, s, post("/api/review/start", `{"kind":"unpushed"}`)); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); r.Ref != "pushed" || !r.Unpushed {
		t.Errorf("review: %+v", r)
	}
	r := getReview(t, s, "/api/review?diff=1")
	if len(r.Diff) != 1 || r.Diff[0].Path != "a.txt" {
		t.Errorf("diff: %+v", r.Diff)
	}
}

// The page's options can be a poll behind: a review of uncommitted changes
// there aren't any of is refused, as the other kinds are.
func TestAReviewOfUncommittedChangesThereArentIsRefused(t *testing.T) {
	s, store := reviewTabServer(t)
	gitIn(t, s.cfg.ReviewTab.Workspace, "commit", "-q", "-am", "two")
	rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no uncommitted changes") {
		t.Errorf("start: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); !r.Started.IsZero() {
		t.Errorf("started anyway: %+v", r)
	}
}

// While the agent is still reviewing, the review stays open though its diff
// has emptied: its comments would go into a closed review, out of sight.
// It closes once the agent has finished.
func TestAReviewTheAgentIsStillAtDoesntCloseItself(t *testing.T) {
	s, _ := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	gitIn(t, s.cfg.ReviewTab.Workspace, "commit", "-q", "-am", "two")
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed != "" {
		t.Errorf("closed while the agent reviews: %q", r.Closed)
	}
	close(f.release)
	idle(t, s)
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed == "" {
		t.Error("still open once the agent had finished")
	}
}

// A busy agent leaves the review as it was: the one before, comments and
// all, not one started for nobody.
func TestABusyAgentLeavesTheLastReviewAsItWas(t *testing.T) {
	s, store := reviewTabServer(t)
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "why two?"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Load()
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	if !s.startTurn("busy") {
		t.Fatal("no first turn")
	}
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusConflict {
		t.Fatalf("start while busy: %d %s", rec.Code, rec.Body)
	}
	after, _ := store.Load()
	if len(after.Threads) != 1 || !after.Started.Equal(before.Started) || after.State != before.State {
		t.Errorf("before %+v\nafter %+v", before, after)
	}
}

// When lines come or go above a comment, it follows its code there; if
// the code is gone, it stays where it was.
func TestACommentFollowsItsCodeWhenTheDiffIsWorkedOut(t *testing.T) {
	s, store := reviewTabServer(t)
	dir := s.cfg.ReviewTab.Workspace
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Code: "two", Body: "why two?"}, review.Agent)
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 1, Code: "gone soon", Body: "?"}, review.Agent)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("zero\nhalf\none\ntwo\n"), 0o644)
	threads := getReview(t, s, "/api/review?diff=1").Review.Threads
	if th := threads[0]; th.Line != 4 || th.EndLine != 4 || th.MovedFrom != 2 {
		t.Errorf("followed: %+v", th)
	}
	if th := threads[1]; th.Line != 1 || th.MovedFrom != 0 {
		t.Errorf("its code gone, it stays: %+v", th)
	}
	if th, _ := store.Load(); th.Threads[0].Line != 4 {
		t.Error("the move wasn't kept")
	}
}

// A comment the agent has seen, edited since, goes to it again saying what
// it said before, so the agent knows what changed.
func TestAnEditedCommentIsSentSayingWhatChanged(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{}
	s.cfg.Converse = f.converse
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "P2: why two?"}, review.Agent)
	store.Do(review.Request{Op: "reply", Thread: 1, Body: "it is needed"}, review.User)
	rv, _ := store.Load()
	store.MarkSent(review.Unsent(rv))
	rv, _ = store.Load()
	said := rv.Threads[0].Comments[1].At
	if _, err := store.Do(review.Request{Op: "edit", Thread: 1, At: said, Body: "it is not needed after all"}, review.User); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, post("/api/review/submit", `{}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body)
	}
	idle(t, s)
	if len(f.prompts) != 1 {
		t.Fatalf("turns: %q", f.prompts)
	}
	for _, want := range []string{"edited", "it is needed", "it is not needed after all"} {
		if !strings.Contains(f.prompts[0], want) {
			t.Errorf("the prompt lacks %q:\n%s", want, f.prompts[0])
		}
	}
}

// The tab asks for a file coloured, on either side of its diff: the whole
// file, as the review has it.
func TestTheTabGetsAFileColoured(t *testing.T) {
	s, _ := reviewTabServer(t)
	dir := s.cfg.ReviewTab.Workspace
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package old\n"), 0o644)
	gitIn(t, dir, "add", "b.go")
	gitIn(t, dir, "commit", "-q", "-m", "b")
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package changed\n\nfunc f() {}\n"), 0o644)
	coloured := func(query string) (int, review.Highlighted) {
		t.Helper()
		rec := do(t, s, req{path: "/api/review/highlight?" + query, headers: authed(nil)})
		var h review.Highlighted
		json.Unmarshal(rec.Body.Bytes(), &h)
		return rec.Code, h
	}
	code, h := coloured("path=b.go&side=new")
	if code != http.StatusOK || h.Language != "Go" || len(h.Lines) != 3 {
		t.Errorf("new side: %d %+v", code, h)
	}
	code, h = coloured("path=b.go&side=old")
	if code != http.StatusOK || len(h.Lines) != 1 || h.Lines[0][len(h.Lines[0])-1].Text != "old" {
		t.Errorf("old side: %d %+v", code, h)
	}
	if code, _ := coloured("path=../secret.go&side=new"); code != http.StatusBadRequest {
		t.Errorf("outside the folder: %d", code)
	}
}

// A file whose colouring never ends (Chroma's JSONata lexer, on a comment
// that isn't closed) is shown uncoloured once its time is up, not waited on
// for good.
func TestAFileThatTakesTooLongToColourIsShownWithout(t *testing.T) {
	old := colourWithin
	colourWithin = time.Second
	t.Cleanup(func() { colourWithin = old })
	s, _ := reviewTabServer(t)
	os.WriteFile(filepath.Join(s.cfg.ReviewTab.Workspace, "x.jsonata"), []byte("/*"), 0o644)
	began := time.Now()
	rec := do(t, s, req{path: "/api/review/highlight?path=x.jsonata&side=new", headers: authed(nil)})
	var h review.Highlighted
	json.Unmarshal(rec.Body.Bytes(), &h)
	if rec.Code != http.StatusOK || h.Lines != nil || !strings.Contains(h.Skipped, "too long") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("took %s", took)
	}
}

// Files that take too long to colour may have been built to: past a few,
// colouring is turned off for good (this vibe), every file shown plain,
// saying why, rather than spending a core on each.
func TestColouringIsTurnedOffOnceFilesKeepTakingTooLong(t *testing.T) {
	old := colourWithin
	t.Cleanup(func() { colourWithin = old })
	s, _ := reviewTabServer(t)
	dir := s.cfg.ReviewTab.Workspace
	os.WriteFile(filepath.Join(dir, "x.jsonata"), []byte("/*"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644)
	colour := func(path string) review.Highlighted {
		t.Helper()
		rec := do(t, s, req{path: "/api/review/highlight?side=new&path=" + path, headers: authed(nil)})
		var h review.Highlighted
		json.Unmarshal(rec.Body.Bytes(), &h)
		return h
	}
	// A file that colours is held to the usual time, however slow the
	// machine (a race-instrumented helper starts slowly); only the file
	// that never finishes is held to a short one, to keep the test quick.
	if h := colour("b.go"); h.Language != "Go" {
		t.Fatalf("before: %+v", h)
	}
	colourWithin = 200 * time.Millisecond
	for i := 0; i < colourGiveUpAfter; i++ {
		if h := colour("x.jsonata"); !strings.Contains(h.Skipped, "too long") {
			t.Fatalf("try %d: %+v", i+1, h)
		}
	}
	h := colour("b.go")
	if h.Lines != nil || !strings.Contains(h.Skipped, "turned off") {
		t.Errorf("after %d took too long: %+v", colourGiveUpAfter, h)
	}
}

// No more than so many files are coloured at once, however many the page
// asks for together: with two at a time, four that each take all their
// time take two turns.
func TestOnlyAFewFilesAreColouredAtOnce(t *testing.T) {
	old := colourWithin
	colourWithin = 500 * time.Millisecond
	t.Cleanup(func() { colourWithin = old })
	s, _ := reviewTabServer(t)
	s.colours = newColourer(2)
	os.WriteFile(filepath.Join(s.cfg.ReviewTab.Workspace, "x.jsonata"), []byte("/*"), 0o644)
	began := time.Now()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { do(t, s, req{path: "/api/review/highlight?side=new&path=x.jsonata", headers: authed(nil)}) })
	}
	wg.Wait()
	if took := time.Since(began); took < 2*colourWithin {
		t.Errorf("4 files that each took %s were coloured in %s: more than 2 at once", colourWithin, took)
	}
}

// Colouring takes half the machine's cores at most, and always has one.
func TestColouringTakesHalfTheCores(t *testing.T) {
	for cpus, want := range map[int]int{1: 1, 2: 1, 3: 1, 4: 2, 16: 8} {
		if got := halfOf(cpus); got != want {
			t.Errorf("halfOf(%d) = %d, want %d", cpus, got, want)
		}
	}
	if colourAtOnce != halfOf(runtime.NumCPU()) {
		t.Errorf("colourAtOnce = %d on %d cores", colourAtOnce, runtime.NumCPU())
	}
}

// A sandbox's folder may be a repository's subfolder: git's paths are the
// repository's, so that's what the tab works in, files read where they are.
func TestASubfoldersReviewIsOfItsRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	top := t.TempDir()
	gitIn(t, top, "init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(top, "sub"), 0o755)
	os.WriteFile(filepath.Join(top, "sub", "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, top, "add", "-A")
	gitIn(t, top, "commit", "-q", "-m", "start")
	os.WriteFile(filepath.Join(top, "sub", "a.txt"), []byte("one\ntwo\n"), 0o644)
	os.WriteFile(filepath.Join(top, "sub", "new.txt"), []byte("fresh\n"), 0o644)
	s := New(Config{Sandbox: "box", Token: token, Port: testPort, ReviewTab: &ReviewTab{Workspace: filepath.Join(top, "sub"), Store: review.NewStore(t.TempDir())}})

	r := getReview(t, s, "/api/review?diff=1")
	var got []string
	for _, f := range r.Diff {
		got = append(got, fmt.Sprintf("%s %s %d", f.Status, f.Path, f.Lines))
	}
	if strings.Join(got, ", ") != "modified sub/a.txt 2, added sub/new.txt 1" {
		t.Errorf("diff: %v (error %q)", got, r.Error)
	}
	rec := do(t, s, req{path: "/api/review/file?path=sub/a.txt", headers: authed(nil)})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"two"`) {
		t.Errorf("file: %d %s", rec.Code, rec.Body)
	}
}

// Whatever the review, the agent answering on it (a Submit, an Ask) keeps
// it open: what it says would go into a closed review, out of sight.
func TestAReviewJustShownDoesntCloseItselfWhileTheAgentAnswers(t *testing.T) {
	s, _ := reviewTabServer(t)
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`)); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	if !s.startTurn("answering a Submit") {
		t.Fatal("no turn")
	}
	gitIn(t, s.cfg.ReviewTab.Workspace, "commit", "-q", "-am", "two")
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed != "" {
		t.Errorf("closed while the agent answers: %q", r.Closed)
	}
	close(f.release)
	idle(t, s)
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed == "" {
		t.Error("still open once the agent had finished")
	}
}

// A review the user reopens is theirs to read: it doesn't close itself
// again.
func TestAReopenedReviewStaysOpen(t *testing.T) {
	s, store := reviewTabServer(t)
	do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`))
	gitIn(t, s.cfg.ReviewTab.Workspace, "commit", "-q", "-am", "two")
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed == "" {
		t.Fatal("didn't close itself")
	}
	if _, err := store.Do(review.Request{Op: "reopen-review"}, review.User); err != nil {
		t.Fatal(err)
	}
	if r := getReview(t, s, "/api/review?diff=1").Review; r.Closed != "" {
		t.Errorf("closed itself again: %q", r.Closed)
	}
}

// A request the review refuses is the page's to put right (400); the review
// failing to be read or kept is vibe's (500), and isn't put as the user's.
func TestAReviewThatCantBeReadIsntTheRequestsFault(t *testing.T) {
	s, store := reviewTabServer(t)
	if rec := do(t, s, post("/api/review/do", `{"op":"reply","thread":99,"body":"x"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("a reply to no thread: %d %s", rec.Code, rec.Body)
	}
	store.Do(review.Request{Op: "general", Body: "x"}, review.Agent)
	if err := os.WriteFile(filepath.Join(filepath.Dir(store.Inbox()), "review.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, post("/api/review/do", `{"op":"reply","thread":1,"body":"x"}`)); rec.Code != http.StatusInternalServerError {
		t.Errorf("with the review unreadable: %d %s", rec.Code, rec.Body)
	}
}

// The agent's requests are taken in whether or not a page is open: vibe
// takes in the inbox itself, every so often.
func TestTheInboxIsTakenInWithNoPageOpen(t *testing.T) {
	s, store := reviewTabServer(t)
	os.MkdirAll(store.Inbox(), 0o755)
	os.WriteFile(filepath.Join(store.Inbox(), "1.json"), []byte(`{"op":"general","body":"seen without a page"}`), 0o644)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		s.takeInbox(done, 10*time.Millisecond)
	}()
	// Until it has stopped it may be writing into the folders the test's cleanup removes.
	defer func() {
		close(done)
		<-stopped
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, _ := store.Load(); len(r.Threads) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the request was never taken in")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The page can leave whitespace changes out; a review whose changes are all
// whitespace still isn't over.
func TestWhitespaceChangesCanBeLeftOutAndTheReviewStays(t *testing.T) {
	s, _ := reviewTabServer(t)
	dir := s.cfg.ReviewTab.Workspace
	gitIn(t, dir, "commit", "-q", "-am", "two")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n  two\n"), 0o644)
	do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`))
	r := getReview(t, s, "/api/review?diff=1&whitespace=ignore")
	if len(r.Diff) != 0 || r.Review.Closed != "" {
		t.Errorf("diff %+v, closed %q", r.Diff, r.Review.Closed)
	}
	if r := getReview(t, s, "/api/review?diff=1"); len(r.Diff) != 1 {
		t.Errorf("with whitespace: %+v", r.Diff)
	}
}

// A served page takes in the agent's requests by itself, page open or not.
func TestAServedPageTakesInTheInbox(t *testing.T) {
	was := reviewTakeEvery
	reviewTakeEvery = 10 * time.Millisecond
	defer func() { reviewTakeEvery = was }()
	store := review.NewStore(t.TempDir())
	s, err := Start(Config{Sandbox: "box", Token: token, Port: 0, ReviewTab: &ReviewTab{Workspace: t.TempDir(), Store: store}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	os.MkdirAll(store.Inbox(), 0o755)
	os.WriteFile(filepath.Join(store.Inbox(), "1.json"), []byte(`{"op":"general","body":"taken in"}`), 0o644)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, _ := store.Load(); len(r.Threads) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the request was never taken in")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A review the page starts for the agent is started only once the agent's
// turn is the page's: a busy agent leaves the review untouched (nothing to
// put back, so nothing the agent drops meanwhile is lost); and a review
// that can't be started gives the turn up.
func TestAReviewThatCantBeStartedGivesTheTurnUp(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	// The review reads, but the last one can't be kept: the start fails
	// once the turn is claimed.
	store.Do(review.Request{Op: "general", Body: "kept"}, review.Agent)
	os.WriteFile(filepath.Join(filepath.Dir(store.Inbox()), "history"), []byte("not a folder"), 0o600)
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusInternalServerError {
		t.Errorf("start: %d %s", rec.Code, rec.Body)
	}
	if s.convo.working() {
		t.Error("the turn was left claimed")
	}
	if r := s.convo.latestReceipt(); r == nil || r["state"] != "undelivered" {
		t.Errorf("the prompt's receipt: %v, want it marked not delivered", r)
	}
	if len(f.prompts) != 0 {
		t.Errorf("the agent was asked anyway: %q", f.prompts)
	}
}

// The inbox taken in with no page open failing isn't silent: it's said in
// vibe's own output.
func TestTheInboxFailingToBeTakenInIsSaid(t *testing.T) {
	s, store := reviewTabServer(t)
	said := make(chan string, 10)
	s.cfg.Log = func(line string) { said <- line }
	os.WriteFile(filepath.Join(filepath.Dir(store.Inbox()), "review.json"), []byte("{broken"), 0o600)
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() { s.takeInbox(done, 5*time.Millisecond); close(ended) }()
	select {
	case line := <-said:
		if !strings.Contains(line, "review") {
			t.Errorf("said %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Error("nothing was said of the inbox failing")
	}
	close(done)
	<-ended
}

// A review that can't be read refuses a start before the agent's turn is
// claimed: what the agent left running isn't ended for nothing.
func TestAnUnreadableReviewRefusesAStartBeforeClaimingTheTurn(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	os.WriteFile(filepath.Join(filepath.Dir(store.Inbox()), "review.json"), []byte("{broken"), 0o600)
	rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "review-unreadable") {
		t.Errorf("start: %d %s", rec.Code, rec.Body)
	}
	if r := s.convo.latestReceipt(); r != nil {
		t.Errorf("a turn was claimed: %v", r)
	}
}

// An unreadable review is said to be, so the page can offer to start
// afresh; doing so sets it aside and begins anew.
func TestAnUnreadableReviewCanBeStartedAfreshFromThePage(t *testing.T) {
	s, store := reviewTabServer(t)
	os.WriteFile(filepath.Join(filepath.Dir(store.Inbox()), "review.json"), []byte("{broken"), 0o600)
	rec := do(t, s, req{path: "/api/review", headers: authed(nil)})
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"review-unreadable"`) {
		t.Fatalf("review: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/review/afresh", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("afresh: %d %s", rec.Code, rec.Body)
	}
	if r := getReview(t, s, "/api/review"); r.Review.Threads == nil {
		t.Errorf("review after: %+v", r.Review)
	}
}

// --- closing a review the agent is at work on ---------------------------------------

// agentAtWork is whether the Review tab is told the agent is at work on
// the review.
func agentAtWork(t *testing.T, s *Server) bool {
	t.Helper()
	rec := do(t, s, req{path: "/api/review", headers: authed(nil)})
	var body struct {
		AgentAtWork bool `json:"agentAtWork"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.AgentAtWork
}

// lastDone is how the latest turn ended: "stopped", or "" when it finished.
func lastDone(t *testing.T, s *Server) string {
	t.Helper()
	p := idle(t, s)
	for i := len(p.Events) - 1; i >= 0; i-- {
		if p.Events[i].Kind == "done" {
			return p.Events[i].Text
		}
	}
	t.Fatal("no turn has ended")
	return ""
}

func TestClosingAReviewTheAgentIsStillWritingStopsItAndDropsTheReview(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "half a review"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if !agentAtWork(t, s) {
		t.Fatal("the tab isn't told the agent is at work on the review")
	}
	if rec := do(t, s, post("/api/review/close", `{"stop":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body)
	}
	if got := lastDone(t, s); got != "stopped" {
		t.Errorf("the agent's turn ended %q, not stopped", got)
	}
	r, _ := store.Load()
	if !r.Started.IsZero() || len(r.Threads) != 0 {
		t.Errorf("the half-written review is still there: %+v", r)
	}
	if !strings.Contains(r.Closed, "dropped") {
		t.Errorf("why it closed doesn't say it was dropped: %q", r.Closed)
	}
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 1, Body: "too late"}, review.Agent); err == nil {
		t.Error("the agent could still comment on the dropped review")
	}
}

func TestClosingAReviewTheAgentIsAtWithoutAgreeingToStopItIsRefused(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	rec := do(t, s, post("/api/review/close", `{}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("close without agreeing to stop the agent: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "stops the agent") {
		t.Errorf("the refusal doesn't say why: %s", rec.Body)
	}
	if r, _ := store.Load(); r.Closed != "" || r.State != review.InProgress {
		t.Errorf("the review changed: %+v", r)
	}
	if !s.convo.working() {
		t.Error("the agent was stopped")
	}
}

// doneWithOpenThread is a finished review with one thread still open.
func doneWithOpenThread(t *testing.T, s *Server, store *review.Store) {
	t.Helper()
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`)); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	for _, r := range []review.Request{
		{Op: "comment", File: "a.txt", Line: 2, Body: "P2: why two?"},
		{Op: "done"},
	} {
		if _, err := store.Do(r, review.Agent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Do(review.Request{Op: "reply", Thread: 1, Body: "please fix"}, review.User); err != nil {
		t.Fatal(err)
	}
}

func TestClosingAReviewWhileTheAgentFixesItsCommentsStopsItAndKeepsTheReview(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	doneWithOpenThread(t, s, store)
	if rec := do(t, s, post("/api/review/submit", `{}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body)
	}
	if !agentAtWork(t, s) {
		t.Fatal("the tab isn't told the agent is at work on the review")
	}
	if rec := do(t, s, post("/api/review/close", `{"stop":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body)
	}
	if got := lastDone(t, s); got != "stopped" {
		t.Errorf("the agent's turn ended %q, not stopped", got)
	}
	r, _ := store.Load()
	if r.Closed == "" || len(r.Threads) != 1 {
		t.Errorf("not closed and kept to reopen: %+v", r)
	}
}

func TestClosingAReviewWithNothingOpenLetsTheAgentFinish(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	doneWithOpenThread(t, s, store)
	if rec := do(t, s, post("/api/review/submit", `{}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body)
	}
	if _, err := store.Do(review.Request{Op: "resolve", Thread: 1, Body: "fixed"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, post("/api/review/close", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); r.Closed == "" {
		t.Error("not closed")
	}
	if !s.convo.working() {
		t.Error("the agent was stopped, with nothing left open for it")
	}
	close(f.release)
	if got := lastDone(t, s); got != "" {
		t.Errorf("the agent's turn ended %q", got)
	}
}

func TestClosingAReviewLeavesATurnThatIsntItsAlone(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	doneWithOpenThread(t, s, store)
	send(t, s, "/api/conversation/send", `{"prompt":"something else"}`)
	if agentAtWork(t, s) {
		t.Error("the tab is told the agent is at work on the review, answering something else")
	}
	if rec := do(t, s, post("/api/review/close", `{"stop":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); r.Closed == "" || len(r.Threads) != 1 {
		t.Errorf("not closed and kept: %+v", r)
	}
	if !s.convo.working() {
		t.Error("closing the review stopped a turn that wasn't the review's")
	}
	close(f.release)
	if got := lastDone(t, s); got != "" {
		t.Errorf("the other turn ended %q", got)
	}
}

// --- stopping the agent while it writes a review (docs/plan-stop-ends-review.md) --------

func startWritingReview(t *testing.T, s *Server) *fakeAgent {
	t.Helper()
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted","agent":true}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	return f
}

func TestStoppingTheAgentWhileItWritesAReviewAsksFirstAndStopsNothing(t *testing.T) {
	s, store := reviewTabServer(t)
	f := startWritingReview(t, s)
	defer close(f.release)
	rec := do(t, s, post("/api/conversation/stop", `{}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "review-in-progress" || !strings.Contains(body.Error, "review") {
		t.Errorf("the refusal: %+v", body)
	}
	if !s.convo.working() {
		t.Error("the agent was stopped by a stop that was refused")
	}
	if r, _ := store.Load(); r.State != review.InProgress || r.Closed != "" {
		t.Errorf("the review changed: %+v", r)
	}
}

func TestConfirmingTheStopStopsTheAgentAndDropsTheReview(t *testing.T) {
	s, store := reviewTabServer(t)
	f := startWritingReview(t, s)
	defer close(f.release)
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "half a review"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, post("/api/conversation/stop", `{"confirmed":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if got := lastDone(t, s); got != "stopped" {
		t.Errorf("the agent's turn ended %q, not stopped", got)
	}
	r, _ := store.Load()
	if !r.Started.IsZero() || len(r.Threads) != 0 {
		t.Errorf("the half-written review is still there: %+v", r)
	}
	if !strings.Contains(r.Closed, "dropped") || !strings.Contains(r.Closed, "stopped") {
		t.Errorf("why it closed doesn't say the stop dropped it: %q", r.Closed)
	}
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 1, Body: "too late"}, review.Agent); err == nil {
		t.Error("the agent could still comment on the dropped review")
	}
}

func TestAStopOfATurnTheReviewDidNotBeginIsNotAskedAbout(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	send(t, s, "/api/conversation/send", `{"prompt":"something else"}`)
	rec := do(t, s, post("/api/conversation/stop", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if got := lastDone(t, s); got != "stopped" {
		t.Errorf("the agent's turn ended %q, not stopped", got)
	}
	if r, _ := store.Load(); r.Closed != "" {
		t.Errorf("a review was closed by a stop of another turn: %+v", r)
	}
}

func TestAStopWhileTheAgentFixesCommentsOfAFinishedReviewIsNotAskedAboutAndKeepsTheReview(t *testing.T) {
	s, store := reviewTabServer(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	doneWithOpenThread(t, s, store)
	if rec := do(t, s, post("/api/review/submit", `{}`)); rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/conversation/stop", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); r.Closed != "" || len(r.Threads) != 1 || r.State != review.Done {
		t.Errorf("a finished review was changed by a stop: %+v", r)
	}
}

func TestAStopWithNoReviewTabIsAsItWas(t *testing.T) {
	s := newServer(&fakeSbx{})
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	send(t, s, "/api/conversation/send", `{"prompt":"hi"}`)
	if rec := do(t, s, post("/api/conversation/stop", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
}

// A review "in progress" that nothing vibe can see is at work on (the agent crashed, the sandbox stopped, it forgot to say it was
// done, or it is writing from a terminal session) is told to the tab as quiet, for how many minutes. Vibe can't tell those apart, so
// it drops nothing by itself and closing asks first, as for a review being written: only a confirmed close drops it.
func TestAReviewInProgressThatNothingIsWritingIsToldAsQuietAndOnlyAConfirmedCloseDropsIt(t *testing.T) {
	s, store, clock := tabServerWithClock(t)
	f := startWritingReview(t, s)
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "half a review"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	idle(t, s)
	clock.advance(5*time.Minute + 30*time.Second)
	if r, _ := store.Load(); r.State != review.InProgress {
		t.Fatalf("the review is %q: it should still be in progress", r.State)
	}
	if got := getReview(t, s, "/api/review"); got.QuietMinutes != 5 {
		t.Fatalf("the tab is told it has been quiet for %d minutes, want 5", got.QuietMinutes)
	}
	if rec := do(t, s, post("/api/review/close", `{"stop":false}`)); rec.Code != http.StatusConflict {
		t.Fatalf("unconfirmed close: %d %s: should ask first", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); r.Started.IsZero() || r.Closed != "" {
		t.Fatalf("an unconfirmed close changed the review: %+v", r)
	}
	if rec := do(t, s, post("/api/review/close", `{"stop":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("confirmed close: %d %s", rec.Code, rec.Body)
	}
	r, _ := store.Load()
	if !r.Started.IsZero() || len(r.Threads) != 0 {
		t.Errorf("half a review was kept: %+v", r)
	}
	if !strings.Contains(r.Closed, "dropped") || strings.Contains(r.Closed, "agent stopped") {
		t.Errorf("why it closed: %q: should say it was dropped, and not claim the agent stopped", r.Closed)
	}
}

// --- a review the agent started itself: nothing of the page's began its turn -----------------

// agentStartedReview is a review the agent made in an ordinary turn, as `vibe-review start` does: in progress, with a turn running
// that the page didn't begin for it.
func agentStartedReview(t *testing.T) (*Server, *review.Store, *reviewClock, *fakeAgent) {
	t.Helper()
	s, store, clock := tabServerWithClock(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	send(t, s, "/api/conversation/send", `{"prompt":"please review my changes"}`)
	if _, err := store.Do(review.Request{Op: "start", Note: "x"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "so far"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	return s, store, clock, f
}

func TestAReviewTheAgentStartedItselfIsNotQuietWhileItsTurnRuns(t *testing.T) {
	s, _, clock, f := agentStartedReview(t)
	defer close(f.release)
	clock.advance(reviewQuiet + time.Hour) // however long since it was touched: a turn is running
	if got := getReview(t, s, "/api/review"); got.QuietMinutes != 0 {
		t.Error("a review being written by a running turn was called quiet")
	}
}

func TestStoppingTheTurnOfAReviewTheAgentStartedItselfAsksFirstAndConfirmedDropsIt(t *testing.T) {
	s, store, _, f := agentStartedReview(t)
	defer close(f.release)
	rec := do(t, s, post("/api/conversation/stop", `{}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "review-in-progress") {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if !s.convo.working() {
		t.Fatal("the turn was stopped by a stop that was refused")
	}
	if rec := do(t, s, post("/api/conversation/stop", `{"confirmed":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("confirmed stop: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); !r.Started.IsZero() || !strings.Contains(r.Closed, "dropped") {
		t.Errorf("the half-written review was kept: %+v", r)
	}
}

func TestClosingAReviewTheAgentStartedItselfWhileItsTurnRunsAsksToStopTheAgent(t *testing.T) {
	s, store, _, f := agentStartedReview(t)
	defer close(f.release)
	rec := do(t, s, post("/api/review/close", `{}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "stops the agent") {
		t.Fatalf("close without agreeing to stop: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); r.Closed != "" {
		t.Errorf("the review was closed: %+v", r)
	}
	if rec := do(t, s, post("/api/review/close", `{"stop":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); !r.Started.IsZero() || !strings.Contains(r.Closed, "dropped") {
		t.Errorf("not dropped: %+v", r)
	}
}

func TestAReviewTheAgentIsWritingItselfIsNotClosedByABranchSwitchWhileItsTurnRuns(t *testing.T) {
	s, store, _, f := agentStartedReview(t)
	reviewIsClosed(t, s) // the first look, which notes the branch the agent started it on
	checkout(t, s, "-b", "feat/other")
	if closed, _ := reviewIsClosed(t, s); closed != "" {
		t.Fatalf("closed under the agent: %q", closed)
	}
	close(f.release)
	idle(t, s)
	store.Do(review.Request{Op: "done"}, review.Agent)
	if closed, _ := reviewIsClosed(t, s); closed == "" {
		t.Error("not closed once the agent had finished")
	}
}

// An old review in progress, with an unrelated prompt now running: that turn is not writing it, so stopping it is not asked about.
func TestStoppingAnUnrelatedTurnIsNotAskedAboutBecauseAnOldReviewIsInProgress(t *testing.T) {
	s, store, clock := tabServerWithClock(t)
	if _, err := store.Do(review.Request{Op: "start", Note: "x"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	clock.advance(reviewQuiet + time.Hour)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	send(t, s, "/api/conversation/send", `{"prompt":"something else"}`)
	if rec := do(t, s, post("/api/conversation/stop", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); r.Closed != "" || r.State != review.InProgress {
		t.Errorf("the old review was changed by a stop of another turn: %+v", r)
	}
}

// A review the agent is writing from a terminal session shows as no turn here: it has touched it lately, so it is not quiet.
func TestAReviewTheAgentTouchedLatelyIsNotQuietEvenWithNoTurnRunning(t *testing.T) {
	s, store, clock := tabServerWithClock(t)
	if _, err := store.Do(review.Request{Op: "start", Note: "x"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	if got := getReview(t, s, "/api/review"); got.QuietMinutes != 0 {
		t.Error("called quiet the moment it was started")
	}
	clock.advance(reviewQuiet - time.Minute)
	if got := getReview(t, s, "/api/review"); got.QuietMinutes != 0 {
		t.Error("called quiet before the quiet time")
	}
	clock.advance(2 * time.Minute)
	if got := getReview(t, s, "/api/review"); got.QuietMinutes == 0 {
		t.Error("not called quiet once it had been left alone for the quiet time")
	}
}

// --- a confirmed stop says what really happened ----------------------------------------------

func TestAConfirmedStopAfterTheAgentHasFinishedTheReviewKeepsItAndSaysNothingWasStopped(t *testing.T) {
	s, store, _ := tabServerWithClock(t)
	f := &fakeAgent{release: make(chan struct{})}
	s.cfg.Converse = f.converse
	defer close(f.release)
	doneWithOpenThread(t, s, store)
	rec := do(t, s, post("/api/conversation/stop", `{"confirmed":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Stopped bool `json:"stopped"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Stopped {
		t.Error("it said it stopped something: nothing was running")
	}
	if r, _ := store.Load(); r.Closed != "" || len(r.Threads) != 1 || r.State != review.Done {
		t.Errorf("a finished review was changed: %+v", r)
	}
}

// --- the branch is not looked up on every poll -------------------------------------------------

func TestTheBranchIsLookedUpAtMostEveryFewSecondsAndAlwaysWhenTheDiffIsAsked(t *testing.T) {
	s, _, clock := tabServerWithClock(t)
	startReviewOnMain(t, s)
	var looks int
	was := s.branchLookup
	s.branchLookup = func(ctx context.Context, dir string) (string, error) { looks++; return was(ctx, dir) }
	for i := 0; i < 5; i++ {
		getReview(t, s, "/api/review")
	}
	if looks > 1 {
		t.Errorf("%d lookups for 5 polls within a moment", looks)
	}
	before := looks
	getReview(t, s, "/api/review?diff=1")
	if looks != before+1 {
		t.Errorf("a diff was asked for and the branch was not looked up: %d lookups, was %d", looks, before)
	}
	clock.advance(branchLookEvery + time.Second)
	getReview(t, s, "/api/review")
	if looks != before+2 {
		t.Errorf("not looked up again after the time: %d lookups", looks)
	}
}

// --- a review closes itself when the folder switches branch -------------------------------

// reviewClock stands in for the server's clock, to be moved on by tests.
type reviewClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *reviewClock) read() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *reviewClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func withReviewClock(s *Server) *reviewClock {
	c := &reviewClock{now: time.Now()}
	s.clock = c.read
	return c
}

// reviewIsClosed looks at the review as the tab does when it polls, some seconds after the last look: the branch is not looked up more
// than every few seconds.
func reviewIsClosed(t *testing.T, s *Server) (closed string, rv review.Review) {
	t.Helper()
	if c, ok := reviewClocks.Load(s); ok {
		c.(*reviewClock).advance(10 * time.Second)
	}
	r := getReview(t, s, "/api/review")
	return r.Review.Closed, r.Review
}

// reviewClocks are the clocks of the servers a test gave one (see tabServerWithClock).
var reviewClocks sync.Map

func tabServerWithClock(t *testing.T) (*Server, *review.Store, *reviewClock) {
	t.Helper()
	s, store := reviewTabServer(t)
	c := withReviewClock(s)
	reviewClocks.Store(s, c)
	t.Cleanup(func() { reviewClocks.Delete(s) })
	return s, store, c
}

func checkout(t *testing.T, s *Server, args ...string) {
	t.Helper()
	gitIn(t, s.cfg.ReviewTab.Workspace, append([]string{"checkout", "-q"}, args...)...)
}

func startReviewOnMain(t *testing.T, s *Server) {
	t.Helper()
	if rec := do(t, s, post("/api/review/start", `{"kind":"uncommitted"}`)); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
}

func TestAReviewIsClosedByTheFolderSwitchingBranchAndSaysWhichBranchesAndIsKept(t *testing.T) {
	s, store, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Body: "keep me"}, review.Agent)
	if closed, _ := reviewIsClosed(t, s); closed != "" {
		t.Fatalf("closed before anything changed: %q", closed)
	}
	checkout(t, s, "-b", "feat/other")
	closed, rv := reviewIsClosed(t, s)
	for _, want := range []string{"main", "feat/other", "was closed when the branch"} {
		if !strings.Contains(closed, want) {
			t.Errorf("why it closed lacks %q: %q", want, closed)
		}
	}
	if len(rv.Threads) != 1 {
		t.Errorf("the review was not kept: %+v", rv)
	}
}

func TestAReviewStaysOpenWhileTheFolderStaysOnItsBranch(t *testing.T) {
	s, _, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	for i := 0; i < 3; i++ {
		if closed, _ := reviewIsClosed(t, s); closed != "" {
			t.Fatalf("closed on look %d: %q", i, closed)
		}
	}
}

func TestComingBackToTheBranchDoesNotReopenTheReview(t *testing.T) {
	s, _, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	checkout(t, s, "-b", "feat/other")
	reviewIsClosed(t, s)
	checkout(t, s, "main")
	if closed, _ := reviewIsClosed(t, s); closed == "" {
		t.Error("the review reopened itself on the branch it was of")
	}
}

func TestTheBranchSwitchMessageStaysTrueOnceTheFolderIsBackOnTheBranch(t *testing.T) {
	s, _, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	checkout(t, s, "-b", "feat/other")
	closed, _ := reviewIsClosed(t, s)
	want := "The last review was closed when the branch for this workspace switched to 'feat/other'. The review was of 'main'. It is kept: reopen it whenever you like."
	if closed != want {
		t.Errorf("closed %q, want %q", closed, want)
	}
}

func TestADetachedHeadIsLeavingTheBranch(t *testing.T) {
	s, _, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	checkout(t, s, "--detach")
	closed, _ := reviewIsClosed(t, s)
	if closed == "" || !strings.Contains(closed, "detached") {
		t.Errorf("closed %q: should say the folder is on a detached head", closed)
	}
}

func TestAReviewIsNotClosedByTheBranchWhileTheAgentIsWritingItButIsOnceItHasFinished(t *testing.T) {
	s, store, _ := tabServerWithClock(t)
	f := startWritingReview(t, s)
	checkout(t, s, "-b", "feat/other")
	if closed, _ := reviewIsClosed(t, s); closed != "" {
		t.Fatalf("closed while the agent was writing it: %q", closed)
	}
	close(f.release)
	idle(t, s)
	store.Do(review.Request{Op: "done"}, review.Agent)
	if closed, _ := reviewIsClosed(t, s); closed == "" {
		t.Error("not closed once the agent had finished")
	}
}

func TestReopeningAReviewTiesItToTheBranchItIsReopenedOnAndLeavingThatClosesItAgain(t *testing.T) {
	s, _, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	checkout(t, s, "-b", "feat/other")
	reviewIsClosed(t, s)
	if rec := do(t, s, post("/api/review/do", `{"op":"reopen-review"}`)); rec.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", rec.Code, rec.Body)
	}
	for i := 0; i < 2; i++ {
		if closed, _ := reviewIsClosed(t, s); closed != "" {
			t.Fatalf("closed on the branch it was reopened on: %q", closed)
		}
	}
	checkout(t, s, "main")
	if closed, _ := reviewIsClosed(t, s); closed == "" {
		t.Error("not closed on leaving the branch it was reopened on")
	}
}

func TestAReviewReopenedWhenTheBranchCantBeReadIsNotClosedAgainByTheBranchItWasClosedFor(t *testing.T) {
	s, _, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	checkout(t, s, "-b", "feat/other")
	reviewIsClosed(t, s)
	real := s.branchLookup
	s.branchLookup = func(context.Context, string) (string, error) { return "", errors.New("git is not answering") }
	if rec := do(t, s, post("/api/review/do", `{"op":"reopen-review"}`)); rec.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", rec.Code, rec.Body)
	}
	if closed, _ := reviewIsClosed(t, s); closed != "" {
		t.Fatalf("closed while the branch can't be read: %q", closed)
	}
	s.branchLookup = real
	if closed, rv := reviewIsClosed(t, s); closed != "" || rv.Branch != "feat/other" {
		t.Fatalf("once readable: closed %q, branch %q: should be tied to the branch it is on now", closed, rv.Branch)
	}
}

// A look at the branch that hangs (git on a stalled disk) is shared by the polls that come while it hangs, and each of them stops
// waiting when its own context ends, instead of queueing behind it.
func TestPollsWaitingOnAHungBranchLookupShareItAndGiveUpWhenTheirContextEnds(t *testing.T) {
	s, _, _ := tabServerWithClock(t)
	var lookups int32
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	s.branchLookup = func(ctx context.Context, dir string) (string, error) {
		atomic.AddInt32(&lookups, 1)
		started <- struct{}{}
		select {
		case <-release:
			return "main", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	first := make(chan error, 1)
	go func() { _, err := s.currentBranch(context.Background(), false); first <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	waited := make(chan error, 1)
	go func() { _, err := s.currentBranch(ctx, false); waited <- err }()
	select {
	case err := <-waited:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("second poll: %v, want its own deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second poll queued behind the hung lookup")
	}
	if n := atomic.LoadInt32(&lookups); n != 1 {
		t.Errorf("%d lookups: the second poll should have shared the first", n)
	}
	close(release)
	if err := <-first; err != nil {
		t.Errorf("the first poll: %v", err)
	}
}

func TestAReviewTheAgentStartedLooksAtTheBranchOnItsFirstLookAndClosesOnLeavingIt(t *testing.T) {
	s, store, _ := tabServerWithClock(t)
	// Started through the agent's own request, so no branch was noted at the start.
	if _, err := store.Do(review.Request{Op: "start", Note: "x"}, review.User); err != nil {
		t.Fatal(err)
	}
	if closed, rv := reviewIsClosed(t, s); closed != "" || !rv.BranchKnown || rv.Branch != "main" {
		t.Fatalf("first look: closed %q, %+v", closed, rv)
	}
	checkout(t, s, "-b", "feat/other")
	if closed, _ := reviewIsClosed(t, s); closed == "" {
		t.Error("not closed on leaving the branch it was first seen on")
	}
}

func TestStartingAReviewNotesTheBranchAtOnceSoASwitchBeforeTheFirstLookIsNoted(t *testing.T) {
	s, store, _ := tabServerWithClock(t)
	startReviewOnMain(t, s)
	if rv, _ := store.Load(); !rv.BranchKnown || rv.Branch != "main" {
		t.Errorf("not noted at the start: %+v", rv)
	}
}

// The turn ended between the question and the confirmation without the review being finished (it crashed, say): nothing was running to
// stop, so nothing is said to have been stopped, but the review is half written and is dropped all the same.
func TestStoppingAndDroppingWhenTheTurnHasEndedDropsAReviewStillInProgressAndSaysNothingWasStopped(t *testing.T) {
	s, store, _ := tabServerWithClock(t)
	f := startWritingReview(t, s)
	close(f.release)
	idle(t, s)
	_, stopped, dropped, err := s.stopAndDropReview("why")
	if err != nil || stopped || !dropped {
		t.Fatalf("stopped %v, dropped %v, %v", stopped, dropped, err)
	}
	if r, _ := store.Load(); !r.Started.IsZero() {
		t.Errorf("the review is still there: %+v", r)
	}
}

// Closing, with the user's agreement to the warning (stop), a review in progress whose turn has ended without it: dropped, not kept
// half written.
func TestClosingAReviewStillInProgressAfterItsTurnEndedDropsIt(t *testing.T) {
	s, store, _ := tabServerWithClock(t)
	f := startWritingReview(t, s)
	close(f.release)
	idle(t, s)
	if rec := do(t, s, post("/api/review/close", `{"stop":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body)
	}
	if r, _ := store.Load(); !r.Started.IsZero() {
		t.Errorf("kept half written: %+v", r)
	}
}
