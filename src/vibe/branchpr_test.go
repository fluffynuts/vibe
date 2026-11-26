package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestTheBranchsPullRequestIsAskedOfGhByRepositoryAndBranch(t *testing.T) {
	_, args := ghStandIns(t, true, `[{"number":40,"url":"https://github.com/me/vibe/pull/40"}]`, nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "-feat/a")
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || pr.Number != 40 || pr.URL != "https://github.com/me/vibe/pull/40" {
		t.Errorf("pr = %+v", pr)
	}
	want := [][]string{{"pr", "list", "--repo=me/vibe", "--head=-feat/a", "--state=open",
		"--json=number,url,isCrossRepository,isDraft,reviewDecision,mergeable,statusCheckRollup", "--limit=20"}}
	if !reflect.DeepEqual(*args, want) {
		t.Errorf("gh ran with %q, want %q", *args, want)
	}
}

// gh in the project would run git there, whose config the sandbox can write: it runs in an empty folder of its own, gone after.
func TestGhRunsOutsideTheProjectInAnEmptyFolder(t *testing.T) {
	dirs, _ := ghStandIns(t, true, `[]`, nil)
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
	ghStandIns(t, true, `[]`, nil)
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
	ghStandIns(t, true, `[{"number":41,"url":"https://github.com/me/vibe/pull/41","isDraft":true,"reviewDecision":"","mergeable":"MERGEABLE",
		"statusCheckRollup":[
			{"__typename":"CheckRun","name":"test (ubuntu-latest)","status":"COMPLETED","conclusion":"SUCCESS"},
			{"__typename":"CheckRun","name":"test (windows-latest)","status":"COMPLETED","conclusion":"FAILURE"},
			{"__typename":"StatusContext","context":"ci/legacy","state":"PENDING"}]}]`, nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "feat/a")
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || !pr.Draft || pr.Status != companion.PRFailing || !strings.Contains(pr.Why, "test (windows-latest) failed") || !strings.Contains(pr.Why, "1 check running") || !pr.Running {
		t.Errorf("pr = %+v", pr)
	}
}

// --head matches a branch by its name alone, so a fork's pull request from a branch of the same name is listed too (on golang/go,
// --head=master gave three, all from forks): only one from this repository is this branch's.
func TestAForksPullRequestFromABranchOfTheSameNameIsNotThisBranchs(t *testing.T) {
	ghStandIns(t, true, `[
		{"number":82519,"url":"https://github.com/me/vibe/pull/82519","isCrossRepository":true},
		{"number":41,"url":"https://github.com/me/vibe/pull/41","isCrossRepository":false}]`, nil)
	pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "master")
	if err != nil || pr == nil || pr.Number != 41 {
		t.Errorf("pr = %+v, err %v; want PR 41, not the fork's", pr, err)
	}
	ghStandIns(t, true, `[{"number":82519,"url":"https://github.com/me/vibe/pull/82519","isCrossRepository":true}]`, nil)
	if pr, err := ghBranchPullRequest(context.Background(), "https://github.com/me/vibe", "master"); pr != nil || err != nil {
		t.Errorf("only a fork's: pr = %+v, err %v; want none", pr, err)
	}
}
