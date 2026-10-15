package companion

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	if rec := do(t, s, post("/api/review/do", `{"op":"comment","file":"a.txt","line":1,"body":"x"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("the page commenting as the agent: %d", rec.Code)
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
