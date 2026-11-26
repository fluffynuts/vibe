package companion

import (
	"fmt"
	"strings"
)

// A pull request's status, for the dot beside its link in the top bar (decided with the user, 2026-11-26, conservatively):
//   - failing (red), something needs you, once no check is running: a check failed (or was cancelled), a review asked for changes, it
//     conflicts with its base, or a review is required and nobody is asked;
//   - pending (yellow), on its way: any check still running (the dot pulses), whatever else; or a review asked for and not given,
//     a person's review that only commented, any conversation unresolved, a review still required, or GitHub not yet sure it merges;
//   - passing (green), good to go: every check passed, every person who reviewed approved, every agent (a bot account: Copilot,
//     any GitHub App) has reviewed, and no conversation is unresolved.
const (
	PRPassing = "passing"
	PRPending = "pending"
	PRFailing = "failing"
)

// PullRequestChecks are what GitHub says of a pull request's checks, reviews, conversations and merge.
type PullRequestChecks struct {
	// Review is GitHub's review decision: APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or "" (no review required).
	Review string
	// Mergeable is MERGEABLE, CONFLICTING or UNKNOWN (not worked out yet).
	Mergeable string
	Checks    []Check
	// Requests are the reviews asked for and not yet given, as said: "@alice", "team core", "" for one GitHub won't name.
	Requests []string
	// Reviews are each reviewer's latest review.
	Reviews []Review
	// Threads are the review conversations; ThreadsCut says there were more than were read.
	Threads    []Thread
	ThreadsCut bool
}

// Check is one check on a pull request: a check run (Status, then Conclusion once COMPLETED) or a commit status (State).
type Check struct {
	Name       string
	Status     string
	Conclusion string
	State      string
}

// Review is a reviewer's latest review: State APPROVED, CHANGES_REQUESTED, COMMENTED, or DISMISSED (as none). Bot says the reviewer
// is an agent: its comments are dealt with by resolving their conversations, as it never approves.
type Review struct {
	Author string
	Bot    bool
	State  string
}

// Thread is a review conversation, by who started it.
type Thread struct {
	Author   string
	Bot      bool
	Resolved bool
}

// failedConclusions are the conclusions of a check run that has failed, in GitHub's words; any other (SUCCESS, NEUTRAL, SKIPPED)
// let it pass.
var failedConclusions = map[string]bool{"FAILURE": true, "TIMED_OUT": true, "CANCELLED": true, "ACTION_REQUIRED": true, "STARTUP_FAILURE": true}

// who is a reviewer as said: a person by @login, an agent by its account's name.
func who(author string, bot bool) string {
	if bot {
		return author
	}
	return "@" + author
}

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
	var bad, waiting, approvedBy []string
	for _, name := range failed {
		bad = append(bad, name+" failed")
	}
	reviewed, changes := false, false
	for _, r := range c.Reviews {
		switch r.State {
		case "CHANGES_REQUESTED":
			reviewed, changes = true, true
			bad = append(bad, who(r.Author, r.Bot)+" requested changes")
		case "APPROVED":
			reviewed = true
			if !r.Bot {
				approvedBy = append(approvedBy, who(r.Author, false))
			}
		case "COMMENTED":
			reviewed = true
			if !r.Bot {
				waiting = append(waiting, who(r.Author, false)+" commented without approving")
			}
		}
	}
	if c.Review == "CHANGES_REQUESTED" && !changes {
		bad = append(bad, "changes requested")
	}
	if c.Mergeable == "CONFLICTING" {
		bad = append(bad, "conflicts with its base branch")
	}
	if c.Review == "REVIEW_REQUIRED" && len(c.Requests) == 0 && !reviewed {
		bad = append(bad, "review required, but nobody is asked")
	}
	if running > 0 {
		waiting = append([]string{plural(running, "check") + " running"}, waiting...)
	}
	for _, r := range c.Requests {
		if r == "" {
			r = "a reviewer"
		}
		waiting = append(waiting, "waiting on "+r)
	}
	if c.Review == "REVIEW_REQUIRED" && (len(c.Requests) > 0 || reviewed) {
		waiting = append(waiting, "review required")
	}
	if open, by := unresolved(c.Threads); open > 0 {
		waiting = append(waiting, plural(open, "conversation")+" unresolved ("+strings.Join(by, ", ")+")")
	}
	if c.ThreadsCut {
		waiting = append(waiting, "too many conversations to read")
	}
	if c.Mergeable == "UNKNOWN" {
		waiting = append(waiting, "GitHub is still working out its mergeability")
	}
	switch {
	// While a check runs the dot is yellow, failures said in words: they may be what is being run again, and it is worked out
	// afresh when the checks end (the user's call, 2026-11-26).
	case running > 0:
		return PRPending, strings.Join(append(bad, waiting...), "; ")
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
	if len(approvedBy) > 0 {
		good = append(good, "approved by "+strings.Join(approvedBy, ", "))
	} else if c.Review == "APPROVED" {
		good = append(good, "approved")
	}
	return PRPassing, strings.Join(good, "; ")
}

// unresolved is how many conversations are unresolved, and who started them, each named once.
func unresolved(threads []Thread) (int, []string) {
	n := 0
	var by []string
	seen := map[string]bool{}
	for _, t := range threads {
		if t.Resolved {
			continue
		}
		n++
		if name := who(t.Author, t.Bot); !seen[name] {
			seen[name] = true
			by = append(by, name)
		}
	}
	return n, by
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}
