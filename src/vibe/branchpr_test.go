package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/companion"
)

// ghStandIns stands in for finding gh and running it, recording the folder and arguments it was run with.
func ghStandIns(t *testing.T, found bool, out string, err error) (dirs *[]string, args *[][]string) {
	t.Helper()
	oldLook, oldRun := lookGh, runGh
	t.Cleanup(func() { lookGh, runGh = oldLook, oldRun })
	lookGh = func() (string, error) {
		if !found {
			return "", errors.New("not found")
		}
		return "/usr/bin/gh", nil
	}
	dirs, args = &[]string{}, &[][]string{}
	runGh = func(_ context.Context, gh, dir string, a ...string) ([]byte, error) {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("gh ran in a folder with files in it: %s", dir)
		}
		*dirs = append(*dirs, dir)
		*args = append(*args, a)
		return []byte(out), err
	}
	return dirs, args
}

// prsSaid is gh api graphql's answer listing the pull requests nodes (JSON objects).
func prsSaid(nodes ...string) string {
	return `{"data":{"repository":{"pullRequests":{"nodes":[` + strings.Join(nodes, ",") + `]}}}}`
}

func TestTheBranchsPullRequestIsAskedOfGhByRepositoryAndBranch(t *testing.T) {
	_, args := ghStandIns(t, true, prsSaid(`{"number":40,"url":"https://github.com/me/vibe/pull/40"}`), nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "-feat/a")
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || pr.Number != 40 || pr.URL != "https://github.com/me/vibe/pull/40" {
		t.Errorf("pr = %+v", pr)
	}
	if len(*args) != 1 {
		t.Fatalf("gh ran %d times", len(*args))
	}
	a := *args
	got := strings.Join(a[0], " ")
	for _, want := range []string{"api graphql", "-f owner=me", "-f name=vibe", "-f head=-feat/a"} {
		if !strings.Contains(got, want) {
			t.Errorf("gh ran with %q, without %q", got, want)
		}
	}
	for _, field := range []string{"isCrossRepository", "isDraft", "reviewDecision", "mergeable", "reviewRequests", "latestReviews",
		"reviewThreads", "isResolved", "statusCheckRollup", "states: OPEN"} {
		if !strings.Contains(got, field) {
			t.Errorf("the query doesn't ask for %s", field)
		}
	}
}

// gh in the project would run git there, whose config the sandbox can write: it runs in an empty folder of its own, gone after.
func TestGhRunsOutsideTheProjectInAnEmptyFolder(t *testing.T) {
	dirs, _ := ghStandIns(t, true, prsSaid(), nil)
	ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "main")
	if len(*dirs) != 1 {
		t.Fatalf("ran %d times", len(*dirs))
	}
	if wd, _ := os.Getwd(); strings.HasPrefix((*dirs)[0], wd+string(filepath.Separator)) || (*dirs)[0] == wd {
		t.Errorf("ran in %s, inside %s", (*dirs)[0], wd)
	}
	if _, err := os.Stat((*dirs)[0]); !os.IsNotExist(err) {
		t.Errorf("its folder was left behind: %v", err)
	}
}

func TestNoOpenPullRequestIsNone(t *testing.T) {
	ghStandIns(t, true, prsSaid(), nil)
	if pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "main"); pr != nil || err != nil {
		t.Errorf("pr %+v, err %v", pr, err)
	}
}

func TestWithoutGhNothingIsAsked(t *testing.T) {
	_, args := ghStandIns(t, false, ``, nil)
	if _, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "main"); !errors.Is(err, companion.ErrNoGh) {
		t.Errorf("err = %v, want ErrNoGh", err)
	}
	if len(*args) != 0 {
		t.Errorf("gh ran: %q", *args)
	}
}

func TestGhFailingSaysWhat(t *testing.T) {
	ghStandIns(t, true, ``, errors.New("exit status 4: To get started with GitHub CLI, please run:  gh auth login"))
	if _, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "main"); err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("err = %v", err)
	}
}

// What gh says of the checks (check runs, and commit statuses, which name and say their state differently), the review and the merge
// becomes the pull request's status, and whether it is a draft.
func TestTheBranchsPullRequestCarriesItsStatusAndDraft(t *testing.T) {
	ghStandIns(t, true, prsSaid(`{"number":41,"url":"https://github.com/me/vibe/pull/41","isDraft":true,"reviewDecision":"","mergeable":"MERGEABLE",
		"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
			{"__typename":"CheckRun","name":"test (ubuntu-latest)","status":"COMPLETED","conclusion":"SUCCESS"},
			{"__typename":"CheckRun","name":"test (windows-latest)","status":"COMPLETED","conclusion":"FAILURE"},
			{"__typename":"StatusContext","context":"ci/legacy","state":"PENDING"}]}}}}]}}`), nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "feat/a")
	if err != nil {
		t.Fatal(err)
	}
	// A check still running: yellow, the failure said meanwhile (see companion.PullRequestChecks.Status).
	if pr == nil || !pr.Draft || pr.Status != companion.PRPending || !strings.Contains(pr.Why, "1 check running; test (windows-latest) failed") || !pr.Running {
		t.Errorf("pr = %+v", pr)
	}
}

// More checks than were read: how many, and GitHub's sum of them all, go with the pull request, so the unread ones count (#155).
func TestAPullRequestWithMoreChecksThanReadCountsTheRestByGitHubsSum(t *testing.T) {
	ghStandIns(t, true, prsSaid(`{"number":41,"url":"https://github.com/me/vibe/pull/41","mergeable":"MERGEABLE",
		"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE","contexts":{"totalCount":120,"nodes":[
			{"__typename":"CheckRun","name":"test (ubuntu-latest)","status":"COMPLETED","conclusion":"SUCCESS"}]}}}}]}}`), nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "feat/a")
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || pr.Status != companion.PRFailing || !strings.Contains(pr.Why, "119 more checks not read") {
		t.Errorf("pr = %+v", pr)
	}
}

// Reviews, requests and conversations, as GitHub's GraphQL gives them: a person by User, an agent by Bot (Copilot's reviewer), a
// request GitHub won't name as null.
func TestTheBranchsPullRequestCarriesItsReviewsAndConversations(t *testing.T) {
	ghStandIns(t, true, prsSaid(`{"number":41,"url":"https://github.com/me/vibe/pull/41","mergeable":"MERGEABLE",
		"reviewRequests":{"nodes":[{"requestedReviewer":{"__typename":"User","login":"bob"}},{"requestedReviewer":{"__typename":"Team","slug":"core"}},{"requestedReviewer":null}]},
		"latestReviews":{"nodes":[{"state":"APPROVED","author":{"__typename":"User","login":"alice"}},
			{"state":"COMMENTED","author":{"__typename":"Bot","login":"copilot-pull-request-reviewer"}}]},
		"reviewThreads":{"totalCount":2,"nodes":[
			{"isResolved":false,"comments":{"nodes":[{"author":{"__typename":"Bot","login":"copilot-pull-request-reviewer"}}]}},
			{"isResolved":true,"comments":{"nodes":[{"author":{"__typename":"User","login":"alice"}}]}}]}}`), nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "feat/a")
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || pr.Status != companion.PRPending {
		t.Fatalf("pr = %+v", pr)
	}
	for _, want := range []string{"waiting on @bob", "waiting on team core", "waiting on a reviewer", "1 conversation unresolved (copilot-pull-request-reviewer)"} {
		if !strings.Contains(pr.Why, want) {
			t.Errorf("why %q lacks %q", pr.Why, want)
		}
	}
}

// More conversations than were read: said, not guessed at.
func TestAPullRequestWithMoreConversationsThanReadSaysSo(t *testing.T) {
	ghStandIns(t, true, prsSaid(`{"number":41,"url":"https://github.com/me/vibe/pull/41","mergeable":"MERGEABLE",
		"reviewThreads":{"totalCount":101,"nodes":[]}}`), nil)
	pr, _ := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "feat/a")
	if pr == nil || pr.Status != companion.PRPending || !strings.Contains(pr.Why, "too many conversations") {
		t.Errorf("pr = %+v", pr)
	}
}

// --head matches a branch by its name alone, so a fork's pull request from a branch of the same name is listed too (on golang/go,
// --head=master gave three, all from forks): only one from this repository is this branch's.
func TestAForksPullRequestFromABranchOfTheSameNameIsNotThisBranchs(t *testing.T) {
	ghStandIns(t, true, prsSaid(`{"number":82519,"url":"https://github.com/me/vibe/pull/82519","isCrossRepository":true}`,
		`{"number":41,"url":"https://github.com/me/vibe/pull/41","isCrossRepository":false}`), nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "master")
	if err != nil || pr == nil || pr.Number != 41 {
		t.Errorf("pr = %+v, err %v; want PR 41, not the fork's", pr, err)
	}
	ghStandIns(t, true, prsSaid(`{"number":82519,"url":"https://github.com/me/vibe/pull/82519","isCrossRepository":true}`), nil)
	if pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "master"); pr != nil || err != nil {
		t.Errorf("only a fork's: pr = %+v, err %v; want none", pr, err)
	}
}
