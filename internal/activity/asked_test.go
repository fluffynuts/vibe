package activity

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func prompt(hhmm, branch, text string) Mark {
	m := mark(hhmm, "vibe", branch)
	m.Kind, m.Text = KindPrompt, text
	return m
}

func TestABranchWithNoCommitsHasTheDaysPromptsAskedInOrderOnceEach(t *testing.T) {
	marks := []Mark{
		prompt("10:05", "master", "now the week view"),
		prompt("09:00", "master", "plan the activity tab"),
		prompt("09:30", "master", "plan the activity tab"),
		mark("09:10", "vibe", "master"),
		{At: day.Add(-time.Hour), Repo: "vibe", Branch: "master", Kind: KindPrompt, Text: "yesterday's"},
	}
	got := only(t, Summarise(day, marks, nil, nil, 30*time.Minute))
	if want := []string{"plan the activity tab", "now the week view"}; !reflect.DeepEqual(got.Asked, want) {
		t.Fatalf("want %q, got %q", want, got.Asked)
	}
}

func TestABranchWithCommitsHasNoPromptsAsked(t *testing.T) {
	marks := []Mark{prompt("09:00", "feat/x", "build it")}
	bullets := []Bullet{{At: at("09:20"), Repo: "vibe", Branch: "feat/x", Text: "built it"}}
	got := only(t, Summarise(day, marks, nil, bullets, 30*time.Minute))
	if len(got.Asked) != 0 || len(got.Commits) != 1 {
		t.Fatalf("want the commits alone, got %+v", got)
	}
}

func TestThePromptKeptIsItsStartOnOneLine(t *testing.T) {
	if got := PromptStart("  plan\n\nthe   tab \t now "); got != "plan the tab now" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("é", PromptKept+10)
	if got := PromptStart(long); got != strings.Repeat("é", PromptKept)+"…" {
		t.Fatalf("want %d characters and an ellipsis, got %d", PromptKept, len([]rune(got)))
	}
}

func TestTheRecorderKeepsEveryPromptWithItsStart(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	now := at("09:00")
	r := &Recorder{Log: l, Sandbox: "s", Folder: "/code/r", Now: func() time.Time { return now }}
	r.Mark(KindPrompt, "b", "first\nask")
	r.Mark(KindPrompt, "b", "second ask")
	r.Wait()
	got, _, _ := l.Marks(now, now)
	var texts []string
	for _, m := range got {
		texts = append(texts, m.Text)
	}
	if !sameSet(texts, []string{"first ask", "second ask"}) {
		t.Fatalf("want both prompts kept in the same minute, got %q", texts)
	}
}

var askedDay = Day{Date: "2026-11-26", Branches: []BranchTime{
	{Repo: "vibe", Branch: "master", Asked: []string{"plan the week view"}},
	{Repo: "vibe", Branch: "idle"},
}}

func TestABranchWithOnlyPromptsIsSummarisedFromThem(t *testing.T) {
	a := &asker{}
	s := summariser(t, t.TempDir(), a, "vibe")
	if got, want := states(s.States(askedDay)), []string{"pending ", "none "}; !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	s.Wait()
	p := a.prompts[0]
	for _, want := range []string{`"prompts": [`, `"plan the week view"`, "what the work was", "don't say that there were no commits"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt doesn't have %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, `"commits"`) {
		t.Errorf("want no empty commits given:\n%s", p)
	}
}

func TestAnotherPromptOnABranchWithoutCommitsSummarisesItAgain(t *testing.T) {
	b := askedDay.Branches[0]
	more := b
	more.Asked = append([]string{}, b.Asked...)
	more.Asked = append(more.Asked, "and the pie")
	if SummaryKey("d", b, "r") == SummaryKey("d", more, "r") {
		t.Fatal("want another prompt to change the key")
	}
}
