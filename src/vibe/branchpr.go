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
		"--state=open", "--json=number,url", "--limit=1")
	if err != nil {
		return nil, err
	}
	var open []companion.BranchPullRequest
	if err := json.Unmarshal(out, &open); err != nil {
		return nil, fmt.Errorf("gh said something unexpected: %q", strings.TrimSpace(string(out)))
	}
	if len(open) == 0 {
		return nil, nil
	}
	return &open[0], nil
}
