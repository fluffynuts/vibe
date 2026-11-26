package companion

import (
	"context"
	"errors"
	"sync"
	"time"
)

// The top bar links the pull request open for the current branch, as the host's gh says (Config.BranchPullRequest): asked in the
// background, never waited on, for the branch and GitHub repository vibe already reads (see docs/plan-review-tab-enhancements.md).

// BranchPullRequest is the pull request open for a branch: whether it is a draft, and its status (PRPassing, PRPending, PRFailing)
// and why, in words (see PullRequestChecks).
type BranchPullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Draft  bool   `json:"draft,omitempty"`
	Status string `json:"status,omitempty"`
	Why    string `json:"why,omitempty"`
	// Running says a check is still running: it is asked about again sooner then (see branchPRPendingEvery). The page needn't know.
	Running bool `json:"-"`
}

// ErrNoGh is Config.BranchPullRequest's answer on a host without gh: nothing to show, and nothing to say about it.
var ErrNoGh = errors.New("gh isn't installed on this machine")

// branchPRAskEvery is how often the same branch is asked about again (a pull request opened, merged or closed since, a review given);
// branchPRPendingEvery, while one of its checks is running, so its dot turns green or red soon after they end. Pending on a review
// alone, news comes when someone acts, which the minute is soon enough for.
const (
	branchPRAskEvery     = time.Minute
	branchPRPendingEvery = 30 * time.Second
)

// branchPRAskWithin bounds one ask of gh, which goes to GitHub.
const branchPRAskWithin = 30 * time.Second

// branchPR is what gh last said of the current branch.
type branchPR struct {
	mu sync.Mutex
	// key is the repository and branch asked about; open, its pull request (nil for none).
	key     string
	open    *BranchPullRequest
	askedAt time.Time
	asking  bool
	// logged is the last failure logged, so the same one isn't logged every ask.
	logged string
	// flight is the ask under way, for tests to wait on.
	flight sync.WaitGroup
}

// branchPullRequest is the state's word on the current branch's pull request: the one last known for it, or nil; a branch not
// asked about, or not lately, is asked about in the background.
func (s *Server) branchPullRequest(git *GitInfo) *BranchPullRequest {
	if s.cfg.BranchPullRequest == nil || git == nil || git.Detached || git.Branch == "" || git.Web == "" {
		return nil
	}
	b := &s.branchPR
	key := git.Web + " " + git.Branch
	b.mu.Lock()
	defer b.mu.Unlock()
	now := s.clock()
	if key != b.key {
		b.key, b.open, b.askedAt = key, nil, time.Time{}
	}
	every := branchPRAskEvery
	if b.open != nil && b.open.Running {
		every = branchPRPendingEvery
	}
	if !b.asking && (b.askedAt.IsZero() || now.Sub(b.askedAt) >= every) {
		b.asking, b.askedAt = true, now
		b.flight.Add(1)
		go s.askBranchPullRequest(key, git.Web, git.Branch)
	}
	return b.open
}

func (s *Server) askBranchPullRequest(key, repo, branch string) {
	b := &s.branchPR
	defer b.flight.Done()
	ctx, cancel := context.WithTimeout(s.lifeCtx, branchPRAskWithin)
	defer cancel()
	open, err := s.cfg.BranchPullRequest(ctx, repo, branch)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.asking = false
	if key != b.key {
		// The branch changed while gh was asked: what it said is of another.
		return
	}
	b.open = open
	if err != nil {
		b.open = nil
		if !errors.Is(err, ErrNoGh) && err.Error() != b.logged {
			b.logged = err.Error()
			s.log("companion page: couldn't ask gh for the branch's pull request: " + err.Error())
		}
	}
}
