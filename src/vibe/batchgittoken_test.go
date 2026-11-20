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

	"vibe/internal/cliargs"
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

	outcomes := giveTokenToSandboxes(home, []state.Instance{one, two}, batchToken{token: "good", source: gitTokenDefault, expires: fakeExpiry}, nil, 1, nil)

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

	giveTokenToSandboxes(home, []state.Instance{one}, batchToken{token: "good", source: gitTokenOwn}, []string{gittoken.RuleNoDefaultBranch}, 1, nil)

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

	outcomes := giveTokenToSandboxes(home, []state.Instance{no, ok}, batchToken{token: "good", source: gitTokenOwn}, nil, 1, nil)

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

	giveTokenToSandboxes(home, []state.Instance{a, b}, batchToken{token: "good", source: gitTokenOwn}, nil, 1, nil)

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

	outcomes := giveTokenToSandboxes(home, []state.Instance{broken, fine}, batchToken{token: "good", source: gitTokenOwn}, nil, 1, nil)

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
	outcomes := giveTokenToSandboxes(home, []state.Instance{inst}, batchToken{token: secret, source: gitTokenOwn}, nil, 1, nil)
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
		done <- giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 2, nil)
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

	giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 1, nil)

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

	giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 5, nil)

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

	outcomes := giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 5, nil)

	for _, o := range outcomes {
		if o.given == (o.name == "c") {
			t.Errorf("%s: given = %v", o.name, o.given)
		}
	}
}

func TestProgressLineSaysHowManyAreDoneAndWhichAreUnderWay(t *testing.T) {
	got := progressLine(0, 3, 10, []string{"a", "b"}, 80)
	for _, want := range []string{"3 of 10 done", "working on a, b"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
	if progressLine(0, 10, 10, nil, 80) != "⠋ 10 of 10 done" {
		t.Errorf("with nothing under way: %q", progressLine(0, 10, 10, nil, 80))
	}
	if progressLine(1, 0, 3, nil, 80) == progressLine(0, 0, 3, nil, 80) {
		t.Error("the frame should turn")
	}
}

func TestProgressLineIsCutToTheWidthOfTheTerminal(t *testing.T) {
	long := progressLine(0, 0, 4, []string{strings.Repeat("x", 60), strings.Repeat("y", 60)}, 40)
	if n := len([]rune(long)); n >= 40 || !strings.HasSuffix(long, "…") {
		t.Errorf("%d columns, %q", n, long)
	}
}

func TestBatchProgressOnAPlainOutputDrawsNoSpinner(t *testing.T) {
	var out strings.Builder
	p := newBatchProgress(2, &out, false, func() int { return 80 })
	p.begin()
	p.started("a")
	p.finished("a", []string{"  github: ok"})
	p.end()
	if out.Len() != 0 {
		t.Errorf("drew %q on a non-terminal", out.String())
	}
}

func TestBatchProgressCountsAndForgetsWhoIsUnderWay(t *testing.T) {
	var out strings.Builder
	p := newBatchProgress(2, &out, true, func() int { return 80 })
	p.started("a")
	p.started("b")
	p.finished("a", nil)
	if p.done != 1 || !reflect.DeepEqual(p.active, []string{"b"}) {
		t.Errorf("done %d, active %v", p.done, p.active)
	}
	if !strings.Contains(out.String(), "1 of 2 done — working on b") {
		t.Errorf("the spinner line should say so: %q", out.String())
	}
}

func TestBatchStartsTheNextSandboxAsSoonAsAnyOneFinishesNotWhenAllDo(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	picked := fiveSandboxes(t, home)[:4]
	releaseA := make(chan struct{})
	reached := make(chan string, 10)
	setGitHubSecret = func(name, token string) error {
		reached <- name
		if name == "a" {
			<-releaseA
		}
		return nil
	}
	done := make(chan struct{})

	go func() {
		giveTokenToSandboxes(home, picked, batchToken{token: "good", source: gitTokenOwn}, nil, 2, nil)
		close(done)
	}()

	// a is held, b finishes at once: with two at a time, c and d follow
	// while a is still going, not once a lets the "batch" end.
	seen := map[string]bool{}
	for len(seen) < 4 {
		select {
		case name := <-reached:
			seen[name] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("only %v were started while a was held: the next one waits for the whole group", seen)
		}
	}
	close(releaseA)
	<-done
}

func TestBatchKeepsWhatARunningVibeChangedInASandboxsRecordSinceTheListWasRead(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	listed := state.Instance{Name: "one", Target: "/w/one", GitToken: gitTokenOwn, GitRules: []string{gittoken.RuleFeatureBranch}}
	saveBatchRecord(t, home, listed)
	// While the user chose a token, the sandbox's own vibe and a --git-token
	// elsewhere changed its record.
	meanwhile := listed
	meanwhile.CompanionPort, meanwhile.KitHash = 4242, "newer-kit"
	meanwhile.GitRules = []string{gittoken.RuleNoDefaultBranch}
	saveBatchRecord(t, home, meanwhile)

	outcomes := giveTokenToSandboxes(home, []state.Instance{listed}, batchToken{token: "good", source: gitTokenDefault}, nil, 1, nil)

	got, _, _ := state.Load(home, "one")
	if !outcomes[0].given || got.GitToken != gitTokenDefault {
		t.Fatalf("not given: %+v %+v", outcomes[0], got)
	}
	if got.CompanionPort != 4242 || got.KitHash != "newer-kit" {
		t.Errorf("record = %+v: what changed since the list was read was reverted", got)
	}
	if !reflect.DeepEqual(got.GitRules, []string{gittoken.RuleNoDefaultBranch}) {
		t.Errorf("rules = %v, want the ones the sandbox has now, not the ones it had when listed", got.GitRules)
	}
}

func TestBatchSaysSoWhenASandboxsRecordHasGoneSinceTheListWasRead(t *testing.T) {
	stubGitAccess(t)
	home := t.TempDir()
	repositories(t, nil)
	gone := state.Instance{Name: "gone", Target: "/w/gone"}

	outcomes := giveTokenToSandboxes(home, []state.Instance{gone}, batchToken{token: "good", source: gitTokenOwn}, nil, 1, nil)

	if outcomes[0].given || !strings.Contains(outcomes[0].problem, "no record") {
		t.Errorf("outcome = %+v: a record that has gone is not one to write a token into", outcomes[0])
	}
}

// returnsSoon says whether f returns within a second: a hang is a failure, not a stuck test run.
func returnsSoon(f func()) bool {
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(time.Second):
		return false
	}
}

func TestBatchProgressThatWasNeverBegunCanBeEnded(t *testing.T) {
	var out strings.Builder
	p := newBatchProgress(2, &out, true, func() int { return 80 })
	if !returnsSoon(p.end) {
		t.Fatal("end() waited for a spinner that was never started")
	}
}

func TestBatchProgressCanBeEndedTwice(t *testing.T) {
	var out strings.Builder
	p := newBatchProgress(2, &out, true, func() int { return 80 })
	p.begin()
	p.end()
	if !returnsSoon(p.end) {
		t.Fatal("a second end() hung or panicked")
	}
}

// --- --sandboxes, for a script -------------------------------------------------------------------------------------------------

// scriptBatch is a home with three sandboxes sbx knows (one, two, other), each in its own GitHub repository.
func scriptBatch(t *testing.T) (*fakeGitAccess, string) {
	t.Helper()
	f := stubGitAccess(t)
	home := t.TempDir()
	repositories(t, map[string]*gittoken.Repo{"/w/one": {Owner: "o", Name: "one"}, "/w/two": {Owner: "o", Name: "two"}, "/w/other": {Owner: "o", Name: "other"}})
	for _, name := range []string{"one", "two", "other"} {
		saveBatchRecord(t, home, state.Instance{Name: name, Target: "/w/" + name})
	}
	old := listSandboxes
	t.Cleanup(func() { listSandboxes = old })
	listSandboxes = func(context.Context) ([]sbxrun.Status, error) {
		return []sbxrun.Status{{Name: "one"}, {Name: "two"}, {Name: "other"}}, nil
	}
	return f, home
}

func TestBatchForAScriptGivesTheNamedSandboxesTheTokenFromTheEnvironmentAndAsksNothing(t *testing.T) {
	f, home := scriptBatch(t)
	t.Setenv("VIBE_TEST_TOKEN", "good")
	err := doBatchGitToken(home, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one", "two"}, GitTokenEnv: "VIBE_TEST_TOKEN"})
	if err != nil {
		t.Fatalf("doBatchGitToken: %v", err)
	}
	if f.secrets["one"] != "good" || f.secrets["two"] != "good" {
		t.Errorf("secrets = %v, want one and two given the token", f.secrets)
	}
	if _, has := f.secrets["other"]; has {
		t.Error("a sandbox that wasn't named was given the token")
	}
	if _, has, _ := gittoken.Load(state.DefaultGitTokenPath(home)); has {
		t.Error("a script's batch made the token the default: it never does")
	}
}

func TestBatchForAScriptRefusesNamesItDoesntKnowSayingWhichBeforeGivingAnything(t *testing.T) {
	f, home := scriptBatch(t)
	t.Setenv("VIBE_TEST_TOKEN", "good")
	err := doBatchGitToken(home, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one", "ghost", "nope"}, GitTokenEnv: "VIBE_TEST_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "ghost, nope") {
		t.Fatalf("doBatchGitToken = %v, want it to name ghost and nope", err)
	}
	if len(f.secrets) != 0 {
		t.Errorf("secrets = %v, want nothing given when a name is unknown", f.secrets)
	}
}

func TestBatchForAScriptWithNoTerminalNeedsTheTokenFromTheEnvironment(t *testing.T) {
	f, home := scriptBatch(t)
	err := doBatchGitToken(home, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one"}})
	if err == nil || !strings.Contains(err.Error(), "--git-token-env") {
		t.Fatalf("doBatchGitToken = %v, want it to say --git-token-env is needed with no terminal", err)
	}
	if len(f.secrets) != 0 {
		t.Errorf("secrets = %v, want nothing given", f.secrets)
	}
}

func TestBatchForAScriptRefusesATokenTheEnvironmentDoesntHave(t *testing.T) {
	f, home := scriptBatch(t)
	err := doBatchGitToken(home, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one"}, GitTokenEnv: "VIBE_TEST_NO_SUCH_VAR"})
	if err == nil || !strings.Contains(err.Error(), "VIBE_TEST_NO_SUCH_VAR") {
		t.Fatalf("doBatchGitToken = %v, want it to name the variable", err)
	}
	if len(f.secrets) != 0 {
		t.Errorf("secrets = %v, want nothing given", f.secrets)
	}
}

// A sandbox whose repository the token can't push to is skipped, as in the asked-for batch, and the script is told it didn't all go.
func TestBatchForAScriptFailsWhenASandboxDidntGetTheTokenAndStillGivesTheOthers(t *testing.T) {
	f, home := scriptBatch(t)
	t.Setenv("VIBE_TEST_TOKEN", "good")
	check := checkGitToken
	checkGitToken = func(token string, repo *gittoken.Repo) gittoken.Report {
		if repo != nil && repo.Name == "two" {
			return gittoken.Report{Login: "someone", Repo: repo, SeesRepo: true, CanPush: false}
		}
		return check(token, repo)
	}
	err := doBatchGitToken(home, cliargs.Args{BatchGitToken: true, Sandboxes: []string{"one", "two"}, GitTokenEnv: "VIBE_TEST_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("doBatchGitToken = %v, want it to say 1 of 2 didn't get it", err)
	}
	if f.secrets["one"] != "good" {
		t.Errorf("secrets = %v, want one given the token all the same", f.secrets)
	}
}
