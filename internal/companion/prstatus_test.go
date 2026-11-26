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
		{"review required", PullRequestChecks{Checks: []Check{ok}, Review: "REVIEW_REQUIRED"}, PRPending, "review required"},
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
