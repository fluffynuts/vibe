package companion

import (
	"strings"
	"testing"
)

func TestAPullRequestsStatusIsWorkedOutFromItsChecksReviewAndMerge(t *testing.T) {
	ok := Check{Name: "test (ubuntu-latest)", Status: "COMPLETED", Conclusion: "SUCCESS"}
	skipped := Check{Name: "release", Status: "COMPLETED", Conclusion: "SKIPPED"}
	running := Check{Name: "test (macos-latest)", Status: "IN_PROGRESS"}
	failed := Check{Name: "test (windows-latest)", Status: "COMPLETED", Conclusion: "FAILURE"}
	statusOK := Check{Name: "ci/legacy", State: "SUCCESS"}
	statusPending := Check{Name: "ci/legacy", State: "PENDING"}
	statusError := Check{Name: "ci/legacy", State: "ERROR"}
	for _, c := range []struct {
		name   string
		checks PullRequestChecks
		status string
		whyHas string
	}{
		{"all passed", PullRequestChecks{Checks: []Check{ok, skipped, statusOK}, Mergeable: "MERGEABLE"}, PRPassing, "3 checks passed"},
		{"approved, no checks", PullRequestChecks{Review: "APPROVED", Mergeable: "MERGEABLE"}, PRPassing, "approved"},
		{"a check running", PullRequestChecks{Checks: []Check{ok, running}, Mergeable: "MERGEABLE"}, PRPending, "1 check running"},
		{"a status pending", PullRequestChecks{Checks: []Check{statusPending}}, PRPending, "1 check running"},
		// Gated, and nobody asked: no one will ever approve it, so it needs you (the user, 2026-11-26).
		{"review required, nobody asked", PullRequestChecks{Checks: []Check{ok}, Review: "REVIEW_REQUIRED"}, PRFailing, "review required, but nobody is asked"},
		{"review required, someone asked", PullRequestChecks{Checks: []Check{ok}, Review: "REVIEW_REQUIRED", Requests: []string{"@alice"}}, PRPending, "waiting on @alice"},
		{"merge not worked out yet", PullRequestChecks{Checks: []Check{ok}, Mergeable: "UNKNOWN"}, PRPending, "mergeability"},
		{"a check failed", PullRequestChecks{Checks: []Check{ok, running, failed}}, PRFailing, "test (windows-latest) failed"},
		{"a status errored", PullRequestChecks{Checks: []Check{statusError}}, PRFailing, "ci/legacy failed"},
		{"cancelled", PullRequestChecks{Checks: []Check{{Name: "build", Status: "COMPLETED", Conclusion: "CANCELLED"}}}, PRFailing, "build failed"},
		{"timed out", PullRequestChecks{Checks: []Check{{Name: "build", Status: "COMPLETED", Conclusion: "TIMED_OUT"}}}, PRFailing, "build failed"},
		{"changes requested", PullRequestChecks{Checks: []Check{ok}, Review: "CHANGES_REQUESTED"}, PRFailing, "changes requested"},
		{"a merge conflict", PullRequestChecks{Checks: []Check{ok}, Mergeable: "CONFLICTING"}, PRFailing, "conflicts"},
	} {
		t.Run(c.name, func(t *testing.T) {
			status, why := c.checks.Status()
			if status != c.status || !strings.Contains(why, c.whyHas) {
				t.Errorf("status %q, why %q; want %q, why with %q", status, why, c.status, c.whyHas)
			}
		})
	}
}

func TestAPullRequestSaysWhetherAnyOfItsChecksIsRunning(t *testing.T) {
	done := Check{Name: "a", Status: "COMPLETED", Conclusion: "SUCCESS"}
	for _, c := range []struct {
		name   string
		checks PullRequestChecks
		want   bool
	}{
		{"all done", PullRequestChecks{Checks: []Check{done}}, false},
		{"a check run in progress", PullRequestChecks{Checks: []Check{done, {Name: "b", Status: "IN_PROGRESS"}}}, true},
		{"a commit status pending", PullRequestChecks{Checks: []Check{{Name: "c", State: "PENDING"}}}, true},
		{"waiting on a review only", PullRequestChecks{Checks: []Check{done}, Review: "REVIEW_REQUIRED"}, false},
	} {
		if got := c.checks.Running(); got != c.want {
			t.Errorf("%s: Running() = %v, want %v", c.name, got, c.want)
		}
	}
}

// Reviews, by person and agent, and the conversations on the pull request (decided with the user, 2026-11-26, conservatively): green
// means every check passed, every human reviewer approved, every agent has reviewed, and no conversation is unresolved.
func TestAPullRequestsReviewersAndConversationsDecideItsStatus(t *testing.T) {
	ok := Check{Name: "test", Status: "COMPLETED", Conclusion: "SUCCESS"}
	approved := func(who string) Review { return Review{Author: who, State: "APPROVED"} }
	commented := func(who string) Review { return Review{Author: who, State: "COMMENTED"} }
	copilot := Review{Author: "copilot-pull-request-reviewer", Bot: true, State: "COMMENTED"}
	for _, c := range []struct {
		name   string
		checks PullRequestChecks
		status string
		whyHas string
	}{
		{"both people approved", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{approved("alice"), approved("bob")}}, PRPassing, "approved by @alice, @bob"},
		{"one of two approved, the other still asked", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{approved("alice")}, Requests: []string{"@bob"}}, PRPending, "waiting on @bob"},
		{"a person only commented", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{approved("alice"), commented("ceo")}}, PRPending, "@ceo commented without approving"},
		{"a person asked for changes", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{approved("alice"), {Author: "bob", State: "CHANGES_REQUESTED"}}}, PRFailing, "@bob requested changes"},
		{"an agent reviewed, its conversations resolved", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{approved("alice"), copilot},
			Threads: []Thread{{Author: "copilot-pull-request-reviewer", Bot: true, Resolved: true}}}, PRPassing, "approved by @alice"},
		{"an agent's conversation unresolved", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{approved("alice"), copilot},
			Threads: []Thread{{Author: "copilot-pull-request-reviewer", Bot: true}}}, PRPending, "1 conversation unresolved (copilot-pull-request-reviewer)"},
		{"a person's conversation unresolved, though they approved", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{approved("alice")},
			Threads: []Thread{{Author: "alice"}, {Author: "alice", Resolved: true}}}, PRPending, "1 conversation unresolved (@alice)"},
		{"an agent asked for changes", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{{Author: "some-bot", Bot: true, State: "CHANGES_REQUESTED"}}}, PRFailing, "some-bot requested changes"},
		{"a request GitHub won't name", PullRequestChecks{Checks: []Check{ok}, Requests: []string{""}}, PRPending, "waiting on a reviewer"},
		{"a team asked", PullRequestChecks{Checks: []Check{ok}, Requests: []string{"team core"}}, PRPending, "waiting on team core"},
		{"too many conversations to read", PullRequestChecks{Checks: []Check{ok}, ThreadsCut: true}, PRPending, "too many conversations to read"},
		{"a dismissed review is no review", PullRequestChecks{Checks: []Check{ok}, Reviews: []Review{{Author: "bob", State: "DISMISSED"}}}, PRPassing, "1 check passed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			status, why := c.checks.Status()
			if status != c.status || !strings.Contains(why, c.whyHas) {
				t.Errorf("status %q, why %q; want %q, why with %q", status, why, c.status, c.whyHas)
			}
		})
	}
}
