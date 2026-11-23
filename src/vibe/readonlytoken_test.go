package main

import (
	"fmt"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/state"
)

// A token that can read the repository but not push: a script gives it read-only only when --git-read-only says so; otherwise it is
// turned down, as before.
func TestATokenFromTheEnvironmentThatCantPushIsReadOnlyOnlyWhenTheScriptSaysSo(t *testing.T) {
	stubGitAccess(t)
	t.Setenv("VIBE_TEST_TOKEN", "readonly")
	repo := &gittoken.Repo{Owner: "o", Name: "r"}
	if _, err := gitAccessFromEnv("VIBE_TEST_TOKEN", repo, nil, false); err == nil {
		t.Error("a token that can't push was taken without --git-read-only")
	}
	access, err := gitAccessFromEnv("VIBE_TEST_TOKEN", repo, nil, true)
	if err != nil || access == nil || !access.readOnly || access.token != "readonly" {
		t.Fatalf("with --git-read-only: %+v, %v", access, err)
	}
	t.Setenv("VIBE_TEST_TOKEN", "good")
	if access, err := gitAccessFromEnv("VIBE_TEST_TOKEN", repo, nil, true); err != nil || access.readOnly {
		t.Errorf("a token that can push was given read-only: %+v, %v", access, err)
	}
}

// Given read-only, a sandbox's record says so, its agent's instructions are the read-only ones, and the line said says it can't push.
func TestASandboxGivenAReadOnlyTokenIsRecordedAndToldSo(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	inst := state.Instance{Name: "box", Target: "/w/box", GitRules: []string{gittoken.RuleFeatureBranch}}
	saveBatchRecord(t, home, inst)
	var said []string
	say := func(format string, a ...interface{}) { said = append(said, fmt.Sprintf(format, a...)) }
	access := &gitAccess{token: "readonly", source: gitTokenOwn, rules: inst.GitRules, expires: fakeExpiry, readOnly: true}
	if _, err := setGitAccessSaying(say, home, &inst, access, &gittoken.Repo{Owner: "o", Name: "r"}); err != nil {
		t.Fatal(err)
	}
	saved, _, _ := state.Load(home, "box")
	if !saved.GitReadOnly {
		t.Error("the record doesn't say read-only")
	}
	if len(saved.GitRules) != 1 {
		t.Errorf("the push rules weren't kept, for a token that can push later: %v", saved.GitRules)
	}
	if !strings.HasPrefix(f.instructions["box"], "# Reading from GitHub") {
		t.Errorf("the agent was given the instructions for pushing:\n%s", f.instructions["box"])
	}
	if all := strings.Join(said, "\n"); !strings.Contains(all, "read-only") || strings.Contains(all, "the agent can push") {
		t.Errorf("said:\n%s", all)
	}
	// Given a token that can push later, it isn't read-only any more.
	if _, err := setGitAccessSaying(say, home, &saved, &gitAccess{token: "good", source: gitTokenOwn, rules: saved.GitRules, expires: fakeExpiry}, &gittoken.Repo{Owner: "o", Name: "r"}); err != nil {
		t.Fatal(err)
	}
	again, _, _ := state.Load(home, "box")
	if again.GitReadOnly || !strings.HasPrefix(f.instructions["box"], "# Pushing to GitHub") {
		t.Errorf("a token that can push left it read-only: %+v\n%s", again, f.instructions["box"])
	}
}

// What a sandbox is told at each start follows its record.
func TestTheInstructionsWrittenAtEachStartFollowWhetherItIsReadOnly(t *testing.T) {
	f := stubGitAccess(t)
	sandboxesUp(t, map[string]bool{"box": true}, true)
	home := t.TempDir()
	saveBatchRecord(t, home, state.Instance{Name: "box", Target: t.TempDir(), GitToken: gitTokenOwn, GitReadOnly: true})
	if err := syncGitInstructions(home, "box"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.instructions["box"], "# Reading from GitHub") {
		t.Errorf("a read-only sandbox was told it can push:\n%s", f.instructions["box"])
	}
}

// A batch for a script, with --git-read-only: a repository the token can only read gets it read-only, one it can push to as
// before, and the summary says which; without the flag, the one it can only read is skipped.
func TestABatchGivesReadOnlyWhereTheTokenCantPushOnlyWhenAccepted(t *testing.T) {
	// two's repository the token can only read.
	twoReadOnly := func() {
		check := checkGitToken
		checkGitToken = func(token string, repo *gittoken.Repo) gittoken.Report {
			if repo != nil && repo.Name == "two" {
				return check("readonly", repo)
			}
			return check(token, repo)
		}
	}
	f, home := scriptBatch(t)
	twoReadOnly()
	t.Setenv("VIBE_TEST_TOKEN", "good")
	said := stderrOf(t, func() {
		if err := doBatchGitToken(home, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one", "two"}, GitTokenEnv: "VIBE_TEST_TOKEN", GitReadOnly: true}); err != nil {
			t.Errorf("doBatchGitToken: %v", err)
		}
	})
	one, _, _ := state.Load(home, "one")
	two, _, _ := state.Load(home, "two")
	if f.secrets["two"] != "good" || !two.GitReadOnly || one.GitReadOnly {
		t.Errorf("secrets %v; one read-only %v, two read-only %v", f.secrets, one.GitReadOnly, two.GitReadOnly)
	}
	if !strings.Contains(said, "✔ two") || !strings.Contains(said, "read-only") {
		t.Errorf("the summary doesn't say two is read-only:\n%s", said)
	}

	f2, home2 := scriptBatch(t)
	twoReadOnly()
	err := doBatchGitToken(home2, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one", "two"}, GitTokenEnv: "VIBE_TEST_TOKEN"})
	if err == nil || f2.secrets["two"] != "" {
		t.Errorf("without --git-read-only, two was given the token (%v) or nothing failed (%v)", f2.secrets, err)
	}
}

// The check from inside expects GitHub to refuse a read-only sandbox's push, and says plainly when it doesn't.
func TestTheCheckFromInsideOfAReadOnlySandboxExpectsARefusal(t *testing.T) {
	refused := pushCheckVerdict(fmt.Errorf("exit status 128: remote: Permission to o/r.git denied to someone.\nfatal: ... error: 403"))
	if got := forReadOnly(refused); got.refused || !strings.Contains(got.said, "read-only, as chosen") {
		t.Errorf("a read-only sandbox refused: %+v", got)
	}
	if got := forReadOnly(pushCheckVerdict(nil)); got.refused || !strings.Contains(got.said, "can push") || !strings.Contains(got.said, "read-only") {
		t.Errorf("a read-only sandbox that can push: %+v", got)
	}
	other := insideCheck{said: "not checked from inside: it isn't running (it isn't started for this)"}
	if got := forReadOnly(other); got != other {
		t.Errorf("a check that didn't happen changed: %+v", got)
	}
}

func TestInfoTheAdminTabAndTheBatchChecklistSayASandboxIsReadOnly(t *testing.T) {
	stubGitAccess(t)
	info := sandboxInfo{name: "proj", target: "/code/proj", profile: "p", agent: "claude",
		gitToken: gitTokenOwn, gitRules: []string{gittoken.RuleFeatureBranch}, gitTokenExpires: fakeExpiry, gitReadOnly: true}
	var out strings.Builder
	printInfo(&out, info, true)
	if !strings.Contains(out.String(), "github    read-only: the agent can fetch and read, not push; you push (own token") ||
		strings.Contains(out.String(), "feature branch") {
		t.Errorf("--info of a read-only sandbox should say it's read-only, without the push rules it isn't told:\n%s", out.String())
	}
	if tab := adminGitToken("proj", "/code/proj", info); !tab.ReadOnly {
		t.Errorf("the Admin tab should be told it's read-only: %+v", tab)
	}
	label := batchLabel(state.Instance{Name: "proj", GitToken: gitTokenOwn, GitReadOnly: true}, fakeNow)
	if !strings.Contains(label, "read-only") {
		t.Errorf("the checklist should say it's read-only: %q", label)
	}
	if label := batchLabel(state.Instance{Name: "proj", GitToken: gitTokenOwn}, fakeNow); strings.Contains(label, "read-only") {
		t.Errorf("only a read-only sandbox is said to be: %q", label)
	}
}

func TestARebuildKeepsASandboxReadOnly(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repo := &gittoken.Repo{Owner: "o", Name: "r"}
	inst := state.Instance{Name: "proj"}
	if err := setGitAccess(home, &inst, &gitAccess{token: "readonly", source: gitTokenOwn, readOnly: true}, repo); err != nil {
		t.Fatal(err)
	}
	if got := recordedGitAccess(home, "proj"); got == nil || !got.readOnly {
		t.Errorf("a rebuild should hand on that the sandbox is read-only: %+v", got)
	}
}

func TestANewDefaultTokenKeepsTheSandboxesUsingItReadOnly(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	ask := askToShareDefault
	askToShareDefault = func([]string) bool { return true }
	t.Cleanup(func() { askToShareDefault = ask })
	if err := state.Save(home, state.Instance{Name: "proj", GitToken: gitTokenDefault, GitReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	offerDefaultToken(home, "good", fakeExpiry)
	if inst, _, _ := state.Load(home, "proj"); !inst.GitReadOnly {
		t.Errorf("a sandbox given read-only should stay so with the new default: %+v", inst)
	}
}
