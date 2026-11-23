package companion

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPullRequestNumbersAreReadFromLsRemote(t *testing.T) {
	out := "599ba51604e4c3a77bdfd3b4566847c79057ba36\trefs/pull/7/head\n" +
		"86485d2f13dabbd8ba8031a8170d78138abaa5b5\trefs/pull/12/head\n" +
		"51e10d691db5c8c87c14cb82e061fb81399a98c6\trefs/pull/12/merge\n" +
		"51e10d691db5c8c87c14cb82e061fb81399a98c6\trefs/heads/master\n" +
		"51e10d691db5c8c87c14cb82e061fb81399a98c6\trefs/pull/x/head\n" +
		"\n"
	if got, want := parsePullRequestRefs(out), []int{7, 12}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// prAsker stands in for the sandbox's git: it answers with numbers or err, and counts the repositories it was asked about.
type prAsker struct {
	mu      sync.Mutex
	numbers []int
	err     error
	asked   []string
}

func (a *prAsker) ask(ctx context.Context, repo string) ([]int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, repo)
	return a.numbers, a.err
}

func (a *prAsker) times() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.asked)
}

type prBody struct {
	Numbers     []int  `json:"numbers"`
	Version     int    `json:"version"`
	Unavailable string `json:"unavailable"`
}

// prServer is a page for a folder on GitHub whose pull requests asker lists, on a clock the test moves.
func prServer(asker *prAsker, logged *[]string) (*Server, *time.Time) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	s := New(Config{Sandbox: "vibe", Token: token, Port: testPort, Workspace: "/work",
		Git:          func(string) *GitInfo { return &GitInfo{Branch: "main", Web: "https://github.com/me/vibe"} },
		PullRequests: asker.ask,
		Log:          func(m string) { *logged = append(*logged, m) },
	})
	s.clock = func() time.Time { return now }
	return s, &now
}

// getPRs asks for the list, wanting the numbers in want (comma-separated), and waits for any ask that started.
func getPRs(t *testing.T, s *Server, want string) prBody {
	t.Helper()
	path := "/api/pull-requests"
	if want != "" {
		path += "?want=" + want
	}
	rec := do(t, s, req{path: path, headers: authed(nil)})
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	s.prs.flight.Wait()
	var b prBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAWantedNumberAsksTheSandboxForTheRepositorysPullRequests(t *testing.T) {
	asker := &prAsker{numbers: []int{3, 7}}
	var logged []string
	s, _ := prServer(asker, &logged)
	if b := getPRs(t, s, ""); b.Version != 0 || asker.times() != 0 {
		t.Fatalf("asked with nothing wanted: %+v, %d asks", b, asker.times())
	}
	getPRs(t, s, "7")
	if !reflect.DeepEqual(asker.asked, []string{"https://github.com/me/vibe"}) {
		t.Fatalf("asked about %v", asker.asked)
	}
	b := getPRs(t, s, "")
	if !reflect.DeepEqual(b.Numbers, []int{3, 7}) || b.Version == 0 {
		t.Errorf("got %+v", b)
	}
	if state := pageState(t, s); state["pullRequests"] != float64(b.Version) {
		t.Errorf("the state's version is %v, the list's %d", state["pullRequests"], b.Version)
	}
}

func TestANumberAlreadyListedAsksNothing(t *testing.T) {
	asker := &prAsker{numbers: []int{3}}
	var logged []string
	s, now := prServer(asker, &logged)
	getPRs(t, s, "3")
	*now = now.Add(time.Hour)
	getPRs(t, s, "3")
	if asker.times() != 1 {
		t.Errorf("asked %d times", asker.times())
	}
}

func TestANumberNotListedAsksAgainAtMostOnceAMinute(t *testing.T) {
	asker := &prAsker{numbers: []int{3}}
	var logged []string
	s, now := prServer(asker, &logged)
	getPRs(t, s, "4")
	getPRs(t, s, "4")
	*now = now.Add(59 * time.Second)
	getPRs(t, s, "4")
	if asker.times() != 1 {
		t.Fatalf("asked %d times within the minute", asker.times())
	}
	before := getPRs(t, s, "").Version
	asker.numbers = []int{3, 4}
	*now = now.Add(time.Second)
	getPRs(t, s, "4")
	if asker.times() != 2 {
		t.Fatalf("asked %d times after a minute", asker.times())
	}
	if b := getPRs(t, s, ""); !reflect.DeepEqual(b.Numbers, []int{3, 4}) || b.Version <= before {
		t.Errorf("got %+v, the version before %d", b, before)
	}
}

func TestAStoppedSandboxIsNotAFailure(t *testing.T) {
	asker := &prAsker{err: ErrNotRunning}
	var logged []string
	s, _ := prServer(asker, &logged)
	getPRs(t, s, "4")
	b := getPRs(t, s, "")
	if b.Numbers == nil || len(b.Numbers) != 0 || !strings.Contains(b.Unavailable, "isn't running") {
		t.Errorf("got %+v", b)
	}
	if len(logged) != 0 {
		t.Errorf("logged %q", logged)
	}
}

func TestAFailedAskIsSaidAndLoggedAndNotRetriedAtOnce(t *testing.T) {
	asker := &prAsker{err: errors.New("fatal: could not read Username")}
	var logged []string
	s, _ := prServer(asker, &logged)
	getPRs(t, s, "4")
	b := getPRs(t, s, "4")
	if asker.times() != 1 {
		t.Errorf("asked %d times", asker.times())
	}
	if !strings.Contains(b.Unavailable, "could not read Username") {
		t.Errorf("unavailable: %q", b.Unavailable)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "could not read Username") || !strings.Contains(logged[0], "pull requests") {
		t.Errorf("logged %q", logged)
	}
}

func TestAFailedAskKeepsTheListAlreadyRead(t *testing.T) {
	asker := &prAsker{numbers: []int{3}}
	var logged []string
	s, now := prServer(asker, &logged)
	getPRs(t, s, "3")
	asker.numbers, asker.err = nil, errors.New("timed out")
	*now = now.Add(time.Minute)
	getPRs(t, s, "4")
	if b := getPRs(t, s, ""); !reflect.DeepEqual(b.Numbers, []int{3}) {
		t.Errorf("got %+v", b)
	}
}

func TestAFolderNotOnGitHubAsksNothing(t *testing.T) {
	asker := &prAsker{numbers: []int{3}}
	var logged []string
	s, _ := prServer(asker, &logged)
	s.cfg.Git = func(string) *GitInfo { return &GitInfo{Branch: "main"} }
	getPRs(t, s, "3")
	if b := getPRs(t, s, ""); asker.times() != 0 || b.Unavailable == "" {
		t.Errorf("got %+v, %d asks", b, asker.times())
	}
}

func TestAPageThatCantAskHasNoPullRequests(t *testing.T) {
	s := newServer(&fakeSbx{})
	if rec := do(t, s, req{path: "/api/pull-requests?want=3", headers: authed(nil)}); rec.Code != 404 {
		t.Errorf("status %d", rec.Code)
	}
}

func TestOddWantsAreRefused(t *testing.T) {
	asker := &prAsker{numbers: []int{3}}
	var logged []string
	s, _ := prServer(asker, &logged)
	for _, want := range []string{"x", "-1", "1,,2", strings.Repeat("1,", 300) + "1"} {
		if rec := do(t, s, req{path: "/api/pull-requests?want=" + want, headers: authed(nil)}); rec.Code != 400 {
			t.Errorf("want=%.20s: status %d", want, rec.Code)
		}
	}
	if asker.times() != 0 {
		t.Errorf("asked %d times", asker.times())
	}
}

func TestAStoppedSandboxsGitIsNotRunToListPullRequests(t *testing.T) {
	ran := 0
	list := PullRequestsFrom(func(context.Context) bool { return false }, func(context.Context, ...string) (string, error) { ran++; return "", nil })
	if _, err := list(context.Background(), "https://github.com/me/vibe"); !errors.Is(err, ErrNotRunning) || ran != 0 {
		t.Errorf("err %v, git ran %d times", err, ran)
	}
}

func TestARunningSandboxsGitListsThePullRequestRefs(t *testing.T) {
	var args []string
	list := PullRequestsFrom(func(context.Context) bool { return true }, func(_ context.Context, a ...string) (string, error) {
		args = a
		return "abc1\trefs/pull/5/head\n", nil
	})
	got, err := list(context.Background(), "https://github.com/me/vibe")
	if err != nil || !reflect.DeepEqual(got, []int{5}) {
		t.Errorf("got %v, %v", got, err)
	}
	if want := []string{"ls-remote", "https://github.com/me/vibe.git", "refs/pull/*/head"}; !reflect.DeepEqual(args, want) {
		t.Errorf("ran git %q", args)
	}
}
