package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The script that re-runs a CI job which never got a runner (.github/scripts/rerun-if-no-runner.sh) decides from the jobs GitHub lists for
// the run. A job that ran and failed has a runner and steps, and is the test's to show; one that never got a runner has neither, and
// has been waiting. These pin that line, with a fake `gh` that answers from a fixture and records what it was asked to re-run.

type fakeJob struct {
	id         int
	conclusion string
	runner     int
	steps      int
	waited     string // how long it was queued, as completed_at after created_at
}

func jobsJSON(jobs ...fakeJob) string {
	type step struct{}
	var list []map[string]any
	for _, j := range jobs {
		steps := make([]step, j.steps)
		completed := "2026-11-12T17:06:20Z"
		switch j.waited {
		case "15m":
			completed = "2026-11-12T17:21:14Z"
		case "3m":
			completed = "2026-11-12T17:09:14Z"
		}
		list = append(list, map[string]any{
			"id": j.id, "name": fmt.Sprintf("test (job %d)", j.id), "conclusion": j.conclusion, "runner_id": j.runner,
			"steps": steps, "created_at": "2026-11-12T17:06:14Z", "started_at": "2026-11-12T17:06:14Z", "completed_at": completed,
		})
	}
	out, _ := json.Marshal(map[string]any{"jobs": list})
	return string(out)
}

// rerunsFor runs the script for a run on its attempt-th attempt, whose jobs are as given, and says which re-run commands it made.
func rerunsFor(t *testing.T, attempt int, jobs ...fakeJob) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a bash script, run by Linux's and macOS's CI")
	}
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on PATH", tool)
		}
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	fixture := filepath.Join(dir, "jobs.json")
	writeFile(t, fixture, jobsJSON(jobs...))
	bin := filepath.Join(dir, "bin")
	writeFile(t, filepath.Join(bin, "gh"), `#!/usr/bin/env bash
case "$1 $2" in
  "api repos/o/r/actions/runs/42/attempts/`+fmt.Sprint(attempt)+`/jobs"*) cat "$FAKE_JOBS" ;;
  "run rerun") echo "$*" >> "$FAKE_LOG" ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`)
	if err := os.Chmod(filepath.Join(bin, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), ".github", "scripts", "rerun-if-no-runner.sh"), "42", fmt.Sprint(attempt))
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_REPO=o/r", "FAKE_JOBS="+fixture, "FAKE_LOG="+log)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the script failed: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(log)
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(data)), "\n", " ; "))
}

func TestAJobThatNeverGotARunnerAndWaitedIsRerunAloneWhateverItsConclusion(t *testing.T) {
	for _, conclusion := range []string{"failure", "cancelled", "timed_out"} {
		t.Run(conclusion, func(t *testing.T) {
			got := rerunsFor(t, 1,
				fakeJob{id: 1, conclusion: "success", runner: 7, steps: 11},
				fakeJob{id: 2, conclusion: conclusion, runner: 0, steps: 0, waited: "15m"},
			)
			if want := "run rerun --job 2"; strings.Join(got, " ") != want {
				t.Errorf("re-ran %q, want %q", strings.Join(got, " "), want)
			}
		})
	}
}

func TestAFailureOfAJobThatRanIsLeftForTheTestToShow(t *testing.T) {
	if got := rerunsFor(t, 1, fakeJob{id: 1, conclusion: "failure", runner: 7, steps: 11}); len(got) != 0 {
		t.Errorf("re-ran %v: a test that failed is not a runner that never came", got)
	}
}

func TestNothingIsRerunWhenATestFailedBesideAJobThatNeverGotARunner(t *testing.T) {
	got := rerunsFor(t, 1,
		fakeJob{id: 1, conclusion: "failure", runner: 7, steps: 11},
		fakeJob{id: 2, conclusion: "cancelled", runner: 0, steps: 0, waited: "15m"},
	)
	if len(got) != 0 {
		t.Errorf("re-ran %v: a real failure is waiting to be looked at, and a re-run would bury it", got)
	}
}

func TestAJobCancelledSoonAfterItWasQueuedIsTheUsersAndIsLeftAlone(t *testing.T) {
	if got := rerunsFor(t, 1, fakeJob{id: 2, conclusion: "cancelled", runner: 0, steps: 0, waited: "3m"}); len(got) != 0 {
		t.Errorf("re-ran %v: someone cancelled it, and did not wait long for a runner", got)
	}
}

func TestAJobCancelledAfterItRanIsTheUsersAndIsLeftAlone(t *testing.T) {
	if got := rerunsFor(t, 1, fakeJob{id: 2, conclusion: "cancelled", runner: 7, steps: 5, waited: "15m"}); len(got) != 0 {
		t.Errorf("re-ran %v", got)
	}
}

func TestItKeepsTryingUntilTheTenthAttemptAndThenGivesUp(t *testing.T) {
	starved := fakeJob{id: 2, conclusion: "failure", runner: 0, steps: 0, waited: "15m"}
	if got := rerunsFor(t, 10, starved); len(got) != 0 {
		t.Errorf("re-ran %v on the tenth attempt: a runner that never comes is not to be waited for for ever", got)
	}
	for _, attempt := range []int{2, 3, 9} {
		if got := strings.Join(rerunsFor(t, attempt, starved), " "); got != "run rerun --job 2" {
			t.Errorf("re-ran %q on attempt %d, want job 2 re-run", got, attempt)
		}
	}
}
