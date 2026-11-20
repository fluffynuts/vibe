package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vibe/internal/gittoken"
	"vibe/internal/state"
)

// What a dry-run push from inside the sandbox said, in the user's words. GitHub's answer to a push the token may not make is a 403
// at the very start, before anything would be sent; a repository with no commit has nothing to name.
func TestPushCheckVerdictSaysWhatTheDryRunPushFound(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"it went":       {nil, "the sandbox can push"},
		"refused":       {errors.New("exit status 128: remote: Permission to o/r.git denied to someone.\nfatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 403"), "GitHub refused the sandbox a push: remote: Permission to o/r.git denied to someone."},
		"no commit":     {errors.New("exit status 1: error: src refspec HEAD does not match any"), "not checked from inside: the repository has no commit yet"},
		"took too long": {context.DeadlineExceeded, "not checked from inside: it didn't answer"},
		"other":         {errors.New("exit status 128: fatal: not a git repository"), "not checked from inside: fatal: not a git repository"},
	} {
		got := pushCheckVerdict(tc.err)
		if got.said != tc.want {
			t.Errorf("%s: pushCheckVerdict said %q, want %q", name, got.said, tc.want)
		}
		if got.refused != (name == "refused") {
			t.Errorf("%s: refused = %v", name, got.refused)
		}
	}
}

// runsInSandbox stands in for sbx exec, keeping what was asked.
func runsInSandbox(t *testing.T, err error) *[][]string {
	t.Helper()
	var asked [][]string
	old := sandboxExec
	t.Cleanup(func() { sandboxExec = old })
	sandboxExec = func(_ context.Context, name string, args ...string) (string, error) {
		asked = append(asked, append([]string{name}, args...))
		return "", err
	}
	return &asked
}

func TestTheCheckFromInsideIsADryRunPushOverHTTPSFromTheWorkspaceToABranchNameThatSendsNothing(t *testing.T) {
	sandboxesUp(t, map[string]bool{"box": true}, true)
	asked := runsInSandbox(t, nil)
	got := checkPushFromInside("box", "/w/box", &gittoken.Repo{Owner: "o", Name: "r"})
	if got.said != "the sandbox can push" || got.refused {
		t.Errorf("checkPushFromInside = %+v", got)
	}
	if len(*asked) != 1 {
		t.Fatalf("asked sbx %d times, want once: %q", len(*asked), *asked)
	}
	cmd := strings.Join((*asked)[0], " ")
	for _, want := range []string{"box ", "git -C /w/box push --dry-run", "https://github.com/o/r.git", "HEAD:refs/heads/vibe-push-check-", "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("ran %q, want it to have %q", cmd, want)
		}
	}
}

// Running a command in a stopped sandbox would start it, which takes gigabytes: it is never asked.
func TestTheCheckFromInsideNeverWakesAStoppedSandbox(t *testing.T) {
	for name, tc := range map[string]struct {
		up    bool
		known bool
		want  string
	}{
		"stopped":    {false, true, "not checked from inside: it isn't running (it isn't started for this)"},
		"sbx unsure": {false, false, "not checked from inside: sbx couldn't say whether it's running"},
	} {
		sandboxesUp(t, map[string]bool{"box": tc.up}, tc.known)
		asked := runsInSandbox(t, nil)
		if got := checkPushFromInside("box", "/w/box", &gittoken.Repo{Owner: "o", Name: "r"}); got.said != tc.want {
			t.Errorf("%s: checkPushFromInside = %q, want %q", name, got.said, tc.want)
		}
		if len(*asked) != 0 {
			t.Errorf("%s: ran %q in the sandbox", name, *asked)
		}
	}
}

func TestThereIsNoCheckFromInsideWithoutAGitHubRepository(t *testing.T) {
	sandboxesUp(t, map[string]bool{"box": true}, true)
	asked := runsInSandbox(t, nil)
	if got := checkPushFromInside("box", "/w/box", nil); got != (insideCheck{}) {
		t.Errorf("checkPushFromInside = %+v, want nothing said", got)
	}
	if len(*asked) != 0 {
		t.Errorf("ran %q in the sandbox", *asked)
	}
}

// --git-token, a renewal and the batch all give an existing sandbox its token through setGitAccessSaying: it checks from inside,
// says so, and gives the batch the answer for its summary.
func TestGivingAnExistingSandboxItsTokenChecksFromInsideAndSaysSo(t *testing.T) {
	stubGitAccess(t)
	old := pushFromInside
	t.Cleanup(func() { pushFromInside = old })
	var checked []string
	pushFromInside = func(name, target string, repo *gittoken.Repo) insideCheck {
		checked = append(checked, name+" "+target+" "+repo.String())
		return insideCheck{said: "the sandbox can push"}
	}
	home := t.TempDir()
	inst := state.Instance{Name: "box", Target: "/w/box"}
	saveBatchRecord(t, home, inst)
	var said []string
	say := func(format string, a ...interface{}) { said = append(said, fmt.Sprintf(format, a...)) }
	inside, err := setGitAccessSaying(say, home, &inst, &gitAccess{token: "good", source: gitTokenOwn}, &gittoken.Repo{Owner: "o", Name: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if inside.said != "the sandbox can push" || len(checked) != 1 || checked[0] != "box /w/box o/r" {
		t.Errorf("inside = %+v, checked %q", inside, checked)
	}
	if !strings.Contains(strings.Join(said, "\n"), "the sandbox can push") {
		t.Errorf("said %q, want the check's answer", said)
	}
}
