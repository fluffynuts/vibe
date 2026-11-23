package companion

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A bare #N in the agent's words links to pull request N when the repository has one (and no Review comment has the number): the
// page asks which numbers are pull requests here, and the host asks the sandbox's git (git ls-remote lists every one, with the
// sandbox's own access) only while it is running, at most once a minute, in the background. The page never waits on sbx.

// prAskEvery is how often the sandbox may be asked again, for a number the list doesn't have (a pull request raised since).
const prAskEvery = time.Minute

// prAskWithin is how long one ask may take: ls-remote on a repository with thousands of pull requests took under a second.
const prAskWithin = 30 * time.Second

// maxWanted is the most numbers a page may ask about at once.
const maxWanted = 200

var pullRequestsFeature = feature{func(s *Server) bool { return s.cfg.PullRequests != nil && s.cfg.Workspace != "" },
	"this page can't ask which pull requests the repository has", 1 << 10}

// pullRequests is what the sandbox last said of the repository's pull requests.
type pullRequests struct {
	mu      sync.Mutex
	numbers map[int]bool
	// version changes whenever numbers does (0: never read): the page's state carries it, so the page fetches the list again.
	version int
	// unavailable is why the last ask gave no list ("" when it did).
	unavailable string
	askedAt     time.Time
	asking      bool
	// flight is the ask under way, for tests to wait on.
	flight sync.WaitGroup
}

func (p *pullRequests) currentVersion() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.version
}

// pullRequestList answers the page with the numbers known to be pull requests. Any number in want the list doesn't have asks the
// sandbox again, in the background, when it hasn't been asked in the last prAskEvery: the state's version says when the answer
// is in.
func (s *Server) pullRequestList(w http.ResponseWriter, r *http.Request) {
	want, ok := wantedNumbers(r.URL.Query().Get("want"))
	if !ok {
		writeError(w, http.StatusBadRequest, "want is up to 200 numbers, separated by commas")
		return
	}
	p := &s.prs
	p.mu.Lock()
	missing := false
	for _, n := range want {
		missing = missing || !p.numbers[n]
	}
	if missing {
		s.askPullRequestsLocked()
	}
	numbers := make([]int, 0, len(p.numbers))
	for n := range p.numbers {
		numbers = append(numbers, n)
	}
	body := map[string]interface{}{"numbers": numbers, "version": p.version, "unavailable": p.unavailable}
	p.mu.Unlock()
	sort.Ints(numbers)
	writeJSON(w, http.StatusOK, body)
}

func wantedNumbers(s string) ([]int, bool) {
	if s == "" {
		return nil, true
	}
	parts := strings.Split(s, ",")
	if len(parts) > maxWanted {
		return nil, false
	}
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// askPullRequestsLocked starts asking the sandbox, unless an ask is under way or one began in the last prAskEvery. s.prs.mu is
// held.
func (s *Server) askPullRequestsLocked() {
	p := &s.prs
	now := s.clock()
	if p.asking || (!p.askedAt.IsZero() && now.Sub(p.askedAt) < prAskEvery) {
		return
	}
	p.asking, p.askedAt = true, now
	p.flight.Add(1)
	go func() {
		defer p.flight.Done()
		numbers, err := s.readPullRequests()
		p.mu.Lock()
		defer p.mu.Unlock()
		p.asking = false
		switch {
		case err != nil:
			// A list already read is kept: a pull request doesn't stop being one.
			p.unavailable = err.Error()
			if !errors.Is(err, ErrNotRunning) && !errors.Is(err, errNotOnGitHub) && s.cfg.Log != nil {
				s.cfg.Log("companion page: couldn't read the repository's pull requests: " + err.Error())
			}
		default:
			p.unavailable = ""
			if !sameNumbers(p.numbers, numbers) {
				p.numbers = map[int]bool{}
				for _, n := range numbers {
					p.numbers[n] = true
				}
				p.version++
			}
		}
	}()
}

var errNotOnGitHub = errors.New("the folder's repository isn't on GitHub")

func (s *Server) readPullRequests() ([]int, error) {
	git := s.gitInfo()
	if git == nil || git.Web == "" {
		return nil, errNotOnGitHub
	}
	ctx, cancel := context.WithTimeout(s.lifeCtx, prAskWithin)
	defer cancel()
	return s.cfg.PullRequests(ctx, git.Web)
}

func sameNumbers(had map[int]bool, now []int) bool {
	if had == nil || len(had) != len(now) {
		return false
	}
	for _, n := range now {
		if !had[n] {
			return false
		}
	}
	return true
}

var pullRef = regexp.MustCompile(`^[0-9a-f]+\trefs/pull/(\d+)/head$`)

// parsePullRequestRefs is the pull requests' numbers in what `git ls-remote <repo> 'refs/pull/*/head'` printed.
func parsePullRequestRefs(out string) []int {
	var numbers []int
	for _, line := range strings.Split(out, "\n") {
		if m := pullRef.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				numbers = append(numbers, n)
			}
		}
	}
	return numbers
}

// PullRequestsFrom is Config.PullRequests asked of git: run (the sandbox's git on its folder) lists repo's pull requests with
// ls-remote, unless running says the sandbox isn't up (asking would start it).
func PullRequestsFrom(running func(ctx context.Context) bool, run func(ctx context.Context, args ...string) (string, error)) func(ctx context.Context, repo string) ([]int, error) {
	return func(ctx context.Context, repo string) ([]int, error) {
		if !running(ctx) {
			return nil, ErrNotRunning
		}
		out, err := run(ctx, "ls-remote", repo+".git", "refs/pull/*/head")
		if err != nil {
			return nil, err
		}
		return parsePullRequestRefs(out), nil
	}
}
