package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/layout"
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
	// Given a token that can push later, by whichever way (a new token here, not a renewal), it isn't read-only any more, and says so.
	said = nil
	if _, err := setGitAccessSaying(say, home, &saved, &gitAccess{token: "good", source: gitTokenOwn, rules: saved.GitRules, expires: fakeExpiry}, &gittoken.Repo{Owner: "o", Name: "r"}); err != nil {
		t.Fatal(err)
	}
	again, _, _ := state.Load(home, "box")
	if again.GitReadOnly || !strings.HasPrefix(f.instructions["box"], "# Pushing to GitHub") {
		t.Errorf("a token that can push left it read-only: %+v\n%s", again, f.instructions["box"])
	}
	if all := strings.Join(said, "\n"); !strings.Contains(all, "isn't read-only any more") {
		t.Errorf("becoming able to push wasn't said:\n%s", all)
	}
	// Given another that can push, there is nothing to say.
	said = nil
	if _, err := setGitAccessSaying(say, home, &again, &gitAccess{token: "good", source: gitTokenOwn, expires: fakeExpiry}, &gittoken.Repo{Owner: "o", Name: "r"}); err != nil {
		t.Fatal(err)
	}
	if all := strings.Join(said, "\n"); strings.Contains(all, "read-only") {
		t.Errorf("a sandbox that could already push was said to change:\n%s", all)
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

	// Without read-only accepted (the flag here; on a terminal, read-only is offered only for the first sandbox's repository, as
	// decided in #117), the one it can only read is skipped, and the summary says how it could have it read-only.
	f2, home2 := scriptBatch(t)
	twoReadOnly()
	var err error
	said = stderrOf(t, func() {
		err = doBatchGitToken(home2, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one", "two"}, GitTokenEnv: "VIBE_TEST_TOKEN"})
	})
	if err == nil || f2.secrets["two"] != "" {
		t.Errorf("without --git-read-only, two was given the token (%v) or nothing failed (%v)", f2.secrets, err)
	}
	if !strings.Contains(said, "✘ two") || !strings.Contains(said, "--git-read-only") {
		t.Errorf("the summary doesn't say how two could have the token read-only:\n%s", said)
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
	// Whether it could push is a fact of the check, not its wording (found in review, #118).
	if got := forReadOnly(insideCheck{said: "pushed, in other words", canPush: true}); !strings.Contains(got.said, "WARNING") {
		t.Errorf("a check that could push, however worded, should warn for a read-only sandbox: %+v", got)
	}
	if !pushCheckVerdict(nil).canPush || pushCheckVerdict(fmt.Errorf("exit status 128: error: 403")).canPush {
		t.Error("canPush should be set only by a check that could push")
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

func TestTheRulesForPushingArentAskedForAReadOnlyTokenAndAreKept(t *testing.T) {
	was := askGitRules
	t.Cleanup(func() { askGitRules = was })
	asked := 0
	askGitRules = func(preset []string) []string { asked++; return []string{gittoken.RuleNoDefaultBranch} }
	kept := []string{gittoken.RuleFeatureBranch}
	if got := gitRulesFor(&gitAccess{readOnly: true}, kept); asked != 0 || len(got) != 1 || got[0] != gittoken.RuleFeatureBranch {
		t.Errorf("read-only: asked %d times, rules %v; want not asked, %v kept", asked, got, kept)
	}
	if got := gitRulesFor(&gitAccess{}, kept); asked != 1 || got[0] != gittoken.RuleNoDefaultBranch {
		t.Errorf("a token that can push: asked %d times, rules %v; want asked once", asked, got)
	}
}

// The kit's pointer to vibe's GitHub file is in the kit's instructions, which an agent reads only when it thinks a kit is relevant:
// asked about its GitHub access, a fresh agent answered from the sandbox's own "push from inside" text, the pointer unread (seen
// twice 2026-11-23, the second time with the firmer pointer). The pointer has to be in what the agent always loads.
func TestTheInstructionsAnAgentAlwaysLoadsPointAtVibesGitHubFile(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	if err := state.Save(lay.Home, state.Instance{Name: "box", Profile: "p"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	merged, _, err := instructionsFor(lay, "box")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{gittoken.SandboxFile, "overrides"} {
		if !strings.Contains(merged, want) {
			t.Errorf("the always-loaded instructions lack %q:\n%s", want, merged)
		}
	}
}

// A read-only sandbox's token renewed with another that can only read stays read-only without asking again, nor telling the user to
// widen a token they chose to keep narrow (found in review, #116). Asking, here, with no terminal, would be a skip.
func TestATokenThatCanOnlyReadIsKeptReadOnlyWithoutAskingWhenReadOnlyWasAlreadyChosen(t *testing.T) {
	stubGitAccess(t)
	repo := &gittoken.Repo{Owner: "o", Name: "r"}
	var verdict vetVerdict
	said := notesOf(func() { verdict, _ = vetGitToken("readonly", repo, true, true, fixedSkip("leave it")) })
	if verdict != vetReadOnly {
		t.Errorf("verdict %v, want read-only, unasked", verdict)
	}
	if all := strings.Join(said, "\n"); strings.Contains(all, "read and write") || !strings.Contains(all, "read-only") {
		t.Errorf("said:\n%s", all)
	}
}

func TestRenewingAReadOnlySandboxsTokenAcceptsReadOnlyAsItWas(t *testing.T) {
	stubGitAccess(t)
	was := enterGitTokenFor
	t.Cleanup(func() { enterGitTokenFor = was })
	var accepted []bool
	enterGitTokenFor = func(_ *gittoken.Repo, _ skipWording, readOnlyOK bool, _ ...string) (string, time.Time, bool, bool) {
		accepted = append(accepted, readOnlyOK)
		return "", time.Time{}, false, false
	}
	home := t.TempDir()
	repo := &gittoken.Repo{Owner: "o", Name: "r"}
	renewGitAccess(home, &state.Instance{Name: "ro", GitToken: gitTokenOwn, GitReadOnly: true}, repo)
	renewGitAccess(home, &state.Instance{Name: "rw", GitToken: gitTokenOwn}, repo)
	if len(accepted) != 2 || !accepted[0] || accepted[1] {
		t.Errorf("read-only accepted for [read-only, read-write] sandboxes: %v, want [true false]", accepted)
	}
}
