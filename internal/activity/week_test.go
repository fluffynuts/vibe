package activity

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// work logs marks on repo's branch every ten minutes from from to to, on the day starting at midnight.
func work(t *testing.T, l Log, midnight time.Time, from, to, repo, branch string) {
	t.Helper()
	start, _ := time.ParseDuration(strings.Replace(from, ":", "h", 1) + "m")
	end, _ := time.ParseDuration(strings.Replace(to, ":", "h", 1) + "m")
	for d := start; d <= end; d += 10 * time.Minute {
		if err := l.Append("s", Mark{At: midnight.Add(d), Folder: "/code/" + repo, Repo: repo, Branch: branch, Kind: KindAgent}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheWeekIsMondayToSundaySummingEachDaysRoundedHoursByRepositoryAndBranch(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	monday := day.AddDate(0, 0, -3)
	work(t, l, monday, "09:00", "10:00", "vibe", "feat/a")
	work(t, l, monday, "11:00", "11:40", "yumbi", "fix/b")
	work(t, l, day, "09:00", "11:00", "vibe", "feat/a")
	work(t, l, day, "13:00", "13:30", "vibe", "master")
	w, err := (&fakeGit{}).desk(l).Week(context.Background(), "2026-11-25", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if w.Start != "2026-11-23" || len(w.Days) != 7 || w.Days[6] != "2026-11-29" {
		t.Fatalf("want Mon 23 to Sun 29, got %s %v", w.Start, w.Days)
	}
	if len(w.Unread) != 0 {
		t.Fatalf("want every day of this week read, got %v unread", w.Unread)
	}
	if len(w.Repos) != 2 || w.Repos[0].Repo != "vibe" || w.Repos[1].Repo != "yumbi" {
		t.Fatalf("want vibe then yumbi, the longest first, got %+v", w.Repos)
	}
	vibe := w.Repos[0]
	if want := []float64{1, 0, 0, 2.5, 0, 0, 0}; !reflect.DeepEqual(vibe.Days, want) || vibe.Hours != 3.5 {
		t.Fatalf("want vibe's days %v (3.5h), got %v (%vh)", want, vibe.Days, vibe.Hours)
	}
	if len(vibe.Branches) != 2 || vibe.Branches[0].Branch != "feat/a" || vibe.Branches[0].Hours != 3 || vibe.Branches[1].Hours != 0.5 {
		t.Fatalf("want feat/a 3h then master 0.5h, got %+v", vibe.Branches)
	}
	if w.Hours != 4 {
		t.Fatalf("want the week's 4h (1.5 on Monday, 2.5 on Thursday), got %v", w.Hours)
	}
}

func TestAWeekReachingBackPastTheDaysKeptSaysWhichDaysWerentRead(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	w, err := (&fakeGit{}).desk(l).Week(context.Background(), "2026-11-12", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if w.Start != "2026-11-09" || !reflect.DeepEqual(w.Unread, []string{"2026-11-09", "2026-11-10", "2026-11-11", "2026-11-12"}) {
		t.Fatalf("want Mon 9 to Thu 12 unread (the 13th is the first kept), got %s %v", w.Start, w.Unread)
	}
	if _, err := (&fakeGit{}).desk(l).Week(context.Background(), "2026-11-01", 30*time.Minute); err == nil {
		t.Fatal("want a week wholly before the days kept refused")
	}
	if _, err := (&fakeGit{}).desk(l).Week(context.Background(), "2026-11-30", 30*time.Minute); err == nil {
		t.Fatal("want a week after this one refused")
	}
}

func TestTheDaysKeptAreTodayAndThirteenBefore(t *testing.T) {
	r, err := (&fakeGit{}).desk(Log{Dir: t.TempDir()}).Day(context.Background(), "", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if r.Earliest != FirstKept(day).Format(time.DateOnly) || r.Earliest != "2026-11-13" {
		t.Fatalf("want the earliest day the first one kept, 2026-11-13, got %s", r.Earliest)
	}
}
