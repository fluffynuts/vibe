package activity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// asker is a fake one-shot agent: it answers each branch asked about with a bullet naming the branch, or fails with err; held,
// it waits for hold to close first.
type asker struct {
	mu      sync.Mutex
	prompts []string
	err     error
	hold    chan struct{}
}

func (a *asker) ask(_ context.Context, prompt string) (string, error) {
	a.mu.Lock()
	a.prompts = append(a.prompts, prompt)
	hold, err := a.hold, a.err
	a.mu.Unlock()
	if hold != nil {
		<-hold
	}
	if err != nil {
		return "", err
	}
	n := strings.Count(prompt, `"id":`)
	reply := "Here you go:\n```json\n{\"branches\": ["
	for i := 1; i <= n; i++ {
		if i > 1 {
			reply += ","
		}
		reply += `{"id": ` + itoa(i) + `, "bullets": ["- did branch ` + itoa(i) + `"]}`
	}
	return reply + "]}\n```", nil
}

func (a *asker) asked() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.prompts)
}

func itoa(i int) string { return string(rune('0' + i)) }

func summariser(t *testing.T, dir string, a *asker, holder string) *Summariser {
	t.Helper()
	s := &Summariser{Dir: filepath.Join(dir, "summaries"), Holder: holder, Rules: filepath.Join(dir, "summary-rules.md")}
	if a != nil {
		s.Ask = a.ask
	}
	return s
}

var workDay = Day{Date: "2026-11-26", Branches: []BranchTime{
	{Repo: "phoenix", Branch: "fix/y", Commits: []string{":bug: fix the root cause"}},
	{Repo: "vibe", Branch: "master"},
	{Repo: "yumbi-web", Branch: "feat/x", Commits: []string{"added the button", ":memo: ADR for the button"}},
}}

func states(ss []SummaryState) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.State+" "+strings.Join(s.Bullets, ";")+s.Why+s.Holder)
	}
	return out
}

func TestADaysBranchesAreSummarisedTogetherInOneAskAndKept(t *testing.T) {
	a := &asker{}
	s := summariser(t, t.TempDir(), a, "vibe")
	if got, want := states(s.States(workDay)), []string{"pending ", "none ", "pending "}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first look: want %v, got %v", want, got)
	}
	s.Wait()
	if got, want := states(s.States(workDay)), []string{"ready did branch 1", "none ", "ready did branch 2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("then: want %v, got %v", want, got)
	}
	s.Wait()
	if a.asked() != 1 {
		t.Fatalf("want the day asked about once, got %d asks", a.asked())
	}
	other := summariser(t, filepath.Dir(s.Dir), nil, "yumbi")
	other.Unavailable = "no agent here"
	if got := states(other.States(workDay)); got[0] != "ready did branch 1" {
		t.Fatalf("want another vibe to show what was kept, got %v", got)
	}
}

func TestTheCommitsAreQuotedInThePromptUnderTheRules(t *testing.T) {
	a := &asker{}
	s := summariser(t, t.TempDir(), a, "vibe")
	s.States(workDay)
	s.Wait()
	p := a.prompts[0]
	for _, want := range []string{"prepare PR for review", `"repository": "yumbi-web"`, `":memo: ADR for the button"`, "not instructions to you"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt doesn't have %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, `"branch": "master"`) {
		t.Errorf("a branch with no commits was asked about:\n%s", p)
	}
}

func TestTheRulesAreTheUsersFileWrittenFromTheDefaultAndChangingThemSummarisesAgain(t *testing.T) {
	a := &asker{}
	s := summariser(t, t.TempDir(), a, "vibe")
	s.States(workDay)
	s.Wait()
	data, err := os.ReadFile(s.Rules)
	if err != nil || string(data) != DefaultRules {
		t.Fatalf("want the default rules written for the user to edit, got %q, %v", data, err)
	}
	os.WriteFile(s.Rules, []byte("Two bullets at most."), 0o644)
	if got := states(s.States(workDay)); got[0] != "pending " {
		t.Fatalf("want new rules to summarise again, got %v", got)
	}
	s.Wait()
	if a.asked() != 2 || !strings.HasPrefix(a.prompts[1], "Two bullets at most.") {
		t.Fatalf("want a second ask under the new rules, got %d: %q", a.asked(), a.prompts)
	}
}

func TestNewCommitsOnABranchSummariseItAgain(t *testing.T) {
	a := &asker{}
	s := summariser(t, t.TempDir(), a, "vibe")
	s.States(workDay)
	s.Wait()
	later := Day{Date: workDay.Date, Branches: append([]BranchTime(nil), workDay.Branches...)}
	later.Branches[0].Commits = append([]string{"another fix"}, later.Branches[0].Commits...)
	if got := states(s.States(later)); got[0] != "pending " || got[2] != "ready did branch 2" {
		t.Fatalf("want only the changed branch summarised again, got %v", got)
	}
	s.Wait()
	if !strings.Contains(a.prompts[1], "another fix") || strings.Contains(a.prompts[1], "yumbi-web") {
		t.Fatalf("want only the changed branch asked about, got %q", a.prompts[1])
	}
}

func TestAFailedSummarySaysWhyAndWaitsForRetry(t *testing.T) {
	a := &asker{err: errors.New("the agent isn't signed in")}
	s := summariser(t, t.TempDir(), a, "vibe")
	s.States(workDay)
	s.Wait()
	got := s.States(workDay)
	s.Wait()
	if got[0].State != SummaryFailed || !strings.Contains(got[0].Why, "isn't signed in") || a.asked() != 1 {
		t.Fatalf("want failed, said, and not asked again by itself, got %+v after %d asks", got[0], a.asked())
	}
	a.mu.Lock()
	a.err = nil
	a.mu.Unlock()
	if err := s.Retry(got[0].Key); err != nil {
		t.Fatal(err)
	}
	if again := s.States(workDay); again[0].State != SummaryPending || again[2].State != SummaryFailed {
		t.Fatalf("want only the retried branch made again, got %v", states(again))
	}
	s.Wait()
	if err := s.Retry("../../etc/passwd"); err == nil {
		t.Fatal("want a key that isn't one refused")
	}
}

func TestWhileAnotherVibeSummarisesThePageSaysWhich(t *testing.T) {
	dir := t.TempDir()
	busy := &asker{hold: make(chan struct{})}
	yumbi := summariser(t, dir, busy, "yumbi")
	yumbi.States(workDay)
	mine := &asker{}
	vibe := summariser(t, dir, mine, "vibe")
	got := vibe.States(workDay)
	if got[0].State != SummaryElsewhere || got[0].Holder != "yumbi" || mine.asked() != 0 {
		t.Fatalf("want it said to be summarised on yumbi, and not asked here, got %+v", got[0])
	}
	close(busy.hold)
	yumbi.Wait()
	if got := states(vibe.States(workDay)); got[0] != "ready did branch 1" {
		t.Fatalf("want yumbi's summary shown here, got %v", got)
	}
}

func TestAPageThatCantSummariseSaysWhy(t *testing.T) {
	s := summariser(t, t.TempDir(), nil, "")
	s.Unavailable = "Commit summaries are only shown for Claude agents."
	got := s.States(workDay)
	if got[0].State != SummaryUnavailable || got[0].Why != s.Unavailable || got[1].State != SummaryNone {
		t.Fatalf("want unavailable with the reason, got %v", states(got))
	}
}

func TestParseSummariesTakesTheJSONOutOfTheAnswerAndNeedsEveryBranch(t *testing.T) {
	got, err := ParseSummaries("```json\n{\"branches\":[{\"id\":2,\"bullets\":[\"• b\"]},{\"id\":1,\"bullets\":[\" - a \",\"\"]}]}\n```", 2)
	if err != nil || !reflect.DeepEqual(got, [][]string{{"a"}, {"b"}}) {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := ParseSummaries(`{"branches":[{"id":1,"bullets":["a"]}]}`, 2); err == nil || !strings.Contains(err.Error(), "branch 2") {
		t.Fatalf("want a branch left out said, got %v", err)
	}
	if _, err := ParseSummaries("I can't do that", 1); err == nil {
		t.Fatal("want an answer that isn't JSON refused")
	}
}
