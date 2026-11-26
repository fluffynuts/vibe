package companion

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// prAt stands in for the host's gh: the open pull request of each branch (none when not listed), and the asks it was given.
type prAt struct {
	mu    sync.Mutex
	open  map[string]*BranchPullRequest
	err   error
	asked []string
}

func (p *prAt) ask(_ context.Context, repo, branch string) (*BranchPullRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, repo+" "+branch)
	if p.err != nil {
		return nil, p.err
	}
	// A copy, as gh's answer is each time: what the test changes later isn't what the page already has.
	if pr := p.open[branch]; pr != nil {
		c := *pr
		return &c, nil
	}
	return nil, nil
}

func (p *prAt) times() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.asked)
}

// branchPRServer is a page for a workspace on GitHub, on branch (git), asking gh through p, its clock at now.
func branchPRServer(t *testing.T, p *prAt, git *GitInfo, now *time.Time, logged *[]string) *Server {
	t.Helper()
	s := newServer(&fakeSbx{})
	s.cfg.Workspace = t.TempDir()
	s.cfg.Git = func(string) *GitInfo { g := *git; return &g }
	s.cfg.BranchPullRequest = p.ask
	s.cfg.Log = func(m string) { *logged = append(*logged, m) }
	s.clock = func() time.Time { return *now }
	return s
}

// stateAfterAsk is the page's state once any ask it set off has been answered, read again.
func stateAfterAsk(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	pageState(t, s)
	s.branchPR.flight.Wait()
	s.cache.clear()
	return pageState(t, s)
}

func TestTheStateCarriesTheBranchsOpenPullRequestOnceAsked(t *testing.T) {
	p := &prAt{open: map[string]*BranchPullRequest{"feat/a": {Number: 40, URL: "https://github.com/me/vibe/pull/40"}}}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	var logged []string
	s := branchPRServer(t, p, &GitInfo{Branch: "feat/a", Web: "https://github.com/me/vibe"}, &now, &logged)
	pr, ok := stateAfterAsk(t, s)["branchPullRequest"].(map[string]interface{})
	if !ok || pr["number"] != float64(40) || pr["url"] != "https://github.com/me/vibe/pull/40" {
		t.Fatalf("branchPullRequest = %v", stateAfterAsk(t, s)["branchPullRequest"])
	}
	if want := []string{"https://github.com/me/vibe feat/a"}; len(p.asked) != 1 || p.asked[0] != want[0] {
		t.Errorf("asked %v, want %v", p.asked, want)
	}
}

func TestABranchWithNoOpenPullRequestHasNone(t *testing.T) {
	p := &prAt{}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	var logged []string
	s := branchPRServer(t, p, &GitInfo{Branch: "feat/a", Web: "https://github.com/me/vibe"}, &now, &logged)
	if v, ok := stateAfterAsk(t, s)["branchPullRequest"]; ok && v != nil {
		t.Errorf("branchPullRequest = %v", v)
	}
}

func TestAnotherBranchIsAskedAboutAtOnce(t *testing.T) {
	p := &prAt{open: map[string]*BranchPullRequest{"feat/b": {Number: 41, URL: "https://github.com/me/vibe/pull/41"}}}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	git := &GitInfo{Branch: "feat/a", Web: "https://github.com/me/vibe"}
	var logged []string
	s := branchPRServer(t, p, git, &now, &logged)
	stateAfterAsk(t, s)
	git.Branch = "feat/b"
	s.cache.clear()
	pr, _ := stateAfterAsk(t, s)["branchPullRequest"].(map[string]interface{})
	if pr == nil || pr["number"] != float64(41) {
		t.Errorf("after switching branch: %v (asked %v)", pr, p.asked)
	}
}

func TestTheSameBranchIsAskedAgainOnlyAfterAMinute(t *testing.T) {
	p := &prAt{}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	var logged []string
	s := branchPRServer(t, p, &GitInfo{Branch: "feat/a", Web: "https://github.com/me/vibe"}, &now, &logged)
	stateAfterAsk(t, s)
	now = now.Add(40 * time.Second)
	stateAfterAsk(t, s)
	if p.times() != 1 {
		t.Fatalf("asked %d times within a minute", p.times())
	}
	now = now.Add(21 * time.Second)
	stateAfterAsk(t, s)
	if p.times() != 2 {
		t.Errorf("asked %d times after a minute, want 2", p.times())
	}
}

func TestNothingIsAskedWithoutABranchOnGitHub(t *testing.T) {
	for name, git := range map[string]*GitInfo{
		"detached":       {Detached: true, Web: "https://github.com/me/vibe"},
		"not on GitHub":  {Branch: "feat/a"},
		"no branch name": {Web: "https://github.com/me/vibe"},
	} {
		t.Run(name, func(t *testing.T) {
			p := &prAt{}
			now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
			var logged []string
			s := branchPRServer(t, p, git, &now, &logged)
			stateAfterAsk(t, s)
			if p.times() != 0 {
				t.Errorf("asked %v", p.asked)
			}
		})
	}
}

// gh missing says nothing: most hosts won't have it. Any other failure (not signed in, the network) is logged once, not every
// minute, and shows no pull request.
func TestAFailedAskShowsNoneAndIsLoggedOnce(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		err  error
		logs int
	}{"no gh": {ErrNoGh, 0}, "gh failed": {errors.New("gh: not logged in"), 1}} {
		t.Run(name, func(t *testing.T) {
			p := &prAt{err: c.err}
			var logged []string
			s := branchPRServer(t, p, &GitInfo{Branch: "feat/a", Web: "https://github.com/me/vibe"}, &now, &logged)
			for i := 0; i < 3; i++ {
				if v, ok := stateAfterAsk(t, s)["branchPullRequest"]; ok && v != nil {
					t.Errorf("branchPullRequest = %v", v)
				}
				now = now.Add(3 * time.Minute)
			}
			if p.times() != 3 || len(logged) != c.logs {
				t.Errorf("asked %d times, logged %q", p.times(), logged)
			}
		})
	}
}

func TestTheStateCarriesThePullRequestsStatusAndDraft(t *testing.T) {
	p := &prAt{open: map[string]*BranchPullRequest{"feat/a": {Number: 41, URL: "https://github.com/me/vibe/pull/41", Draft: true,
		Status: PRFailing, Why: "test (windows-latest) failed"}}}
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	var logged []string
	s := branchPRServer(t, p, &GitInfo{Branch: "feat/a", Web: "https://github.com/me/vibe"}, &now, &logged)
	pr, _ := stateAfterAsk(t, s)["branchPullRequest"].(map[string]interface{})
	if pr == nil || pr["draft"] != true || pr["status"] != PRFailing || pr["why"] != "test (windows-latest) failed" {
		t.Errorf("branchPullRequest = %v", pr)
	}
}

// While checks run, the dot is asked about every 30 s, so it turns green or red soon after they end; settled, every minute.
func TestAPendingPullRequestIsAskedAboutAgainSooner(t *testing.T) {
	p := &prAt{open: map[string]*BranchPullRequest{"feat/a": {Number: 41, URL: "https://github.com/me/vibe/pull/41", Status: PRPending}}}
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	var logged []string
	s := branchPRServer(t, p, &GitInfo{Branch: "feat/a", Web: "https://github.com/me/vibe"}, &now, &logged)
	stateAfterAsk(t, s)
	now = now.Add(20 * time.Second)
	stateAfterAsk(t, s)
	if p.times() != 1 {
		t.Fatalf("asked %d times within 30 s", p.times())
	}
	now = now.Add(11 * time.Second)
	stateAfterAsk(t, s)
	if p.times() != 2 {
		t.Fatalf("asked %d times after 30 s while pending, want 2", p.times())
	}
	p.mu.Lock()
	p.open["feat/a"].Status = PRPassing
	p.mu.Unlock()
	now = now.Add(31 * time.Second)
	stateAfterAsk(t, s)
	now = now.Add(31 * time.Second)
	stateAfterAsk(t, s)
	if p.times() != 3 {
		t.Errorf("asked %d times once passing, want 3 (no more till a minute)", p.times())
	}
}
