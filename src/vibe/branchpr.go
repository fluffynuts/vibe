package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"vibe/internal/companion"
)

// lookGh finds the host's gh (a variable, for tests).
var lookGh = func() (string, error) { return exec.LookPath("gh") }

// runGh runs gh in dir, giving what it printed; a failure carries what it said (a variable, for tests).
var runGh = func(ctx context.Context, gh, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, gh, args...)
	cmd.Dir = dir
	// Nobody is there to answer a prompt, and an update notice would be read as its answer.
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

// ghBranchPullRequest is companion.Config.BranchPullRequest asked of the host's gh: the pull request open for branch in repo
// (https://github.com/owner/name). gh runs in an empty folder of its own, never the project's: there it would run git, and the
// project's .git/config can be written from inside the sandbox (core.fsmonitor and the like run commands on this machine). The
// repository and branch go as --repo and --head, so gh needs no repository around it.
func ghBranchPullRequest(ctx context.Context, repo, branch string) (*companion.BranchPullRequest, error) {
	gh, err := lookGh()
	if err != nil {
		return nil, companion.ErrNoGh
	}
	dir, err := os.MkdirTemp("", "vibe-gh-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	owner, name, _ := strings.Cut(strings.TrimPrefix(repo, "https://github.com/"), "/")
	out, err := runGh(ctx, gh, dir, "api", "graphql", "-f", "query="+branchPullRequestsQuery, "-f", "owner="+owner, "-f", "name="+name, "-f", "head="+branch)
	if err != nil {
		return nil, err
	}
	var said struct {
		Data struct {
			Repository struct {
				PullRequests struct {
					Nodes []ghPullRequest `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &said); err != nil {
		return nil, fmt.Errorf("gh said something unexpected: %q", strings.TrimSpace(string(out)))
	}
	// headRefName matches a branch by its name alone: a fork's pull request from a branch of the same name (master, say) is listed
	// too, and isn't this branch's.
	for _, pr := range said.Data.Repository.PullRequests.Nodes {
		if !pr.IsCrossRepository {
			return pr.pullRequest(), nil
		}
	}
	return nil, nil
}

// maxThreads is the most review conversations read; past it the dot says so rather than guess.
const maxThreads = 100

// branchPullRequestsQuery asks GitHub for the open pull requests from a branch, with all the dot needs: checks, reviews (each
// reviewer's latest, telling people from agents by account type), reviews asked for, and the conversations, resolved or not.
var branchPullRequestsQuery = `query($owner: String!, $name: String!, $head: String!) {
  repository(owner: $owner, name: $name) {
    pullRequests(headRefName: $head, states: OPEN, first: 20) {
      nodes {
        number url isDraft isCrossRepository reviewDecision mergeable
        reviewRequests(first: 20) { nodes { requestedReviewer { __typename ... on User { login } ... on Team { slug } ... on Bot { login } ... on Mannequin { login } } } }
        latestReviews(first: 50) { nodes { state author { __typename login } } }
        reviewThreads(first: ` + strconv.Itoa(maxThreads) + `) { totalCount nodes { isResolved comments(first: 1) { nodes { author { __typename login } } } } }
        commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
          __typename ... on CheckRun { name status conclusion } ... on StatusContext { context state } } } } } } }
      }
    }
  }
}`

// ghAuthor is who wrote a review or comment: Bot for an agent (Copilot, a GitHub App); null for a deleted account.
type ghAuthor struct {
	Type  string `json:"__typename"`
	Login string `json:"login"`
}

func (a *ghAuthor) name() (string, bool) {
	if a == nil {
		return "ghost", false
	}
	return a.Login, a.Type == "Bot"
}

// ghPullRequest is a pull request as the query gives it.
type ghPullRequest struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	IsDraft           bool   `json:"isDraft"`
	ReviewDecision    string `json:"reviewDecision"`
	Mergeable         string `json:"mergeable"`
	ReviewRequests    struct {
		Nodes []struct {
			Requested *struct {
				Type  string `json:"__typename"`
				Login string `json:"login"`
				Slug  string `json:"slug"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	LatestReviews struct {
		Nodes []struct {
			State  string    `json:"state"`
			Author *ghAuthor `json:"author"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	ReviewThreads struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			IsResolved bool `json:"isResolved"`
			Comments   struct {
				Nodes []struct {
					Author *ghAuthor `json:"author"`
				} `json:"nodes"`
			} `json:"comments"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					Contexts struct {
						Nodes []struct {
							Type       string `json:"__typename"`
							Name       string `json:"name"`
							Status     string `json:"status"`
							Conclusion string `json:"conclusion"`
							Context    string `json:"context"`
							State      string `json:"state"`
						} `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

func (g ghPullRequest) pullRequest() *companion.BranchPullRequest {
	checks := companion.PullRequestChecks{Review: g.ReviewDecision, Mergeable: g.Mergeable,
		ThreadsCut: g.ReviewThreads.TotalCount > len(g.ReviewThreads.Nodes)}
	for _, c := range g.Commits.Nodes {
		if c.Commit.Rollup == nil {
			continue
		}
		for _, n := range c.Commit.Rollup.Contexts.Nodes {
			if n.Type == "StatusContext" {
				checks.Checks = append(checks.Checks, companion.Check{Name: n.Context, State: n.State})
			} else {
				checks.Checks = append(checks.Checks, companion.Check{Name: n.Name, Status: n.Status, Conclusion: n.Conclusion})
			}
		}
	}
	for _, r := range g.ReviewRequests.Nodes {
		switch {
		case r.Requested == nil:
			checks.Requests = append(checks.Requests, "")
		case r.Requested.Type == "Team":
			checks.Requests = append(checks.Requests, "team "+r.Requested.Slug)
		case r.Requested.Type == "Bot":
			checks.Requests = append(checks.Requests, r.Requested.Login)
		default:
			checks.Requests = append(checks.Requests, "@"+r.Requested.Login)
		}
	}
	for _, r := range g.LatestReviews.Nodes {
		author, bot := r.Author.name()
		checks.Reviews = append(checks.Reviews, companion.Review{Author: author, Bot: bot, State: r.State})
	}
	for _, t := range g.ReviewThreads.Nodes {
		author, bot := "ghost", false
		if len(t.Comments.Nodes) > 0 {
			author, bot = t.Comments.Nodes[0].Author.name()
		}
		checks.Threads = append(checks.Threads, companion.Thread{Author: author, Bot: bot, Resolved: t.IsResolved})
	}
	status, why := checks.Status()
	return &companion.BranchPullRequest{Number: g.Number, URL: g.URL, Draft: g.IsDraft, Status: status, Why: why, Running: checks.Running()}
}
