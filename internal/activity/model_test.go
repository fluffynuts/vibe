package activity

import (
	"reflect"
	"testing"
	"time"
)

var zone = time.FixedZone("SAST", 2*60*60)

var day = time.Date(2026, 11, 26, 0, 0, 0, 0, zone)

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-11-26 "+hhmm, zone)
	if err != nil {
		panic(err)
	}
	return t
}

func mark(hhmm, repo, branch string) Mark {
	return Mark{At: at(hhmm), Folder: "/code/" + repo, Repo: repo, Branch: branch, Kind: "agent"}
}

func event(hhmm, repo, branch string) Event {
	return Event{At: at(hhmm), Folder: "/code/" + repo, Repo: repo, Branch: branch}
}

func only(t *testing.T, d Day) BranchTime {
	t.Helper()
	if len(d.Branches) != 1 {
		t.Fatalf("want one branch, got %+v", d.Branches)
	}
	return d.Branches[0]
}

func span(from, to string, measured bool) Stretch {
	return Stretch{Start: at(from), End: at(to), Measured: measured}
}

func TestMarksLessThanFifteenMinutesApartAreOneStretch(t *testing.T) {
	d := Summarise(day, []Mark{mark("09:00", "r", "b"), mark("09:14", "r", "b"), mark("09:20", "r", "b")}, nil, nil, 15*time.Minute)
	got := only(t, d)
	if want := []Stretch{span("09:00", "09:21", true)}; !reflect.DeepEqual(got.Stretches, want) {
		t.Fatalf("stretches: want %v, got %v", want, got.Stretches)
	}
	if got.Measured != 21 || got.Estimated != 0 {
		t.Fatalf("want 21 measured minutes, got %d measured, %d estimated", got.Measured, got.Estimated)
	}
}

func TestMarksMoreThanFifteenMinutesApartAreTwoStretches(t *testing.T) {
	d := Summarise(day, []Mark{mark("09:00", "r", "b"), mark("09:16", "r", "b")}, nil, nil, 15*time.Minute)
	if want := []Stretch{span("09:00", "09:01", true), span("09:16", "09:17", true)}; !reflect.DeepEqual(only(t, d).Stretches, want) {
		t.Fatalf("want %v, got %v", want, only(t, d).Stretches)
	}
}

func TestGitEventsUpToAnHourApartCountInFullWithFifteenMinutesBeforeTheFirst(t *testing.T) {
	d := Summarise(day, nil, []Event{event("10:00", "r", "b"), event("10:55", "r", "b"), event("13:00", "r", "b")}, nil, 15*time.Minute)
	got := only(t, d)
	want := []Stretch{span("09:45", "10:55", false), span("12:45", "13:00", false)}
	if !reflect.DeepEqual(got.Stretches, want) {
		t.Fatalf("want %v, got %v", want, got.Stretches)
	}
	if got.Estimated != 85 || got.Measured != 0 {
		t.Fatalf("want 85 estimated minutes, got %d estimated, %d measured", got.Estimated, got.Measured)
	}
}

func TestEstimatedTimeOverlappingMeasuredTimeOnTheSameBranchCountsOnceAsMeasured(t *testing.T) {
	marks := []Mark{mark("10:00", "r", "b"), mark("10:10", "r", "b")}
	events := []Event{event("10:20", "r", "b")}
	got := only(t, Summarise(day, marks, events, nil, 15*time.Minute))
	want := []Stretch{span("10:00", "10:11", true), span("10:11", "10:20", false)}
	if !reflect.DeepEqual(got.Stretches, want) {
		t.Fatalf("want %v, got %v", want, got.Stretches)
	}
	if got.Measured != 11 || got.Estimated != 9 {
		t.Fatalf("want 11 measured and 9 estimated, got %d and %d", got.Measured, got.Estimated)
	}
}

func TestMeasuredTimeOnAnotherBranchDoesNotCoverEstimatedTime(t *testing.T) {
	d := Summarise(day, []Mark{mark("10:00", "r", "a")}, []Event{event("10:10", "r", "b")}, nil, 15*time.Minute)
	if len(d.Branches) != 2 || d.Branches[1].Estimated != 15 {
		t.Fatalf("want b's 15 estimated minutes untouched, got %+v", d.Branches)
	}
}

func TestOverlappingBranchesEachCountInFull(t *testing.T) {
	var marks []Mark
	for _, hhmm := range []string{"09:00", "09:10", "09:20", "09:29"} {
		marks = append(marks, mark(hhmm, "r", "a"), mark(hhmm, "other", "b"))
	}
	d := Summarise(day, marks, nil, nil, 30*time.Minute)
	if len(d.Branches) != 2 || d.Branches[0].Hours != 0.5 || d.Branches[1].Hours != 0.5 || d.Hours != 1 {
		t.Fatalf("want both branches at 0.5 and a total of 1, got %+v total %v", d.Branches, d.Hours)
	}
}

func TestRoundingIsUpToTheStepWithAtLeastOneStep(t *testing.T) {
	for _, c := range []struct {
		name    string
		minutes int
		step    time.Duration
		want    float64
	}{
		{"a minute is a whole quarter hour", 1, 15 * time.Minute, 0.25},
		{"exactly a step stays one step", 30, 30 * time.Minute, 0.5},
		{"a minute over goes up", 31, 30 * time.Minute, 1},
		{"a short PR fix is half an hour", 10, 30 * time.Minute, 0.5},
		{"hours", 61, time.Hour, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			var marks []Mark
			start := at("09:00")
			for i := 0; i < c.minutes; i++ {
				marks = append(marks, Mark{At: start.Add(time.Duration(i) * time.Minute), Repo: "r", Branch: "b"})
			}
			if got := only(t, Summarise(day, marks, nil, nil, c.step)).Hours; got != c.want {
				t.Fatalf("want %v hours, got %v", c.want, got)
			}
		})
	}
}

func TestTheDayIsCutAtLocalMidnight(t *testing.T) {
	before := Mark{At: day.Add(-5 * time.Minute), Repo: "r", Branch: "b"}
	after := Mark{At: day.Add(5 * time.Minute), Repo: "r", Branch: "b"}
	got := only(t, Summarise(day, []Mark{before, after}, nil, nil, 15*time.Minute))
	if want := []Stretch{{Start: day, End: day.Add(6 * time.Minute), Measured: true}}; !reflect.DeepEqual(got.Stretches, want) {
		t.Fatalf("want the stretch from midnight, got %v", got.Stretches)
	}
	tomorrow := Summarise(day.AddDate(0, 0, 1), []Mark{mark("23:58", "r", "b")}, nil, nil, 15*time.Minute)
	if len(tomorrow.Branches) != 0 {
		t.Fatalf("a mark before midnight that ends before it shows nothing the next day, got %+v", tomorrow.Branches)
	}
	yesterday := Summarise(day.AddDate(0, 0, -1), []Mark{after}, nil, nil, 15*time.Minute)
	if len(yesterday.Branches) != 0 {
		t.Fatalf("a mark after midnight shows nothing the day before, got %+v", yesterday.Branches)
	}
}

func TestBulletsAreThoseAuthoredThatDayOnBranchesWithTime(t *testing.T) {
	bullets := []Bullet{
		{At: at("09:30"), Repo: "r", Branch: "b", Text: "added the button"},
		{At: day.AddDate(0, 0, -6), Repo: "r", Branch: "b", Text: "last week's work, rebased today"},
		{At: at("09:40"), Repo: "r", Branch: "b", Text: "rebased onto master"},
		{At: at("09:45"), Repo: "r", Branch: "b", Text: "added the button"},
		{At: at("09:50"), Repo: "r", Branch: "idle", Text: "on a branch with no time"},
	}
	got := only(t, Summarise(day, nil, []Event{event("09:40", "r", "b")}, bullets, 15*time.Minute))
	if want := []string{"added the button", "rebased onto master"}; !reflect.DeepEqual(got.Bullets, want) {
		t.Fatalf("want %q, got %q", want, got.Bullets)
	}
}

func TestBranchesAreGroupedByRepositoryAcrossFolders(t *testing.T) {
	marks := []Mark{
		{At: at("09:00"), Folder: "/code/yumbi", Repo: "yumbi-web", Branch: "b"},
		{At: at("09:05"), Folder: "/code/yumbi.alt", Repo: "yumbi-web", Branch: "b"},
	}
	got := only(t, Summarise(day, marks, nil, nil, 15*time.Minute))
	if want := []string{"/code/yumbi", "/code/yumbi.alt"}; !reflect.DeepEqual(got.Folders, want) {
		t.Fatalf("want both folders, got %v", got.Folders)
	}
	if want := []Stretch{span("09:00", "09:06", true)}; !reflect.DeepEqual(got.Stretches, want) {
		t.Fatalf("want one stretch, got %v", got.Stretches)
	}
}

func TestBranchesAreInRepositoryThenBranchOrder(t *testing.T) {
	d := Summarise(day, []Mark{mark("09:00", "z", "a"), mark("09:00", "a", "z"), mark("09:00", "a", "b")}, nil, nil, 15*time.Minute)
	var got []string
	for _, b := range d.Branches {
		got = append(got, b.Repo+" "+b.Branch)
	}
	if want := []string{"a b", "a z", "z a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}
