package companion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	Review review.Review `json:"review"`
	Diff   []review.File `json:"diff"`
	Error  string        `json:"diffError"`
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
	colourWithin = 200 * time.Millisecond
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
	if h := colour("b.go"); h.Language != "Go" {
		t.Fatalf("before: %+v", h)
	}
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
	defer close(done)
	go s.takeInbox(done, 10*time.Millisecond)
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
	var said []string
	s.cfg.Log = func(line string) { said = append(said, line) }
	os.WriteFile(filepath.Join(filepath.Dir(store.Inbox()), "review.json"), []byte("{broken"), 0o600)
	done := make(chan struct{})
	go s.takeInbox(done, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	close(done)
	if len(said) == 0 || !strings.Contains(said[0], "review") {
		t.Errorf("said %q", said)
	}
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
