package companion

import (
	"fmt"
	"strings"
)

// A pull request's status, for the dot beside its link in the top bar (decided with the user, 2026-11-26, conservatively):
//   - failing (red), something needs you, once no check is running: a check failed (or was cancelled), a review asked for changes, it
//     conflicts with its base, or a review is required and nobody is asked;
//   - pending (yellow), on its way: any check still running (the dot pulses), whatever else; or a check waiting on a person (a
//     deployment's approval, a status expected), a review asked for and not given,
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
	// ChecksUnread is how many checks there were past those read, and ChecksSummary GitHub's sum of all of them (SUCCESS, FAILURE,
	// ERROR, PENDING or EXPECTED), which decides for the unread ones.
	ChecksUnread  int
	ChecksSummary string
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

// failedConclusions are the conclusions of a check run that hasn't passed, in GitHub's words; any other (SUCCESS, NEUTRAL, SKIPPED)
// let it pass. CANCELLED and STALE (a run GitHub gave up on after about two weeks unfinished) never ran to the end, so never passed.
var failedConclusions = map[string]bool{"FAILURE": true, "TIMED_OUT": true, "CANCELLED": true, "ACTION_REQUIRED": true, "STARTUP_FAILURE": true, "STALE": true}

// How a check stands, for the dot.
type checkStanding int

const (
	checkPassed checkStanding = iota
	checkFailed
	checkRunning
	// checkWaiting is a check held for a person, not a runner: a job waiting for a deployment's approval, or a commit status
	// required and not yet reported. It may wait for days, so it neither pulses the dot nor hides a failure.
	checkWaiting
)

func (check Check) standing() checkStanding {
	if check.State != "" {
		switch check.State {
		case "FAILURE", "ERROR":
			return checkFailed
		case "PENDING":
			return checkRunning
		case "EXPECTED":
			return checkWaiting
		}
		return checkPassed
	}
	switch {
	case check.Status == "WAITING":
		return checkWaiting
	case check.Status != "COMPLETED":
		return checkRunning
	case failedConclusions[check.Conclusion]:
		return checkFailed
	}
	return checkPassed
}

// waitingFor says what a waiting check waits for.
func (check Check) waitingFor() string {
	if check.State != "" {
		return check.Name + " expected, not reported yet"
	}
	return check.Name + " waiting for approval"
}

// unreadSummary is GitHub's sum of all the checks, said, when some weren't read and it isn't that they passed.
func (c PullRequestChecks) unreadSummary() (standing checkStanding, said string) {
	if c.ChecksUnread <= 0 {
		return checkPassed, ""
	}
	not := plural(c.ChecksUnread, "more check") + " not read; GitHub sums up all the checks as "
	switch c.ChecksSummary {
	case "SUCCESS":
		return checkPassed, ""
	case "FAILURE", "ERROR":
		return checkFailed, not + "failed"
	case "PENDING":
		return checkRunning, not + "running"
	case "EXPECTED":
		return checkWaiting, not + "expected"
	}
	return checkWaiting, plural(c.ChecksUnread, "more check") + " not read"
}

// who is a reviewer as said: a person by @login, an agent by its account's name.
func who(author string, bot bool) string {
	if bot {
		return author
	}
	return "@" + author
}

// Running says one of the checks is running: a check run queued or in progress, or a commit status pending; not one held for a person.
func (c PullRequestChecks) Running() bool {
	for _, check := range c.Checks {
		if check.standing() == checkRunning {
			return true
		}
	}
	unread, _ := c.unreadSummary()
	return unread == checkRunning
}

// Status is the pull request's status, and why, in words.
func (c PullRequestChecks) Status() (status, why string) {
	var bad, waiting, approvedBy []string
	running, passed := 0, 0
	for _, check := range c.Checks {
		switch check.standing() {
		case checkFailed:
			bad = append(bad, check.Name+" failed")
		case checkRunning:
			running++
		case checkWaiting:
			waiting = append(waiting, check.waitingFor())
		default:
			passed++
		}
	}
	var runningSaid []string
	if running > 0 {
		runningSaid = append(runningSaid, plural(running, "check")+" running")
	}
	switch unread, unreadSaid := c.unreadSummary(); unread {
	case checkPassed:
		passed += c.ChecksUnread
	case checkFailed:
		bad = append(bad, unreadSaid)
	case checkRunning:
		runningSaid = append(runningSaid, unreadSaid)
	default:
		waiting = append(waiting, unreadSaid)
	}
	// Only a person's review is on its way to approval: an agent never approves.
	personReviewed, changes := false, false
	for _, r := range c.Reviews {
		switch r.State {
		case "CHANGES_REQUESTED":
			changes = true
			personReviewed = personReviewed || !r.Bot
			bad = append(bad, who(r.Author, r.Bot)+" requested changes")
		case "APPROVED":
			if !r.Bot {
				personReviewed = true
				approvedBy = append(approvedBy, who(r.Author, false))
			}
		case "COMMENTED":
			if !r.Bot {
				personReviewed = true
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
	if c.Review == "REVIEW_REQUIRED" && len(c.Requests) == 0 && !personReviewed {
		bad = append(bad, "review required, but nobody is asked")
	}
	for _, r := range c.Requests {
		if r == "" {
			r = "a reviewer"
		}
		waiting = append(waiting, "waiting on "+r)
	}
	if c.Review == "REVIEW_REQUIRED" && (len(c.Requests) > 0 || personReviewed) {
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
	// While a check runs the dot is yellow, failures said in words after the running that makes it yellow: they may be what is
	// being run again, and it is worked out afresh when the checks end (the user's call, 2026-11-26).
	case len(runningSaid) > 0:
		return PRPending, strings.Join(append(append(runningSaid, bad...), waiting...), "; ")
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
