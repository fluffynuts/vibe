package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	out, err := runGh(ctx, gh, dir, "pr", "list", "--repo="+strings.TrimPrefix(repo, "https://github.com/"), "--head="+branch,
		"--state=open", "--json=number,url,isCrossRepository,isDraft,reviewDecision,mergeable,statusCheckRollup", "--limit=20")
	if err != nil {
		return nil, err
	}
	var open []ghPullRequest
	if err := json.Unmarshal(out, &open); err != nil {
		return nil, fmt.Errorf("gh said something unexpected: %q", strings.TrimSpace(string(out)))
	}
	// --head matches a branch by its name alone: a fork's pull request from a branch of the same name (master, say) is listed too,
	// and isn't this branch's.
	for _, pr := range open {
		if !pr.IsCrossRepository {
			return pr.pullRequest(), nil
		}
	}
	return nil, nil
}

// ghPullRequest is a pull request as gh pr list --json gives it.
type ghPullRequest struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	IsDraft           bool   `json:"isDraft"`
	ReviewDecision    string `json:"reviewDecision"`
	Mergeable         string `json:"mergeable"`
	// StatusCheckRollup holds check runs (name, status, conclusion) and commit statuses (context, state), told apart by __typename.
	StatusCheckRollup []struct {
		Type       string `json:"__typename"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Context    string `json:"context"`
		State      string `json:"state"`
	} `json:"statusCheckRollup"`
}

func (g ghPullRequest) pullRequest() *companion.BranchPullRequest {
	checks := companion.PullRequestChecks{Review: g.ReviewDecision, Mergeable: g.Mergeable}
	for _, c := range g.StatusCheckRollup {
		if c.Type == "StatusContext" {
			checks.Checks = append(checks.Checks, companion.Check{Name: c.Context, State: c.State})
		} else {
			checks.Checks = append(checks.Checks, companion.Check{Name: c.Name, Status: c.Status, Conclusion: c.Conclusion})
		}
	}
	status, why := checks.Status()
	return &companion.BranchPullRequest{Number: g.Number, URL: g.URL, Draft: g.IsDraft, Status: status, Why: why, Running: checks.Running()}
}
