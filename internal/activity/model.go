// Package activity works out how a day's time split between repositories and branches, for a timesheet: from the marks
// vibe keeps while its page runs (measured), and what git recorded in the folders vibe has opened (estimated). See
// docs/plan-activity.md.
package activity

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	// joinGap is the longest gap between vibe's marks on a branch that is still one stretch of work.
	joinGap = 15 * time.Minute
	// markLength is the time a mark of vibe's stands for: the agent and the page mark at most once a minute.
	markLength = time.Minute
	// gitGap is the longest gap between git's events on a branch that is counted in full.
	gitGap = time.Hour
	// gitLead is the time counted before the first git event of a run: the work that led to it.
	gitLead = 15 * time.Minute
)

// Mark is a moment vibe saw work on a branch: a prompt, the agent working, the user on the page, a review comment, or the
// folder's git status changing.
type Mark struct {
	At     time.Time `json:"at"`
	Folder string    `json:"folder"`
	Repo   string    `json:"repo"`
	Branch string    `json:"branch"`
	Kind   string    `json:"kind"`
}

// Event is a moment git recorded work on a branch (a commit, a checkout, a merge, a rebase): work done whether or not vibe
// was running, so the time around it is estimated.
type Event struct {
	At     time.Time
	Folder string
	Repo   string
	Branch string
}

// Bullet is a line for the timesheet saying what was done on a branch: a commit's subject, "merged master", "rebased onto
// master". At is when it was authored, which decides its day: a rebase doesn't move last week's commits into today.
type Bullet struct {
	At     time.Time
	Repo   string
	Branch string
	Text   string
}

// Stretch is a span of work on a branch.
type Stretch struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Measured bool      `json:"measured"`
}

// BranchTime is a day's work on one branch of one repository.
type BranchTime struct {
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	Stretches []Stretch `json:"stretches"`
	// Measured and Estimated are the minutes of each, before rounding.
	Measured  int `json:"measuredMinutes"`
	Estimated int `json:"estimatedMinutes"`
	// Hours is the time rounded to the nearest step, in hours: none for less than half a step.
	Hours float64 `json:"hours"`
	// Commits are what was done on the branch that day, a line each: the user's commits' subjects, merges and rebases.
	Commits []string `json:"commits"`
	Folders []string `json:"folders"`
}

// Day is the work of one day, local midnight to local midnight.
type Day struct {
	Date     string       `json:"date"`
	Branches []BranchTime `json:"branches"`
	Hours    float64      `json:"hours"`
}

type branchKey struct{ repo, branch string }

// Summarise is the day starting at midnight's work, each branch's time rounded to the nearest step. Marks, events and bullets from other days may be given: they are left out, though marks and events just
// before midnight can carry a stretch into the day.
func Summarise(midnight time.Time, marks []Mark, events []Event, bullets []Bullet, step time.Duration) Day {
	end := midnight.AddDate(0, 0, 1)
	measured := map[branchKey][]Stretch{}
	estimated := map[branchKey][]Stretch{}
	folders := map[branchKey]map[string]bool{}
	seen := func(k branchKey, folder string) {
		if folders[k] == nil {
			folders[k] = map[string]bool{}
		}
		folders[k][folder] = true
	}
	for k, ms := range marksByBranch(marks) {
		for _, s := range join(times(ms), joinGap, 0, markLength) {
			if s, ok := clip(s, midnight, end); ok {
				s.Measured = true
				measured[k] = append(measured[k], s)
			}
		}
		for _, m := range ms {
			if inDay(m.At, midnight, end) {
				seen(k, m.Folder)
			}
		}
	}
	for k, es := range eventsByBranch(events) {
		for _, s := range join(eventTimes(es), gitGap, gitLead, 0) {
			if s, ok := clip(s, midnight, end); ok {
				estimated[k] = append(estimated[k], without(s, measured[k])...)
			}
		}
		for _, e := range es {
			if inDay(e.At, midnight, end) {
				seen(k, e.Folder)
			}
		}
	}
	keys := map[branchKey]bool{}
	for k := range measured {
		keys[k] = true
	}
	for k, ss := range estimated {
		if len(ss) > 0 {
			keys[k] = true
		}
	}
	notes := map[branchKey][]string{}
	sorted := append([]Bullet(nil), bullets...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	for _, b := range sorted {
		k := branchKey{b.Repo, b.Branch}
		if inDay(b.At, midnight, end) && keys[k] && !contains(notes[k], b.Text) {
			notes[k] = append(notes[k], b.Text)
		}
	}
	day := Day{Date: midnight.Format(time.DateOnly), Branches: []BranchTime{}}
	for k := range keys {
		bt := BranchTime{Repo: k.repo, Branch: k.branch, Commits: notes[k], Folders: sortedKeys(folders[k])}
		bt.Stretches = append(append([]Stretch{}, measured[k]...), estimated[k]...)
		sort.Slice(bt.Stretches, func(i, j int) bool { return bt.Stretches[i].Start.Before(bt.Stretches[j].Start) })
		var m, e time.Duration
		for _, s := range bt.Stretches {
			if s.Measured {
				m += s.End.Sub(s.Start)
			} else {
				e += s.End.Sub(s.Start)
			}
		}
		bt.Measured, bt.Estimated = int(m.Minutes()), int(e.Minutes())
		bt.Hours = rounded(m+e, step)
		if bt.Commits == nil {
			bt.Commits = []string{}
		}
		day.Branches = append(day.Branches, bt)
		day.Hours += bt.Hours
	}
	sort.Slice(day.Branches, func(i, j int) bool {
		a, b := day.Branches[i], day.Branches[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.Branch < b.Branch
	})
	return day
}

// rounded is d in hours, rounded to the nearest whole number of steps, exactly half a step going up: what is under half a
// step (a minute's switch to master to pull) counts for none, and a few minutes past a step (33 of a half hour) don't
// count for another.
func rounded(d, step time.Duration) float64 {
	return math.Round(float64(d)/float64(step)) * step.Hours()
}

func inDay(t, midnight, end time.Time) bool { return !t.Before(midnight) && t.Before(end) }

func marksByBranch(marks []Mark) map[branchKey][]Mark {
	out := map[branchKey][]Mark{}
	for _, m := range marks {
		k := branchKey{m.Repo, m.Branch}
		out[k] = append(out[k], m)
	}
	return out
}

func eventsByBranch(events []Event) map[branchKey][]Event {
	out := map[branchKey][]Event{}
	for _, e := range events {
		k := branchKey{e.Repo, e.Branch}
		out[k] = append(out[k], e)
	}
	return out
}

func times(ms []Mark) []time.Time {
	out := make([]time.Time, len(ms))
	for i, m := range ms {
		out[i] = m.At
	}
	return out
}

func eventTimes(es []Event) []time.Time {
	out := make([]time.Time, len(es))
	for i, e := range es {
		out[i] = e.At
	}
	return out
}

// join makes stretches of moments no more than gap apart, each from lead before its first to tail after its last.
func join(at []time.Time, gap, lead, tail time.Duration) []Stretch {
	if len(at) == 0 {
		return nil
	}
	sorted := append([]time.Time(nil), at...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	var out []Stretch
	cur := Stretch{Start: sorted[0].Add(-lead), End: sorted[0].Add(tail)}
	for _, t := range sorted[1:] {
		if t.Sub(cur.End.Add(-tail)) <= gap {
			if end := t.Add(tail); end.After(cur.End) {
				cur.End = end
			}
			continue
		}
		out = append(out, cur)
		cur = Stretch{Start: t.Add(-lead), End: t.Add(tail)}
	}
	return append(out, cur)
}

func clip(s Stretch, from, to time.Time) (Stretch, bool) {
	if s.Start.Before(from) {
		s.Start = from
	}
	if s.End.After(to) {
		s.End = to
	}
	return s, s.End.After(s.Start)
}

// without is what of s isn't covered by any of cover: time measured on a branch is counted once, as measured.
func without(s Stretch, cover []Stretch) []Stretch {
	left := []Stretch{s}
	for _, c := range cover {
		var next []Stretch
		for _, l := range left {
			if !c.Start.Before(l.End) || !c.End.After(l.Start) {
				next = append(next, l)
				continue
			}
			if c.Start.After(l.Start) {
				next = append(next, Stretch{Start: l.Start, End: c.Start})
			}
			if c.End.Before(l.End) {
				next = append(next, Stretch{Start: c.End, End: l.End})
			}
		}
		left = next
	}
	return left
}

func contains(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		if strings.TrimSpace(k) != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
