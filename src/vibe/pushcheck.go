package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vibe/internal/gittoken"
	"vibe/internal/sbxrun"
)

// pushCheckWithin is how long the check from inside a sandbox may take: one exec, and one request to GitHub.
const pushCheckWithin = 60 * time.Second

// sandboxExec runs a command in a sandbox; tests stand in for it.
var sandboxExec = sbxrun.ExecStdout

// pushFromInside is checkPushFromInside; the tests of what calls it stand in for it.
var pushFromInside = checkPushFromInside

// insideCheck is what a push checked from inside a sandbox found: said, in the user's words ("" when there was nothing to check),
// and whether GitHub refused it (the sandbox has the token, and can't push with it).
type insideCheck struct {
	said    string
	refused bool
	// canPush says GitHub would have taken the push.
	canPush bool
}

// checkPushFromInside says whether sandbox name can push to repo with the token sbx gives it, as the agent would: GitHub is asked,
// from the sandbox's workspace, for a push to a branch that doesn't exist, which it refuses (403) before anything is sent when the
// token may not push, and which --dry-run then never sends. (GitHub's API won't say: its push permission is the account's, not
// the token's.) A sandbox that isn't running isn't asked: running anything in it would start it. "" with no GitHub repository.
func checkPushFromInside(name, target string, repo *gittoken.Repo) insideCheck {
	if repo == nil {
		return insideCheck{}
	}
	up, known := sandboxUp(context.Background(), name)
	switch {
	case !known:
		return insideCheck{said: "not checked from inside: sbx couldn't say whether it's running"}
	case !up:
		return insideCheck{said: "not checked from inside: it isn't running (it isn't started for this)"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushCheckWithin)
	defer cancel()
	ref := fmt.Sprintf("HEAD:refs/heads/vibe-push-check-%d", timeNow().UnixNano())
	// --no-verify: the repository's pre-push hook has no say in whether GitHub lets the token push, and can be slow, fail on
	// something else (seen: one requiring a clean tree, failing on a .pnpm-store folder, until the check ran out of time), or ask.
	_, err := sandboxExec(ctx, name, "env", "GIT_TERMINAL_PROMPT=0", "git", "-C", target, "push", "--dry-run", "--no-verify", "--quiet",
		"https://github.com/"+repo.String()+".git", ref)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = context.DeadlineExceeded
	}
	return pushCheckVerdict(err)
}

// pushCheckVerdict is what the dry-run push's outcome means.
func pushCheckVerdict(err error) insideCheck {
	if err == nil {
		return insideCheck{said: "the sandbox can push", canPush: true}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return insideCheck{said: "not checked from inside: it didn't answer"}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "src refspec") && strings.Contains(msg, "does not match"):
		return insideCheck{said: "not checked from inside: the repository has no commit yet"}
	case strings.Contains(msg, "error: 403") || strings.Contains(msg, "denied"):
		return insideCheck{said: "GitHub refused the sandbox a push: " + firstLineWith(msg, "denied", "403"), refused: true}
	}
	return insideCheck{said: "not checked from inside: " + strings.TrimSpace(lastLine(msg))}
}

// firstLineWith is the first line of msg having any of words in it (git's own line saying why), without the exit status before it.
func firstLineWith(msg string, words ...string) string {
	for _, line := range strings.Split(msg, "\n") {
		for _, w := range words {
			if strings.Contains(line, w) {
				return strings.TrimSpace(trimExitStatus(line))
			}
		}
	}
	return strings.TrimSpace(lastLine(msg))
}

func lastLine(msg string) string {
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	return trimExitStatus(lines[len(lines)-1])
}

// trimExitStatus drops sbx's "exit status N: " from the front of a line.
func trimExitStatus(line string) string {
	if strings.HasPrefix(line, "exit status ") {
		if _, rest, ok := strings.Cut(line, ": "); ok {
			return rest
		}
	}
	return line
}

// forReadOnly is what a check from inside means for a sandbox given its token read-only: GitHub refusing the push is what was
// chosen, not a problem; GitHub letting it push means the token can do more than vibe recorded, which is said plainly. A check that
// didn't happen is left as it is.
func forReadOnly(c insideCheck) insideCheck {
	switch {
	case c.refused:
		return insideCheck{said: "read-only, as chosen: GitHub refuses the sandbox a push"}
	case c.canPush:
		return insideCheck{said: "WARNING: given read-only, but the sandbox can push: the token can do more than vibe recorded"}
	}
	return c
}
