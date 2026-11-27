package activity

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var workWeek = WeekReport{Start: "2026-11-23", Repos: []RepoWeek{
	{Repo: "vibe", Branches: []BranchWeek{
		{Branch: "feat/activity", Commits: []DayLines{{Date: "2026-11-23", Lines: []string{"the week view"}}, {Date: "2026-11-26", Lines: []string{":bug: fix the pie"}}}},
		{Branch: "master", Asked: []DayLines{{Date: "2026-11-24", Lines: []string{"plan the release"}}}},
		{Branch: "idle"},
	}},
}}

func TestTheWeekIsSummarisedInOneAskUnderItsOwnRulesEachBranchKeyedByRepositoryAndBranch(t *testing.T) {
	a := &asker{}
	dir := t.TempDir()
	s := summariser(t, dir, a, "vibe")
	got := s.WeekStates(workWeek)
	if got[TicketKey("vibe", "feat/activity")].State != SummaryPending || got[TicketKey("vibe", "master")].State != SummaryPending || got[TicketKey("vibe", "idle")].State != SummaryNone {
		t.Fatalf("want the branches with work pending and the idle one with none, got %+v", got)
	}
	s.Wait()
	if got := s.WeekStates(workWeek)[TicketKey("vibe", "feat/activity")]; got.State != SummaryReady {
		t.Fatalf("want it made and kept, got %+v", got)
	}
	s.Wait()
	if a.asked() != 1 {
		t.Fatalf("want the week asked about once, got %d", a.asked())
	}
	p := a.prompts[0]
	for _, want := range []string{"Up to 10", "one week", "under 600 characters", `"Mon: the week view"`, `"Thu: :bug: fix the pie"`, `"prompts": [`, `"Tue: plan the release"`} {
		if !strings.Contains(p, want) {
			t.Errorf("the week's prompt doesn't have %q:\n%s", want, p)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "week-summary-rules.md"))
	if err != nil || string(data) != DefaultWeekRules {
		t.Fatalf("want the week's default rules written for the user to edit, got %q, %v", data, err)
	}
}

func TestAWeeksSummaryKeepsUpToTenBullets(t *testing.T) {
	bullets := make([]string, 12)
	for i := range bullets {
		bullets[i] = `"line ` + string(rune('a'+i)) + `"`
	}
	got, err := parseSummaries(`{"branches":[{"id":1,"bullets":[`+strings.Join(bullets, ",")+`]}]}`, 1, weekBullets, weekBullet)
	if err != nil || len(got[0]) != 10 || got[0][9] != "line j" {
		t.Fatalf("want the first ten kept, got %v, %v", got, err)
	}
	if got, _ := ParseSummaries(`{"branches":[{"id":1,"bullets":[`+strings.Join(bullets, ",")+`]}]}`, 1); len(got[0]) != dayBullets {
		t.Fatalf("want a day's kept to %d, got %d", dayBullets, len(got[0]))
	}
}

func TestTheWeekKeepsEachBranchsCommitsAndPromptsByDayAndItsTicket(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	monday := day.AddDate(0, 0, -3)
	work(t, l, monday, "09:00", "10:00", "vibe", "feat/a")
	l.Append("s", Mark{At: monday.Add(9*time.Hour + 5*time.Minute), Folder: "/code/vibe", Repo: "vibe", Branch: "feat/a", Kind: KindPrompt, Text: "plan it"})
	work(t, l, day, "09:00", "10:00", "vibe", "feat/a")
	l.Append("s", Mark{At: day.Add(9*time.Hour + 5*time.Minute), Folder: "/code/vibe", Repo: "vibe", Branch: "feat/a", Kind: KindPrompt, Text: "and the pie"})
	d := (&fakeGit{}).desk(l)
	d.Tickets = &Tickets{Path: filepath.Join(t.TempDir(), "tickets.json")}
	d.Tickets.Set("vibe", "feat/a", "https://trello.com/c/a")
	w, err := d.Week(context.Background(), "", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b := w.Repos[0].Branches[0]
	want := []DayLines{{Date: "2026-11-23", Lines: []string{"plan it"}}, {Date: "2026-11-26", Lines: []string{"and the pie"}}}
	if !reflect.DeepEqual(b.Asked, want) || len(b.Commits) != 0 || b.Ticket != "https://trello.com/c/a" {
		t.Fatalf("want the week's prompts by day (no commits) and the ticket, got %+v", b)
	}
}

func TestAWeeksLineKeepsTwiceADays(t *testing.T) {
	long := strings.Repeat("x", 550)
	reply := `{"branches":[{"id":1,"bullets":["` + long + `"]}]}`
	if got, _ := parseSummaries(reply, 1, weekBullets, weekBullet); got[0][0] != long {
		t.Fatalf("want a week's 550-character line whole, got %d characters", len(got[0][0]))
	}
	if got, _ := ParseSummaries(reply, 1); len(got[0][0]) >= 550 {
		t.Fatalf("want a day's line cut, got %d characters", len(got[0][0]))
	}
}
