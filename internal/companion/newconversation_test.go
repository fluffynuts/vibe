package companion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	cfg := Config{Sandbox: "box", Token: token, Port: testPort, StartAgent: f.agent.start}
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
		Current      string   `json:"current"`
		Dirty        bool     `json:"dirty"`
		Conflicted   bool     `json:"conflicted"`
		Default      string   `json:"default"`
		DefaultSet   bool     `json:"defaultSet"`
		DefaultEmpty bool     `json:"defaultEmpty"`
		NoDefault    string   `json:"noDefault"`
		Unavailable  string   `json:"unavailable"`
		Branches     []string `json:"branches"`
		Remote       []string `json:"remote"`
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

// The dialog says as the name is typed that a branch of that name is there already, here or at
// origin (as last fetched), so the options carry both lists.
func TestTheBranchOptionsListTheBranchesHereAndAtOrigin(t *testing.T) {
	work, upstream := repoWithOrigin(t)
	gitOut(t, work, "branch", "feat/here-only")
	gitOut(t, upstream, "branch", "feat/origin-only")
	gitOut(t, upstream, "push", "-q", "origin", "feat/origin-only")
	gitOut(t, work, "fetch", "-q", "origin")
	s := freshServer(t, freshSetup{work: work})
	o := options(t, s)
	sort.Strings(o.Branch.Branches)
	sort.Strings(o.Branch.Remote)
	if want := []string{"feat/here-only", "master"}; !reflect.DeepEqual(o.Branch.Branches, want) {
		t.Errorf("here: %v, want %v", o.Branch.Branches, want)
	}
	if want := []string{"feat/origin-only", "master"}; !reflect.DeepEqual(o.Branch.Remote, want) {
		t.Errorf("origin: %v, want %v (not origin/HEAD)", o.Branch.Remote, want)
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

func TestADetachedHeadIsOfferedAsOnNoBranch(t *testing.T) {
	work, _ := repoWithOrigin(t)
	gitOut(t, work, "switch", "-q", "--detach")
	s := freshServer(t, freshSetup{work: work})
	if o := options(t, s); o.Branch == nil || o.Branch.Current != "" || o.Branch.Unavailable != "" {
		t.Errorf("detached: %+v", o.Branch)
	}
}

// Each sbx exec costs a second or more (sbx's own start-up), so the dialog's options read the repository with one run of git
// in the sandbox, whatever they find: a set default branch too.
func TestTheOptionsRunGitInTheSandboxOnce(t *testing.T) {
	for name, setUp := range map[string]func(t *testing.T) string{
		"origin's default branch": func(t *testing.T) string { work, _ := repoWithOrigin(t); return work },
		"a set default branch": func(t *testing.T) string {
			work := newRepo(t)
			commitIn(t, work, "a.txt", "one\n")
			gitOut(t, work, "config", "vibe.defaultBranch", "master")
			gitOut(t, work, "switch", "-q", "-c", "feat/a")
			return work
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := freshServer(t, freshSetup{work: setUp(t)})
			var runs atomic.Int32
			git := s.cfg.SandboxGit
			s.cfg.SandboxGit = func(ctx context.Context, args ...string) (string, error) {
				runs.Add(1)
				return git(ctx, args...)
			}
			if o := options(t, s); o.Branch == nil || o.Branch.Unavailable != "" {
				t.Fatalf("branch = %+v", o.Branch)
			}
			if n := runs.Load(); n != 1 {
				t.Errorf("git ran %d times in the sandbox, want 1", n)
			}
		})
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

// Start can be pressed before the clipboard's counts are known, its box ticked as ever: an empty clipboard has nothing cleared to
// say, so the new conversation says nothing of it.
func TestClearingAnEmptyClipboardSaysNothing(t *testing.T) {
	s := freshServer(t, freshSetup{clipboard: &clipboardItemsStandIn{}})
	if code, out := send(t, s, "/api/conversation/new", `{"clipboard":"all"}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if said := strings.Join(eventsAfterReset(t, s), "\n"); strings.Contains(said, "clipboard") {
		t.Errorf("the conversation says %q", said)
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

func TestTheClipboardOfferAsksAgainOnlyWhereAMomentMayCureIt(t *testing.T) {
	var status int
	var body string
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer page.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	cases := []struct {
		name, url, asleep, body string
		status                  int
		retry                   bool
	}{
		{"nothing listening", closedURL, "", "", 0, true},
		{"asleep", page.URL, "the sandbox could not be woken", `{"items":[]}`, 200, false},
		{"answering", page.URL, "", `{"items":[]}`, 200, false},
		{"an error status", page.URL, "", "", 500, false},
		{"unreadable items", page.URL, "", "not json", 200, false},
	}
	for _, c := range cases {
		status, body = c.status, c.body
		s := freshServer(t, freshSetup{})
		s.cfg.ClipboardURL = func() string { return c.url }
		if got := s.clipboardOffer(context.Background(), c.asleep); got.Retry != c.retry {
			t.Errorf("%s: Retry is %v, want %v (%+v)", c.name, got.Retry, c.retry, got)
		}
	}
}

// The dialog's Start doesn't wait for the sandbox: its first answer, what the dialog offers at all, is from this machine alone.
// Starting wakes the sandbox anyway, and does what was ticked once it is up.
func TestTheDialogsFirstAnswerAsksNothingOfTheSandbox(t *testing.T) {
	work, _ := repoWithOrigin(t)
	s := freshServer(t, freshSetup{work: work, clipboard: &clipboardItemsStandIn{sent: []int{1}}, store: openReview(t)})
	s.cfg.Wake = func(context.Context, func(string)) error { t.Error("the sandbox was woken"); return nil }
	s.cfg.SandboxGit = func(context.Context, ...string) (string, error) { t.Error("git ran in the sandbox"); return "", nil }
	rec := do(t, s, req{path: "/api/conversation/new/ready", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("ready: %d %s", rec.Code, rec.Body)
	}
	var got map[string]bool
	json.Unmarshal(rec.Body.Bytes(), &got)
	if want := map[string]bool{"review": true, "clipboard": true, "repository": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("ready = %v, want %v", got, want)
	}

	none := freshServer(t, freshSetup{})
	rec = do(t, none, req{path: "/api/conversation/new/ready", headers: authed(nil)})
	json.Unmarshal(rec.Body.Bytes(), &got)
	if want := map[string]bool{"review": false, "clipboard": false, "repository": false}; !reflect.DeepEqual(got, want) {
		t.Errorf("with nothing to offer, ready = %v, want %v", got, want)
	}
}

// clipboardAgain asks the clipboard's counts again, as the dialog does while the clipboard page may still be starting.
func clipboardAgain(t *testing.T, s *Server, body string) clipboardOffer {
	t.Helper()
	rec := do(t, s, post("/api/conversation/new/clipboard", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("clipboard: %d %s", rec.Code, rec.Body)
	}
	var o clipboardOffer
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

// Asking the clipboard page again costs no sbx call: only the page is asked, not the sandbox woken nor git run, so the dialog can
// ask often while the page starts.
func TestTheClipboardIsAskedAgainOnItsOwn(t *testing.T) {
	work, _ := repoWithOrigin(t)
	s := freshServer(t, freshSetup{work: work, clipboard: &clipboardItemsStandIn{sent: []int{1, 2}, unsent: []int{3}}})
	s.cfg.Wake = func(context.Context, func(string)) error { t.Error("the sandbox was woken"); return nil }
	s.cfg.SandboxGit = func(context.Context, ...string) (string, error) { t.Error("git ran in the sandbox"); return "", nil }
	if o := clipboardAgain(t, s, `{"try":2}`); o.Sent != 2 || o.Unsent != 1 || o.Unavailable != "" || o.Retry {
		t.Errorf("clipboard = %+v", o)
	}
}

// A slow start of the dialog reads as one story in vibe's log: the clipboard page not answering at first, then answering on a
// later try, or not at all by the last.
func TestTheClipboardPageAnsweringLateIsLogged(t *testing.T) {
	items := &clipboardItemsStandIn{sent: []int{1}}
	answer := items.serve(t)
	var starting atomic.Bool
	starting.Store(true)
	// A page still starting drops the connection, as the user saw ("connection reset"); then it answers.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if starting.Load() {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		res, err := http.Get(answer + r.URL.Path)
		if err != nil {
			t.Error(err)
			return
		}
		defer res.Body.Close()
		io.Copy(w, res.Body)
	}))
	t.Cleanup(page.Close)
	s := freshServer(t, freshSetup{})
	s.cfg.ClipboardURL = func() string { return page.URL }
	var logged []string
	s.cfg.Log = func(m string) { logged = append(logged, m) }

	if o := options(t, s); o.Clipboard == nil || o.Clipboard.Unavailable == "" {
		t.Fatalf("clipboard = %+v", o.Clipboard)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "the clipboard page didn't answer") || !strings.Contains(logged[0], "asks again") {
		t.Errorf("the first try: %q", logged)
	}
	logged = nil
	clipboardAgain(t, s, `{"try":2}`)
	if len(logged) != 0 {
		t.Errorf("a try between the first and the last: %q", logged)
	}
	clipboardAgain(t, s, `{"try":5,"last":true}`)
	if len(logged) != 1 || !strings.Contains(logged[0], "still didn't answer") || !strings.Contains(logged[0], "5 tries") {
		t.Errorf("the last try: %q", logged)
	}
	logged = nil
	starting.Store(false)
	clipboardAgain(t, s, `{"try":3}`)
	if len(logged) != 1 || !strings.Contains(logged[0], "answered on try 3") {
		t.Errorf("answered on a later try: %q", logged)
	}
	logged = nil
	clipboardAgain(t, s, `{"try":1}`)
	if len(logged) != 0 {
		t.Errorf("answered on the first try: %q", logged)
	}
}

// Opening the New conversation dialog was slow to offer its button, saying nothing of why: when its options take long, vibe's log
// says how long each step took, and how many times git was run in the sandbox, to tell which to make faster.
func TestSlowNewConversationOptionsSayWhereTheTimeWent(t *testing.T) {
	work, _ := repoWithOrigin(t)
	s := freshServer(t, freshSetup{work: work, clipboard: &clipboardItemsStandIn{}})
	var logged []string
	s.cfg.Log = func(m string) { logged = append(logged, m) }
	var mu sync.Mutex
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	s.clock = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	s.cfg.Wake = func(context.Context, func(string)) error { advance(3 * time.Second); return nil }
	git := s.cfg.SandboxGit
	s.cfg.SandboxGit = func(ctx context.Context, args ...string) (string, error) {
		advance(time.Second)
		return git(ctx, args...)
	}
	options(t, s)
	var line string
	for _, m := range logged {
		if strings.Contains(m, "New conversation") {
			line = m
		}
	}
	for _, want := range []string{"waking the sandbox 3s", "git in the sandbox 1s (1 run)", "the clipboard page 0s"} {
		if !strings.Contains(line, want) {
			t.Errorf("the log line lacks %q: %q", want, line)
		}
	}

	logged = nil
	s.cfg.Wake = func(context.Context, func(string)) error { return nil }
	s.cfg.SandboxGit = git
	options(t, s)
	for _, m := range logged {
		if strings.Contains(m, "New conversation") {
			t.Errorf("a quick one was logged: %q", m)
		}
	}
}
