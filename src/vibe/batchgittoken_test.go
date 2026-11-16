package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vibe/internal/gittoken"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// repositories stands in for what each folder's origin remote points at.
func repositories(t *testing.T, byTarget map[string]*gittoken.Repo) {
	t.Helper()
	old := batchRepoOf
	t.Cleanup(func() { batchRepoOf = old })
	batchRepoOf = func(target string) *gittoken.Repo { return byTarget[target] }
}

func saveBatchRecord(t *testing.T, home string, inst state.Instance) {
	t.Helper()
	if err := state.Save(home, inst); err != nil {
		t.Fatal(err)
	}
}

func TestBatchOffersOnlySandboxesSbxKnowsByName(t *testing.T) {
	home := t.TempDir()
	saveBatchRecord(t, home, state.Instance{Name: "zeta"})
	saveBatchRecord(t, home, state.Instance{Name: "alpha"})
	saveBatchRecord(t, home, state.Instance{Name: "lost"})
	old := listSandboxes
	t.Cleanup(func() { listSandboxes = old })
	listSandboxes = func(context.Context) ([]sbxrun.Status, error) {
		return []sbxrun.Status{{Name: "zeta"}, {Name: "alpha"}, {Name: "made-with-sbx"}}, nil
	}

	got, err := batchCandidates(home)

	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, inst := range got {
		names = append(names, inst.Name)
	}
	if !reflect.DeepEqual(names, []string{"alpha", "zeta"}) {
		t.Errorf("offered %v, want [alpha zeta]: only vibe's sandboxes sbx still has, by name", names)
	}
}

func TestBatchLabelSaysWhatTheTokenIsNowFromVibesRecords(t *testing.T) {
	stubGitAccess(t)
	own := batchLabel(state.Instance{Name: "a", Target: "/w/a", Profile: "p", GitToken: gitTokenOwn, GitTokenExpires: fakeExpiry}, fakeNow)
	for _, want := range []string{"a", "/w/a", "[p]", "own", gittoken.DescribeExpiry(fakeExpiry, fakeNow)} {
		if !strings.Contains(own, want) {
			t.Errorf("%q should contain %q", own, want)
		}
	}
	if none := batchLabel(state.Instance{Name: "b", Target: "/w/b"}, fakeNow); !strings.Contains(none, "no token") {
		t.Errorf("a sandbox without a token should say so: %q", none)
	}
	if def := batchLabel(state.Instance{Name: "c", GitToken: gitTokenDefault}, fakeNow); !strings.Contains(def, "default") {
		t.Errorf("%q", def)
	}
}

func TestBatchGivesTheTokenToThePickedSandboxesAndNoOtherKeepingTheirRules(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	repositories(t, map[string]*gittoken.Repo{"/w/one": {Owner: "o", Name: "one"}, "/w/two": {Owner: "o", Name: "two"}})
	one := state.Instance{Name: "one", Target: "/w/one", GitToken: gitTokenOwn, GitRules: []string{gittoken.RuleFeatureBranch}}
	two := state.Instance{Name: "two", Target: "/w/two"}
	other := state.Instance{Name: "other", Target: "/w/other", GitToken: gitTokenOwn}
	for _, inst := range []state.Instance{one, two, other} {
		saveBatchRecord(t, home, inst)
	}

	outcomes := giveTokenToSandboxes(home, []state.Instance{one, two}, batchToken{token: "good", source: gitTokenDefault, expires: fakeExpiry}, nil, 1)

	if f.secrets["one"] != "good" || f.secrets["two"] != "good" {
		t.Errorf("secrets = %v, want both picked given the token", f.secrets)
	}
	if _, has := f.secrets["other"]; has {
		t.Error("a sandbox that wasn't picked was given the token")
	}
	gotOne, _, _ := state.Load(home, "one")
	if gotOne.GitToken != gitTokenDefault || !reflect.DeepEqual(gotOne.GitRules, []string{gittoken.RuleFeatureBranch}) {
		t.Errorf("one's record = %+v, want the new source and its own rules", gotOne)
	}
	if len(outcomes) != 2 || !outcomes[0].given || !outcomes[1].given {
		t.Errorf("outcomes = %+v", outcomes)
	}
}

func TestBatchPresetRulesReplaceEachSandboxsOwn(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	one := state.Instance{Name: "one", Target: "/w/one", GitToken: gitTokenOwn, GitRules: []string{gittoken.RuleFeatureBranch}}
	saveBatchRecord(t, home, one)

	giveTokenToSandboxes(home, []state.Instance{one}, batchToken{token: "good", source: gitTokenOwn}, []string{gittoken.RuleNoDefaultBranch}, 1)

	if got, _, _ := state.Load(home, "one"); !reflect.DeepEqual(got.GitRules, []string{gittoken.RuleNoDefaultBranch}) {
		t.Errorf("rules = %v, want the preset", got.GitRules)
	}
}

func TestBatchSkipsASandboxWhoseRepositoryTheTokenCantPushToAndGoesOn(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	repositories(t, map[string]*gittoken.Repo{"/w/ok": {Owner: "o", Name: "ok"}, "/w/no": {Owner: "o", Name: "no"}})
	checkGitToken = func(token string, repo *gittoken.Repo) gittoken.Report {
		return gittoken.Report{Login: "x", Repo: repo, SeesRepo: true, CanPush: repo.Name == "ok", Expires: fakeExpiry}
	}
	no := state.Instance{Name: "no", Target: "/w/no"}
	ok := state.Instance{Name: "ok", Target: "/w/ok"}
	saveBatchRecord(t, home, no)
	saveBatchRecord(t, home, ok)

	outcomes := giveTokenToSandboxes(home, []state.Instance{no, ok}, batchToken{token: "good", source: gitTokenOwn}, nil, 1)

	if _, has := f.secrets["no"]; has || f.secrets["ok"] != "good" {
		t.Errorf("secrets = %v, want only 'ok' given the token", f.secrets)
	}
	if outcomes[0].given || !strings.Contains(outcomes[0].problem, "o/no") || !outcomes[1].given {
		t.Errorf("outcomes = %+v", outcomes)
	}
}

func TestBatchChecksEachRepositoryOnce(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, map[string]*gittoken.Repo{"/w/a": {Owner: "o", Name: "shared"}, "/w/b": {Owner: "o", Name: "shared"}})
	checks := 0
	checkGitToken = func(token string, repo *gittoken.Repo) gittoken.Report {
		checks++
		return gittoken.Report{Repo: repo, SeesRepo: true, CanPush: true}
	}
	a, b := state.Instance{Name: "a", Target: "/w/a"}, state.Instance{Name: "b", Target: "/w/b"}
	saveBatchRecord(t, home, a)
	saveBatchRecord(t, home, b)

	giveTokenToSandboxes(home, []state.Instance{a, b}, batchToken{token: "good", source: gitTokenOwn}, nil, 1)

	if checks != 1 {
		t.Errorf("GitHub was asked %d times about one repository, want 1", checks)
	}
}

func TestBatchAFailureToSetOneSecretStopsNoOther(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	setGitHubSecret = func(name, token string) error {
		if name == "broken" {
			return errors.New("sbx refused")
		}
		f.secrets[name] = token
		return nil
	}
	broken, fine := state.Instance{Name: "broken", Target: "/w/broken"}, state.Instance{Name: "fine", Target: "/w/fine"}
	saveBatchRecord(t, home, broken)
	saveBatchRecord(t, home, fine)

	outcomes := giveTokenToSandboxes(home, []state.Instance{broken, fine}, batchToken{token: "good", source: gitTokenOwn}, nil, 1)

	if outcomes[0].given || !outcomes[1].given || f.secrets["fine"] != "good" {
		t.Errorf("outcomes = %+v, secrets = %v", outcomes, f.secrets)
	}
	if got, _, _ := state.Load(home, "broken"); got.GitToken != "" {
		t.Errorf("a sandbox that didn't get the token records one: %+v", got)
	}
}

func TestBatchSummaryNamesEachOutcomeWithExpiriesAndHowToTryAgainButNeverTheToken(t *testing.T) {
	stubGitAccess(t)
	oldExpiry := fakeNow.AddDate(0, 0, 3)
	lines := strings.Join(batchSummary([]batchOutcome{
		{name: "got", given: true, oldExpires: oldExpiry, newExpires: fakeExpiry, pushCheck: "GitHub says it can push to o/got"},
		{name: "missed", folder: "/w/missed", problem: "the token can't see o/missed"},
	}, fakeNow), "\n")

	for _, want := range []string{
		"✔ got", gittoken.DescribeExpiry(oldExpiry, fakeNow), gittoken.DescribeExpiry(fakeExpiry, fakeNow), "can push to o/got",
		"✘ missed", "can't see o/missed", "'vibe --git-token' in /w/missed tries again",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("summary lacks %q:\n%s", want, lines)
		}
	}
}

func TestBatchOutputNeverHasTheToken(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	inst := state.Instance{Name: "one", Target: "/w/one"}
	saveBatchRecord(t, home, inst)
	secret := "good"
	outcomes := giveTokenToSandboxes(home, []state.Instance{inst}, batchToken{token: secret, source: gitTokenOwn}, nil, 1)
	for _, line := range batchSummary(outcomes, fakeNow) {
		if strings.Contains(line, secret) {
			t.Errorf("the summary has the token: %q", line)
		}
	}
}

// slowSecrets makes giving a secret wait until release is closed, counting
// how many are being given at once.
type slowSecrets struct {
	mu                  sync.Mutex
	inFlight, mostAtOne int
	reached             chan struct{}
	release             chan struct{}
}

func holdSecrets(t *testing.T) *slowSecrets {
	t.Helper()
	s := &slowSecrets{reached: make(chan struct{}, 100), release: make(chan struct{})}
	setGitHubSecret = func(name, token string) error {
		s.mu.Lock()
		s.inFlight++
		s.mostAtOne = max(s.mostAtOne, s.inFlight)
		s.mu.Unlock()
		s.reached <- struct{}{}
		<-s.release
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
		return nil
	}
	return s
}

func fiveSandboxes(t *testing.T, home string) []state.Instance {
	t.Helper()
	var picked []state.Instance
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		inst := state.Instance{Name: name, Target: "/w/" + name}
		saveBatchRecord(t, home, inst)
		picked = append(picked, inst)
	}
	return picked
}

func TestBatchGivesToNoMoreSandboxesAtOnceThanJobsSays(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	picked := fiveSandboxes(t, home)
	held := holdSecrets(t)
	done := make(chan []batchOutcome)

	go func() {
		done <- giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 2)
	}()
	<-held.reached
	<-held.reached
	select {
	case <-held.reached:
		t.Fatal("a third sandbox was started while two were being given the token")
	case <-time.After(50 * time.Millisecond):
	}
	close(held.release)
	outcomes := <-done

	if held.mostAtOne != 2 {
		t.Errorf("most at once = %d, want 2", held.mostAtOne)
	}
	for i, o := range outcomes {
		if !o.given || o.name != picked[i].Name {
			t.Errorf("outcome %d = %+v: all should be given, in the order picked", i, o)
		}
	}
}

func TestBatchOneAtATimeIsTheDefaultBehaviour(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	picked := fiveSandboxes(t, home)
	held := holdSecrets(t)
	close(held.release)

	giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 1)

	if held.mostAtOne != 1 {
		t.Errorf("most at once = %d, want 1", held.mostAtOne)
	}
}

func TestBatchAsksGitHubOncePerRepositoryEvenWhenSandboxesRunTogether(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	byTarget := map[string]*gittoken.Repo{}
	picked := fiveSandboxes(t, home)
	for _, inst := range picked {
		byTarget[inst.Target] = &gittoken.Repo{Owner: "o", Name: "shared"}
	}
	repositories(t, byTarget)
	var checks atomic.Int32
	checkGitToken = func(token string, repo *gittoken.Repo) gittoken.Report {
		checks.Add(1)
		return gittoken.Report{Repo: repo, SeesRepo: true, CanPush: true}
	}

	giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 5)

	if checks.Load() != 1 {
		t.Errorf("GitHub was asked %d times about one repository, want 1", checks.Load())
	}
}

func TestBatchAFailureAmongSandboxesGivenTogetherStopsNoOther(t *testing.T) {
	f := stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	picked := fiveSandboxes(t, home)
	var mu sync.Mutex
	setGitHubSecret = func(name, token string) error {
		if name == "c" {
			return errors.New("sbx refused")
		}
		mu.Lock()
		defer mu.Unlock()
		f.secrets[name] = token
		return nil
	}

	outcomes := giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 5)

	for _, o := range outcomes {
		if o.given == (o.name == "c") {
			t.Errorf("%s: given = %v", o.name, o.given)
		}
	}
}
