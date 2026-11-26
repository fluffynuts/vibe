package companion

import (
	"fmt"
	"strings"
)

// A pull request's status, for the dot beside its link in the top bar: failing (red) when a check failed, a review asked for changes
// or it conflicts with its base; pending (yellow) when a check is still running, a review is required and not given, or GitHub hasn't
// worked out yet whether it merges; else passing (green).
const (
	PRPassing = "passing"
	PRPending = "pending"
	PRFailing = "failing"
)

// PullRequestChecks are what GitHub says of a pull request's checks, reviews and merge.
type PullRequestChecks struct {
	// Review is GitHub's review decision: APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or "" (no review asked for).
	Review string
	// Mergeable is MERGEABLE, CONFLICTING or UNKNOWN (not worked out yet).
	Mergeable string
	Checks    []Check
}

// Check is one check on a pull request: a check run (Status, then Conclusion once COMPLETED) or a commit status (State).
type Check struct {
	Name       string
	Status     string
	Conclusion string
	State      string
}

// failedConclusions are the conclusions of a check run that has failed, in GitHub's words; any other (SUCCESS, NEUTRAL, SKIPPED)
// let it pass.
var failedConclusions = map[string]bool{"FAILURE": true, "TIMED_OUT": true, "CANCELLED": true, "ACTION_REQUIRED": true, "STARTUP_FAILURE": true}

// Running says one of the checks hasn't finished: a check run not yet completed, or a commit status pending.
func (c PullRequestChecks) Running() bool {
	for _, check := range c.Checks {
		if check.State != "" {
			if check.State == "PENDING" || check.State == "EXPECTED" {
				return true
			}
		} else if check.Status != "COMPLETED" {
			return true
		}
	}
	return false
}

// Status is the pull request's status, and why, in words.
func (c PullRequestChecks) Status() (status, why string) {
	var failed []string
	running, passed := 0, 0
	for _, check := range c.Checks {
		switch {
		case check.State != "":
			switch check.State {
			case "FAILURE", "ERROR":
				failed = append(failed, check.Name)
			case "PENDING", "EXPECTED":
				running++
			default:
				passed++
			}
		case check.Status != "COMPLETED":
			running++
		case failedConclusions[check.Conclusion]:
			failed = append(failed, check.Name)
		default:
			passed++
		}
	}
	var bad, waiting []string
	for _, name := range failed {
		bad = append(bad, name+" failed")
	}
	if c.Review == "CHANGES_REQUESTED" {
		bad = append(bad, "changes requested")
	}
	if c.Mergeable == "CONFLICTING" {
		bad = append(bad, "conflicts with its base branch")
	}
	if running > 0 {
		waiting = append(waiting, plural(running, "check")+" running")
	}
	if c.Review == "REVIEW_REQUIRED" {
		waiting = append(waiting, "review required")
	}
	if c.Mergeable == "UNKNOWN" {
		waiting = append(waiting, "GitHub is still working out its mergeability")
	}
	switch {
	case len(bad) > 0:
		return PRFailing, strings.Join(append(bad, waiting...), "; ")
	case len(waiting) > 0:
		return PRPending, strings.Join(waiting, "; ")
	}
	var good []string
	if passed > 0 {
		good = append(good, plural(passed, "check")+" passed")
	} else {
		good = append(good, "no checks")
	}
	if c.Review == "APPROVED" {
		good = append(good, "approved")
	}
	return PRPassing, strings.Join(good, "; ")
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}
