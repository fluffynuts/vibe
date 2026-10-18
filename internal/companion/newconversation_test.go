package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"vibe/internal/review"
)

// gitOut runs git in dir as a test sets things up, giving what it said,
// and failing the test if git fails.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=master"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitIn commits a change to name in dir.
func commitIn(t *testing.T, dir, name, body string) {
	t.Helper()
	writeIn(t, dir, name, body)
	gitOut(t, dir, "add", "-A")
	gitOut(t, dir, "commit", "-q", "-m", "change "+name)
}

// repoWithOrigin is a clone of an origin whose default branch is master,
// with a commit on it: work is the clone, upstream another clone, for
// pushing commits to origin that work hasn't fetched.
func repoWithOrigin(t *testing.T) (work, upstream string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitOut(t, root, "init", "-q", "--bare", "-b", "master", origin)
	seed := filepath.Join(root, "seed")
	gitOut(t, root, "clone", "-q", origin, seed)
	commitIn(t, seed, "a.txt", "one\n")
	gitOut(t, seed, "push", "-q", "origin", "master")
	work = filepath.Join(root, "work")
	gitOut(t, root, "clone", "-q", origin, work)
	return work, seed
}

// gitStandIn runs git on this machine in place of the sandbox's: what the
// sandbox's git would do to the same folder.
func gitStandIn(dir string) func(ctx context.Context, args ...string) (string, error) {
	return func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			return out.String(), errors.New(strings.TrimSpace(errOut.String()))
		}
		return out.String(), nil
	}
}

// clipboardItemsStandIn is the sandbox's clipboard page, as far as its items go:
// sent are delivered already, unsent are waiting. It keeps what was deleted.
type clipboardItemsStandIn struct {
	mu      sync.Mutex
	sent    []int
	unsent  []int
	deleted []string
	down    bool
}

func (c *clipboardItemsStandIn) serve(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.down {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/items":
			items := []map[string]interface{}{}
			for _, id := range c.sent {
				items = append(items, map[string]interface{}{"id": id, "delivered": "2026-10-18T00:00:00Z"})
			}
			for _, id := range c.unsent {
				items = append(items, map[string]interface{}{"id": id, "delivered": nil})
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"items": items})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/items":
			c.deleted = append(c.deleted, "all")
			n := len(c.sent) + len(c.unsent)
			c.sent, c.unsent = nil, nil
			json.NewEncoder(w).Encode(map[string]int{"deleted": n})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/items/"):
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/items/"))
			c.deleted = append(c.deleted, strconv.Itoa(id))
			json.NewEncoder(w).Encode(map[string]int{"deleted": id})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

type freshSetup struct {
	work      string
	clipboard *clipboardItemsStandIn
	store     *review.Store
	wake      error
	agent     *fakeAgent
}

func freshServer(t *testing.T, f freshSetup) *Server {
	t.Helper()
	if f.agent == nil {
		f.agent = &fakeAgent{}
	}
	cfg := Config{Sandbox: "box", Token: token, Port: testPort, Converse: f.agent.converse}
	if f.work != "" {
		cfg.Workspace = f.work
		cfg.SandboxGit = gitStandIn(f.work)
	}
	if f.clipboard != nil {
		url := f.clipboard.serve(t)
		cfg.ClipboardURL = func() string { return url }
	}
	if f.store != nil {
		cfg.ReviewTab = &ReviewTab{Workspace: t.TempDir(), Store: f.store}
	}
	wake := f.wake
	cfg.Wake = func(context.Context, func(string)) error { return wake }
	return New(cfg)
}

type freshOptions struct {
	Review    bool `json:"review"`
	Clipboard *struct {
		Sent        int    `json:"sent"`
		Unsent      int    `json:"unsent"`
		Unavailable string `json:"unavailable"`
	} `json:"clipboard"`
	Branch *struct {
		Current      string `json:"current"`
		Dirty        bool   `json:"dirty"`
		Conflicted   bool   `json:"conflicted"`
		Default      string `json:"default"`
		DefaultSet   bool   `json:"defaultSet"`
		DefaultEmpty bool   `json:"defaultEmpty"`
		NoDefault    string `json:"noDefault"`
		Unavailable  string `json:"unavailable"`
	} `json:"branch"`
}

func options(t *testing.T, s *Server) (o freshOptions) {
	t.Helper()
	rec := do(t, s, post("/api/conversation/new/options", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("options: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

type fetchResult struct {
	Checked bool   `json:"checked"`
	Why     string `json:"why"`
	Behind  int    `json:"behind"`
	Ahead   int    `json:"ahead"`
	Local   bool   `json:"local"`
}

func fetchOrigin(t *testing.T, s *Server) (f fetchResult) {
	t.Helper()
	rec := do(t, s, post("/api/conversation/new/fetch", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func openReview(t *testing.T) *review.Store {
	t.Helper()
	store := review.NewStore(t.TempDir())
	if _, err := store.Do(review.Request{Op: "start", Ref: "HEAD"}, review.User); err != nil {
		t.Fatal(err)
	}
	return store
}

// eventsSince are the conversation's events after a reset, as kind: text.
func eventsAfterReset(t *testing.T, s *Server) []string {
	t.Helper()
	var out []string
	for _, e := range poll(t, s, 0).Events {
		if e.Kind == "reset" {
			out = nil
			continue
		}
		out = append(out, e.Kind+": "+e.Text)
	}
	return out
}

func TestTheNewConversationOptionsOfferOnlyWhatApplies(t *testing.T) {
	s := freshServer(t, freshSetup{})
	o := options(t, s)
	if o.Review || o.Clipboard != nil || o.Branch != nil {
		t.Errorf("with no review, clipboard or folder, it offered %+v", o)
	}
}

func TestAnOpenReviewIsOfferedAndClosedForANewConversation(t *testing.T) {
	store := openReview(t)
	s := freshServer(t, freshSetup{store: store})
	if !options(t, s).Review {
		t.Fatal("an open review isn't offered for closing")
	}
	if code, out := send(t, s, "/api/conversation/new", `{"review":true}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	rv, _ := store.Load()
	if rv.Closed != "Closed for a new conversation." {
		t.Errorf("the review's closed as %q", rv.Closed)
	}
	if options(t, s).Review {
		t.Error("a closed review is still offered")
	}
}

func TestAReviewLeftOpenWhenNotAsked(t *testing.T) {
	store := openReview(t)
	s := freshServer(t, freshSetup{store: store})
	send(t, s, "/api/conversation/new", `{"review":false}`)
	if rv, _ := store.Load(); rv.Closed != "" {
		t.Errorf("closed unasked: %q", rv.Closed)
	}
}

func TestTheClipboardsSentAndUnsentItemsAreCounted(t *testing.T) {
	s := freshServer(t, freshSetup{clipboard: &clipboardItemsStandIn{sent: []int{1, 2}, unsent: []int{3}}})
	o := options(t, s)
	if o.Clipboard == nil || o.Clipboard.Sent != 2 || o.Clipboard.Unsent != 1 {
		t.Errorf("clipboard = %+v", o.Clipboard)
	}
}

func TestClearingTheClipboardLeavesUnsentItemsUnlessAsked(t *testing.T) {
	c := &clipboardItemsStandIn{sent: []int{1, 2}, unsent: []int{3}}
	s := freshServer(t, freshSetup{clipboard: c})
	if code, out := send(t, s, "/api/conversation/new", `{"clipboard":"sent"}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if got := strings.Join(c.deleted, ","); got != "1,2" {
		t.Errorf("deleted %q, want the sent items 1 and 2 alone", got)
	}
}

func TestClearingUnsentItemsTooEmptiesTheClipboard(t *testing.T) {
	c := &clipboardItemsStandIn{sent: []int{1}, unsent: []int{3}}
	s := freshServer(t, freshSetup{clipboard: c})
	send(t, s, "/api/conversation/new", `{"clipboard":"all"}`)
	if got := strings.Join(c.deleted, ","); got != "all" {
		t.Errorf("deleted %q, want everything", got)
	}
}

func TestTheClipboardIsLeftAloneWhenNotAsked(t *testing.T) {
	c := &clipboardItemsStandIn{sent: []int{1}, unsent: []int{3}}
	s := freshServer(t, freshSetup{clipboard: c})
	send(t, s, "/api/conversation/new", `{}`)
	if len(c.deleted) != 0 {
		t.Errorf("deleted %v unasked", c.deleted)
	}
}

func TestASandboxThatWontWakeMakesTheClipboardAndBranchUnavailableSayingWhy(t *testing.T) {
	work, _ := repoWithOrigin(t)
	s := freshServer(t, freshSetup{work: work, clipboard: &clipboardItemsStandIn{sent: []int{1}}, store: openReview(t),
		wake: errors.New("the sandbox was stopped on purpose")})
	o := options(t, s)
	if o.Clipboard == nil || !strings.Contains(o.Clipboard.Unavailable, "stopped on purpose") {
		t.Errorf("clipboard = %+v", o.Clipboard)
	}
	if o.Branch == nil || !strings.Contains(o.Branch.Unavailable, "stopped on purpose") {
		t.Errorf("branch = %+v", o.Branch)
	}
	if !o.Review {
		t.Error("the review needs only this machine, but isn't offered")
	}
}

func TestTheBranchOptionsSayWhereYouAreAndWhatTheDefaultBranchIs(t *testing.T) {
	work, _ := repoWithOrigin(t)
	s := freshServer(t, freshSetup{work: work})
	o := options(t, s)
	if o.Branch == nil || o.Branch.Current != "master" || o.Branch.Default != "master" || o.Branch.Dirty || o.Branch.Unavailable != "" {
		t.Errorf("on master: %+v", o.Branch)
	}
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	if o = options(t, s); o.Branch.Current != "feat/a" || o.Branch.Default != "master" {
		t.Errorf("on feat/a: %+v", o.Branch)
	}
}

func TestWithoutOriginHEADTheDefaultBranchIsntGuessed(t *testing.T) {
	work, _ := repoWithOrigin(t)
	gitOut(t, work, "remote", "set-head", "origin", "-d")
	s := freshServer(t, freshSetup{work: work})
	o := options(t, s)
	if o.Branch.Default != "" || !strings.Contains(o.Branch.NoDefault, "git remote set-head origin --auto") {
		t.Errorf("branch = %+v", o.Branch)
	}
}

func TestADirtyTreeIsSaidAndUntrackedFilesCount(t *testing.T) {
	work, _ := repoWithOrigin(t)
	s := freshServer(t, freshSetup{work: work})
	writeIn(t, work, "new.txt", "untracked\n")
	if o := options(t, s); !o.Branch.Dirty {
		t.Errorf("an untracked file: %+v", o.Branch)
	}
}

func TestConflictsMakeBranchingUnavailable(t *testing.T) {
	work, _ := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "-c", "other")
	commitIn(t, work, "a.txt", "other\n")
	gitOut(t, work, "switch", "-q", "master")
	commitIn(t, work, "a.txt", "mine\n")
	exec.Command("git", "-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "-q", "other").Run()
	s := freshServer(t, freshSetup{work: work})
	o := options(t, s)
	if !o.Branch.Conflicted || !strings.Contains(o.Branch.Unavailable, "conflict") {
		t.Errorf("mid-merge with a conflict: %+v", o.Branch)
	}
}

// A folder below its repository's top: git in the sandbox, which has only
// the folder, sees no repository at all.
func TestGitInTheSandboxSeeingNoRepositoryMakesBranchingUnavailable(t *testing.T) {
	plain := t.TempDir()
	s := freshServer(t, freshSetup{work: plain})
	s.cfg.Git = func(string) *GitInfo { return &GitInfo{Branch: "master"} }
	o := options(t, s)
	if o.Branch == nil || o.Branch.Unavailable == "" {
		t.Errorf("branch = %+v", o.Branch)
	}
}

func TestNoBranchIsOfferedOutsideARepository(t *testing.T) {
	s := freshServer(t, freshSetup{work: t.TempDir()})
	s.cfg.Git = func(string) *GitInfo { return nil }
	if o := options(t, s); o.Branch != nil {
		t.Errorf("branch = %+v", o.Branch)
	}
}

func TestFetchingSaysHowFarBehindOriginTheLocalDefaultBranchIs(t *testing.T) {
	work, upstream := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	commitIn(t, upstream, "b.txt", "two\n")
	commitIn(t, upstream, "c.txt", "three\n")
	gitOut(t, upstream, "push", "-q", "origin", "master")
	s := freshServer(t, freshSetup{work: work})
	f := fetchOrigin(t, s)
	if !f.Checked || !f.Local || f.Behind != 2 || f.Ahead != 0 {
		t.Errorf("fetch = %+v", f)
	}
}

func TestFetchingSaysWhenTheLocalDefaultBranchHasCommitsOriginHasnt(t *testing.T) {
	work, _ := repoWithOrigin(t)
	commitIn(t, work, "mine.txt", "local\n")
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	s := freshServer(t, freshSetup{work: work})
	if f := fetchOrigin(t, s); !f.Checked || f.Ahead != 1 || f.Behind != 0 {
		t.Errorf("fetch = %+v", f)
	}
}

func TestAFailedFetchSaysWhy(t *testing.T) {
	work, _ := repoWithOrigin(t)
	gitOut(t, work, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	s := freshServer(t, freshSetup{work: work})
	if f := fetchOrigin(t, s); f.Checked || f.Why == "" {
		t.Errorf("fetch = %+v", f)
	}
}

func currentBranch(t *testing.T, dir string) string {
	return gitOut(t, dir, "branch", "--show-current")
}

func TestBranchingFromHereTakesUncommittedWorkAlong(t *testing.T) {
	work, _ := repoWithOrigin(t)
	writeIn(t, work, "a.txt", "half done\n")
	s := freshServer(t, freshSetup{work: work})
	if code, out := send(t, s, "/api/conversation/new", `{"branch":{"name":"feat/x","from":"here"}}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if b := currentBranch(t, work); b != "feat/x" {
		t.Errorf("on %q", b)
	}
	if data, _ := os.ReadFile(filepath.Join(work, "a.txt")); string(data) != "half done\n" {
		t.Errorf("the uncommitted change is gone: %q", data)
	}
	if out, _ := exec.Command("git", "-C", work, "config", "branch.feat/x.merge").Output(); len(out) != 0 {
		t.Errorf("the new branch tracks %s", out)
	}
}

func TestBranchingFromTheDefaultBranchStartsAtOriginsWithoutTrackingIt(t *testing.T) {
	work, upstream := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	commitIn(t, work, "a-only.txt", "a\n")
	commitIn(t, upstream, "b.txt", "two\n")
	gitOut(t, upstream, "push", "-q", "origin", "master")
	s := freshServer(t, freshSetup{work: work})
	fetchOrigin(t, s)
	if code, out := send(t, s, "/api/conversation/new", `{"branch":{"name":"feat/x","from":"default"}}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if b := currentBranch(t, work); b != "feat/x" {
		t.Errorf("on %q", b)
	}
	if head, want := gitOut(t, work, "rev-parse", "HEAD"), gitOut(t, work, "rev-parse", "origin/master"); head != want {
		t.Errorf("feat/x is at %s, want origin/master's %s", head, want)
	}
	if out, _ := exec.Command("git", "-C", work, "config", "branch.feat/x.merge").Output(); len(out) != 0 {
		t.Errorf("the new branch tracks %s", out)
	}
}

func TestBranchingCanFastForwardTheLocalDefaultBranch(t *testing.T) {
	work, upstream := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	commitIn(t, upstream, "b.txt", "two\n")
	gitOut(t, upstream, "push", "-q", "origin", "master")
	s := freshServer(t, freshSetup{work: work})
	fetchOrigin(t, s)
	if code, out := send(t, s, "/api/conversation/new", `{"branch":{"name":"feat/x","from":"here","update":true}}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if local, origin := gitOut(t, work, "rev-parse", "master"), gitOut(t, work, "rev-parse", "origin/master"); local != origin {
		t.Errorf("local master is at %s, origin's at %s", local, origin)
	}
	if b := currentBranch(t, work); b != "feat/x" {
		t.Errorf("on %q", b)
	}
}

// Asked to update a local default branch that has its own commits (it
// gained them after the dialog looked), it's left as it is, saying so; the
// branch is still made.
func TestALocalDefaultBranchThatCantFastForwardIsLeftAsItIsSayingSo(t *testing.T) {
	work, upstream := repoWithOrigin(t)
	commitIn(t, work, "mine.txt", "local\n")
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	commitIn(t, upstream, "b.txt", "two\n")
	gitOut(t, upstream, "push", "-q", "origin", "master")
	s := freshServer(t, freshSetup{work: work})
	fetchOrigin(t, s)
	before := gitOut(t, work, "rev-parse", "master")
	if code, out := send(t, s, "/api/conversation/new", `{"branch":{"name":"feat/x","from":"here","update":true}}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if after := gitOut(t, work, "rev-parse", "master"); after != before {
		t.Error("local master was moved")
	}
	said := strings.Join(eventsAfterReset(t, s), "\n")
	if !strings.Contains(said, "master wasn't brought up to date") {
		t.Errorf("the conversation says:\n%s", said)
	}
}

func TestBranchingFromTheDefaultBranchIsRefusedWhenTheTreeIsDirtyAndNothingElseIsDone(t *testing.T) {
	work, _ := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	writeIn(t, work, "a.txt", "half done\n")
	store := openReview(t)
	c := &clipboardItemsStandIn{sent: []int{1}}
	f := &fakeAgent{}
	s := freshServer(t, freshSetup{work: work, store: store, clipboard: c, agent: f})
	send(t, s, "/api/conversation/send", `{"prompt":"keep me"}`)
	idle(t, s)
	code, out := send(t, s, "/api/conversation/new", `{"review":true,"clipboard":"all","branch":{"name":"feat/x","from":"default"}}`)
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "uncommitted") {
		t.Errorf("new: %d %v", code, out)
	}
	assertNothingDone(t, s, work, "feat/a", store, c)
}

func assertNothingDone(t *testing.T, s *Server, work, branch string, store *review.Store, c *clipboardItemsStandIn) {
	t.Helper()
	if b := currentBranch(t, work); b != branch {
		t.Errorf("on %q, want %q", b, branch)
	}
	if rv, _ := store.Load(); rv.Closed != "" {
		t.Error("the review was closed")
	}
	if len(c.deleted) != 0 {
		t.Errorf("the clipboard was cleared: %v", c.deleted)
	}
	for _, e := range poll(t, s, 0).Events {
		if e.Kind == "reset" {
			t.Error("the conversation was reset")
		}
	}
}

func TestABadOrTakenBranchNameIsRefusedAndNothingElseIsDone(t *testing.T) {
	for _, name := range []string{"feat/", "feat/a b", "-x", "", "master"} {
		t.Run(name, func(t *testing.T) {
			work, _ := repoWithOrigin(t)
			store := openReview(t)
			c := &clipboardItemsStandIn{sent: []int{1}}
			s := freshServer(t, freshSetup{work: work, store: store, clipboard: c})
			send(t, s, "/api/conversation/send", `{"prompt":"keep me"}`)
			idle(t, s)
			body, _ := json.Marshal(map[string]interface{}{"review": true, "clipboard": "all", "branch": map[string]string{"name": name, "from": "here"}})
			if code, out := send(t, s, "/api/conversation/new", string(body)); code != http.StatusBadRequest || out["error"] == "" {
				t.Errorf("new: %d %v", code, out)
			}
			assertNothingDone(t, s, work, "master", store, c)
		})
	}
}

func TestNothingIsDoneWhileTheAgentIsAnswering(t *testing.T) {
	work, _ := repoWithOrigin(t)
	f := &fakeAgent{release: make(chan struct{})}
	store := openReview(t)
	c := &clipboardItemsStandIn{sent: []int{1}}
	s := freshServer(t, freshSetup{work: work, store: store, clipboard: c, agent: f})
	send(t, s, "/api/conversation/send", `{"prompt":"slow"}`)
	code, _ := send(t, s, "/api/conversation/new", `{"review":true,"clipboard":"all","branch":{"name":"feat/x","from":"here"}}`)
	if code != http.StatusConflict {
		t.Errorf("new while answering: %d", code)
	}
	close(f.release)
	idle(t, s)
	assertNothingDone(t, s, work, "master", store, c)
}

func TestTheNewConversationSaysWhatWasDone(t *testing.T) {
	work, upstream := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	commitIn(t, upstream, "b.txt", "two\n")
	gitOut(t, upstream, "push", "-q", "origin", "master")
	s := freshServer(t, freshSetup{work: work, store: openReview(t), clipboard: &clipboardItemsStandIn{sent: []int{1}, unsent: []int{2}}})
	fetchOrigin(t, s)
	send(t, s, "/api/conversation/new", `{"review":true,"clipboard":"all","branch":{"name":"feat/x","from":"default","update":true}}`)
	said := strings.Join(eventsAfterReset(t, s), "\n")
	for _, want := range []string{"new branch feat/x", "from origin/master", "local master brought up to date", "Closed the review", "Cleared the clipboard"} {
		if !strings.Contains(said, want) {
			t.Errorf("the conversation doesn't say %q:\n%s", want, said)
		}
	}
}

func TestAClipboardThatCantBeClearedIsReportedAndTheConversationStillStartsAnew(t *testing.T) {
	c := &clipboardItemsStandIn{sent: []int{1}, down: true}
	s := freshServer(t, freshSetup{clipboard: c})
	if code, out := send(t, s, "/api/conversation/new", `{"clipboard":"sent"}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	said := eventsAfterReset(t, s)
	if len(said) != 1 || !strings.HasPrefix(said[0], "error: ") || !strings.Contains(said[0], "clipboard") {
		t.Errorf("the conversation says %q", said)
	}
}

func TestANewConversationWithNoOptionsSaysNothingMore(t *testing.T) {
	s := freshServer(t, freshSetup{})
	send(t, s, "/api/conversation/new", `{}`)
	if said := eventsAfterReset(t, s); len(said) != 0 {
		t.Errorf("the conversation says %q", said)
	}
}

func TestAnUnknownClipboardChoiceIsRefused(t *testing.T) {
	s := freshServer(t, freshSetup{clipboard: &clipboardItemsStandIn{}})
	if code, _ := send(t, s, "/api/conversation/new", `{"clipboard":"some"}`); code != http.StatusBadRequest {
		t.Errorf("new: %d", code)
	}
}

func TestWithNoOriginRemoteTheDefaultBranchIsntGuessed(t *testing.T) {
	work, _ := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	gitOut(t, work, "remote", "remove", "origin")
	s := freshServer(t, freshSetup{work: work})
	o := options(t, s)
	if o.Branch.Default != "" || !strings.Contains(o.Branch.NoDefault, "no origin remote") {
		t.Errorf("branch = %+v", o.Branch)
	}
}
