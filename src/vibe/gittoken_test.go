package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/state"
)

// fakeGitAccess stands in for GitHub, sbx and the sandbox, recording what
// was asked of them. GitHub accepts the token "good" and can push to any
// repository with it.
type fakeGitAccess struct {
	secrets      map[string]string
	instructions map[string]string
	setErr       error
}

// fakeNow is the time the tests run at, and fakeExpiry when the fake
// GitHub's tokens expire: a week later.
var (
	fakeNow    = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	fakeExpiry = fakeNow.Add(7 * 24 * time.Hour)
)

func stubGitAccess(t *testing.T) *fakeGitAccess {
	t.Helper()
	f := &fakeGitAccess{secrets: map[string]string{}, instructions: map[string]string{}}
	oldCheck, oldSet, oldRemove, oldWrite, oldRemoveInstr, oldNow := checkGitToken, setGitHubSecret, removeGitHubSecret, writeGitInstructions, removeGitInstructions, timeNow
	t.Cleanup(func() {
		checkGitToken, setGitHubSecret, removeGitHubSecret, writeGitInstructions, removeGitInstructions, timeNow = oldCheck, oldSet, oldRemove, oldWrite, oldRemoveInstr, oldNow
	})
	timeNow = func() time.Time { return fakeNow }
	checkGitToken = func(token string, repo *gittoken.Repo) gittoken.Report {
		if token != "good" {
			return gittoken.Report{Rejected: true}
		}
		return gittoken.Report{Login: "someone", Repo: repo, SeesRepo: repo != nil, CanPush: repo != nil, Expires: fakeExpiry}
	}
	setGitHubSecret = func(name, token string) error {
		if f.setErr != nil {
			return f.setErr
		}
		f.secrets[name] = token
		return nil
	}
	removeGitHubSecret = func(name string) error {
		delete(f.secrets, name)
		return nil
	}
	writeGitInstructions = func(name, content string) error {
		f.instructions[name] = content
		return nil
	}
	removeGitInstructions = func(name string) error {
		delete(f.instructions, name)
		return nil
	}
	return f
}

func TestPickGitAccessDefaultsToNone(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	// No terminal (see TestMain), and -f: neither asks, and neither gives
	// a token — even with a default stored.
	if err := gittoken.Save(state.DefaultGitTokenPath(home), "good"); err != nil {
		t.Fatal(err)
	}
	for _, args := range []cliargs.Args{{}, {Force: true}, {NoGitToken: true}} {
		access, err := pickGitAccess(home, args, "proj", t.TempDir())
		if err != nil || access != nil {
			t.Errorf("%+v: got %+v, %v; want no token", args, access, err)
		}
	}
	if _, err := pickGitAccess(home, cliargs.Args{GitRules: "yolo"}, "proj", t.TempDir()); err == nil {
		t.Error("an unknown --git-rules was accepted")
	}
}

func TestPickGitAccessFromEnv(t *testing.T) {
	stubGitAccess(t)
	t.Setenv("VIBE_TEST_TOKEN", " good\n")
	access, err := pickGitAccess(t.TempDir(), cliargs.Args{Force: true, GitTokenEnv: "VIBE_TEST_TOKEN", GitRules: "no-default-branch,feature-branch"}, "proj", t.TempDir())
	if err != nil || access == nil {
		t.Fatalf("got %+v, %v", access, err)
	}
	if access.token != "good" || access.source != gitTokenOwn || strings.Join(access.rules, ",") != "feature-branch,no-default-branch" || !access.expires.Equal(fakeExpiry) {
		t.Errorf("got %+v", access)
	}

	t.Setenv("VIBE_TEST_TOKEN", "bad")
	if _, err := pickGitAccess(t.TempDir(), cliargs.Args{GitTokenEnv: "VIBE_TEST_TOKEN"}, "proj", t.TempDir()); err == nil {
		t.Error("a token GitHub turns down was used, with no one to ask")
	}
	t.Setenv("VIBE_TEST_TOKEN", "")
	if _, err := pickGitAccess(t.TempDir(), cliargs.Args{GitTokenEnv: "VIBE_TEST_TOKEN"}, "proj", t.TempDir()); err == nil {
		t.Error("an empty --git-token-env was accepted")
	}
}

func TestVetGitTokenWhenGitHubCantBeAsked(t *testing.T) {
	stubGitAccess(t)
	checkGitToken = func(string, *gittoken.Repo) gittoken.Report {
		return gittoken.Report{Unreachable: errors.New("offline")}
	}
	if verdict, _ := vetGitToken("github_pat_x", nil, false, ""); verdict != vetUse {
		t.Error("not being able to ask GitHub should warn, not refuse the token")
	}
}

func TestGiveTakeAndRecordGitAccess(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	repo := &gittoken.Repo{Owner: "o", Name: "r"}

	given := giveGitAccess(home, "proj", &gitAccess{token: "good", source: gitTokenOwn, rules: []string{gittoken.RuleFeatureBranch}, expires: fakeExpiry}, repo)
	if given == nil || f.secrets["proj"] != "good" {
		t.Fatalf("token not handed to sbx: %+v %v", given, f.secrets)
	}
	if !strings.Contains(f.instructions["proj"], "feature branch") || !strings.Contains(f.instructions["proj"], "o/r") || !strings.Contains(f.instructions["proj"], "2026-10-16") {
		t.Errorf("instructions not written: %q", f.instructions["proj"])
	}
	if token, ok, _ := gittoken.Load(state.GitTokenPath(home, "proj")); !ok || token != "good" {
		t.Error("token not kept on the host for a rebuild")
	}

	// A rebuild hands on what the record and the kept token say.
	inst := state.Instance{Name: "proj", GitToken: given.source, GitRules: given.rules, GitTokenExpires: given.expires}
	if err := state.Save(home, inst); err != nil {
		t.Fatal(err)
	}
	if got := recordedGitAccess(home, "proj"); got == nil || got.token != "good" || got.source != gitTokenOwn || len(got.rules) != 1 || !got.expires.Equal(fakeExpiry) {
		t.Errorf("recordedGitAccess = %+v", got)
	}

	// Taking it away clears sbx, the sandbox, the host copy and the record.
	if err := setGitAccess(home, &inst, nil, repo); err != nil {
		t.Fatal(err)
	}
	if _, has := f.secrets["proj"]; has {
		t.Error("sbx secret left behind")
	}
	if _, has := f.instructions["proj"]; has {
		t.Error("instructions left behind")
	}
	if _, ok, _ := gittoken.Load(state.GitTokenPath(home, "proj")); ok {
		t.Error("host copy left behind")
	}
	if got, _, _ := state.Load(home, "proj"); got.GitToken != "" || got.GitRules != nil || !got.GitTokenExpires.IsZero() {
		t.Errorf("record still has a token: %+v", got)
	}
	if recordedGitAccess(home, "proj") != nil {
		t.Error("a sandbox without a token is rebuilt with one")
	}
}

func TestGiveGitAccessWhenSbxRefuses(t *testing.T) {
	f := stubGitAccess(t)
	f.setErr = errors.New("no")
	home := t.TempDir()
	if got := giveGitAccess(home, "proj", &gitAccess{token: "good", source: gitTokenOwn}, nil); got != nil {
		t.Errorf("a token sbx wouldn't take was recorded: %+v", got)
	}
	if _, ok, _ := gittoken.Load(state.GitTokenPath(home, "proj")); ok {
		t.Error("a token sbx wouldn't take was kept")
	}
	if _, has := f.instructions["proj"]; has {
		t.Error("the agent was told it can push")
	}
}

func TestRecordedGitAccessWithoutItsToken(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	if err := state.Save(home, state.Instance{Name: "proj", GitToken: gitTokenDefault}); err != nil {
		t.Fatal(err)
	}
	if got := recordedGitAccess(home, "proj"); got != nil {
		t.Errorf("a token that's gone from the host was handed on: %+v", got)
	}
}

func TestForgetGitSecret(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	f.secrets["mine"], f.secrets["theirs"] = "good", "set by hand"
	state.Save(home, state.Instance{Name: "mine", GitToken: gitTokenOwn})
	state.Save(home, state.Instance{Name: "theirs"})
	forgetGitSecret(home, "mine")
	forgetGitSecret(home, "theirs")
	if _, has := f.secrets["mine"]; has {
		t.Error("the secret vibe gave was left behind")
	}
	if _, has := f.secrets["theirs"]; !has {
		t.Error("a secret vibe didn't give was removed")
	}
}

func TestStateRemoveTakesTheGitToken(t *testing.T) {
	home := t.TempDir()
	path := state.GitTokenPath(home, "proj")
	if err := gittoken.Save(path, "good"); err != nil {
		t.Fatal(err)
	}
	if err := state.Remove(home, "proj"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := gittoken.Load(path); ok {
		t.Error("the git token outlived the sandbox's record")
	}
}

func TestGitFlagsOnlyApplyToNewSandboxesOrGitToken(t *testing.T) {
	for _, argv := range [][]string{
		{"--info", "--no-git-token"},
		{"-r", "--git-token-env", "X"},
		{"--list", "--git-rules", "feature-branch"},
	} {
		err := run(argv)
		if err == nil || !strings.Contains(err.Error(), "only apply") {
			t.Errorf("%v: got %v", argv, err)
		}
	}
}

func TestPrintInfoGitHub(t *testing.T) {
	stubGitAccess(t)
	info := sandboxInfo{name: "proj", target: "/code/proj", profile: "p", agent: "claude"}
	var out strings.Builder
	printInfo(&out, info, true)
	if !strings.Contains(out.String(), "github    no token: you commit and push yourself\n") {
		t.Errorf("info without a token:\n%s", out.String())
	}
	info.gitToken, info.gitRules = gitTokenOwn, []string{gittoken.RuleFeatureBranch, gittoken.RuleNoDefaultBranch}
	info.gitTokenExpires = fakeExpiry
	out.Reset()
	printInfo(&out, info, true)
	want := "github    the agent can push (own token, expires " + fakeExpiry.Local().Format("2006-01-02 15:04") + " (in 7 days)); it's told never to force-push, and to only work in a feature branch, and to never push the default branch (main/master)\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("info with a token:\n%s", out.String())
	}
}

func TestCheckGitTokenExpiryWithNoOneToAsk(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	f.secrets["proj"] = "old"
	inst := state.Instance{Name: "proj", GitToken: gitTokenOwn, GitTokenExpires: fakeNow.Add(-time.Hour)}
	state.Save(home, inst)
	// No terminal (see TestMain): it warns, and the session goes on with
	// the token as it was.
	checkGitTokenExpiry(home, cliargs.Args{}, "proj")
	checkGitTokenExpiry(home, cliargs.Args{Force: true}, "proj")
	if got, _, _ := state.Load(home, "proj"); got.GitToken != gitTokenOwn || f.secrets["proj"] != "old" {
		t.Errorf("an expired token was changed with no one to ask: %+v %v", got, f.secrets)
	}
	// Nothing to check is fine too.
	checkGitTokenExpiry(home, cliargs.Args{}, "nothing-recorded")
}

func TestSyncGitInstructions(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	state.Save(home, state.Instance{Name: "with", Target: t.TempDir(), GitToken: gitTokenOwn, GitRules: []string{gittoken.RuleNoDefaultBranch}, GitTokenExpires: fakeExpiry})
	state.Save(home, state.Instance{Name: "without", Target: t.TempDir()})
	f.instructions["without"] = "left over from a token since taken away"
	for _, name := range []string{"with", "without"} {
		if err := syncGitInstructions(home, name); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.instructions["with"]; !strings.Contains(got, "Never push the default branch") || !strings.Contains(got, "2026-10-16") {
		t.Errorf("instructions for a sandbox with a token: %q", got)
	}
	if _, has := f.instructions["without"]; has {
		t.Error("a sandbox without a token kept instructions saying it has one")
	}
}

func TestShareDefaultToken(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	for _, inst := range []state.Instance{
		{Name: "renewing", GitToken: gitTokenDefault},
		{Name: "also-default", GitToken: gitTokenDefault},
		{Name: "own", GitToken: gitTokenOwn},
	} {
		state.Save(home, inst)
		f.secrets[inst.Name] = "old"
	}
	if !shareDefaultToken(home, "new", fakeExpiry, "renewing") {
		t.Fatal("the default wasn't stored")
	}
	path := state.DefaultGitTokenPath(home)
	if token, _, _ := gittoken.Load(path); token != "new" || !gittoken.LoadExpiry(path).Equal(fakeExpiry) {
		t.Errorf("default = %q, expiring %v", token, gittoken.LoadExpiry(path))
	}
	// No terminal to ask on (see TestMain): the others are left as they
	// were, rather than changed unasked.
	for name, want := range map[string]string{"renewing": "old", "also-default": "old", "own": "old"} {
		if f.secrets[name] != want {
			t.Errorf("%s's secret = %q, want %q", name, f.secrets[name], want)
		}
	}
}

func TestVetGitTokenWithNoOneToAsk(t *testing.T) {
	stubGitAccess(t)
	if verdict, expires := vetGitToken("good", nil, false, ""); verdict != vetUse || !expires.Equal(fakeExpiry) {
		t.Errorf("a good token: %v, %v", verdict, expires)
	}
	if verdict, _ := vetGitToken("bad", nil, false, ""); verdict != vetSkip {
		t.Errorf("a token GitHub turns down, with no one to ask: %v", verdict)
	}
}

func TestPaintLeavesNoColourAlone(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := paint(colorRed, "All repositories"); got != "All repositories" {
		t.Errorf("paint with NO_COLOR = %q", got)
	}
}
