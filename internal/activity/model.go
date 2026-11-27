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
	// Text is the start of the prompt a prompt's mark is for (PromptStart), "" for other marks and prompts vibe made.
	Text string `json:"text,omitempty"`
}

// Event is a moment git recorded work on a branch (a commit, a checkout, a merge, a rebase): work done whether or not vibe
// was running, so the time around it is estimated.
type Event struct {
	At     time.Time
	Folder string
	Repo   string
	Branch string
	// Link is a move onto the branch that is no work of itself (a checkout, a pull, a reset): it joins work before and
	// after it, and starts what follows, but alone counts for nothing.
	Link bool
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
	// Ticket is the link the user gave the branch to its ticket, "" for none (see Tickets).
	Ticket string `json:"ticket,omitempty"`
	// Commits are what was done on the branch that day, a line each: the user's commits' subjects, merges and rebases.
	Commits []string `json:"commits"`
	// Asked are the starts of the day's prompts on a branch with no commits, oldest first: what the work was, when no commit
	// says (planning, investigating). A branch with commits has none: its commits tell its day.
	Asked   []string `json:"asked,omitempty"`
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
		for _, s := range gitStretches(es) {
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
	asked := askedOn(marks, midnight, end)
	raw := map[int]time.Duration{}
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
		raw[len(day.Branches)] = m + e
		if bt.Commits == nil {
			bt.Commits = []string{}
		}
		if len(bt.Commits) == 0 {
			bt.Asked = asked[k]
		}
		day.Branches = append(day.Branches, bt)

	}
	times := make([]time.Duration, len(day.Branches))
	for i := range day.Branches {
		times[i] = raw[i]
	}
	for i, h := range sharedOut(times, step) {
		day.Branches[i].Hours = h
		day.Hours += h
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

// sharedOut is each of times in hours, in whole steps, adding up to their total rounded to the nearest step (exactly half
// going up): each gets its whole steps, and the steps left go to those with the most left over, the longer first where
// that's even. So no time is more than a step off, and small tasks that each round to nothing don't lose the day their
// sum: four 10-minute fixes at half-hour steps keep their half hour, on the longest.
func sharedOut(times []time.Duration, step time.Duration) []float64 {
	var total time.Duration
	steps := make([]int, len(times))
	order := make([]int, len(times))
	for i, t := range times {
		total += t
		steps[i] = int(t / step)
		order[i] = i
	}
	left := int(math.Round(float64(total) / float64(step)))
	for _, n := range steps {
		left -= n
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := times[order[a]]%step, times[order[b]]%step
		if ra != rb {
			return ra > rb
		}
		return times[order[a]] > times[order[b]]
	})
	for i := 0; i < left && i < len(order); i++ {
		steps[order[i]]++
	}
	out := make([]float64, len(times))
	for i, n := range steps {
		out[i] = float64(n) * step.Hours()
	}
	return out
}

// gitStretches are the stretches git's events on a branch stand for: those no more than gitGap apart are one stretch,
// from gitLead before its first piece of work (or the checkout before it, if earlier) to its last. Links (checkouts, pulls)
// join work either side and start what follows them, but a stretch with no work in it counts for nothing.
func gitStretches(es []Event) []Stretch {
	sorted := append([]Event(nil), es...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	var out []Stretch
	for i := 0; i < len(sorted); {
		j := i + 1
		for j < len(sorted) && sorted[j].At.Sub(sorted[j-1].At) <= gitGap {
			j++
		}
		var first, last time.Time
		for _, e := range sorted[i:j] {
			if e.Link {
				continue
			}
			if first.IsZero() {
				first = e.At
			}
			last = e.At
		}
		if !first.IsZero() {
			start := first.Add(-gitLead)
			if sorted[i].At.Before(start) {
				start = sorted[i].At
			}
			out = append(out, Stretch{Start: start, End: last})
		}
		i = j
	}
	return out
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

// askedOn are the prompts' starts in the day, by branch, oldest first, once each.
func askedOn(marks []Mark, midnight, end time.Time) map[branchKey][]string {
	sorted := append([]Mark(nil), marks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	out := map[branchKey][]string{}
	for _, m := range sorted {
		k := branchKey{m.Repo, m.Branch}
		if m.Kind == KindPrompt && m.Text != "" && inDay(m.At, midnight, end) && !contains(out[k], m.Text) {
			out[k] = append(out[k], m.Text)
		}
	}
	return out
}

// PromptKept is how many characters of a prompt its mark keeps: enough to say what was asked, not the whole of it.
const PromptKept = 200

// PromptStart is what a prompt's mark keeps of it: its first PromptKept characters, on one line.
func PromptStart(prompt string) string {
	r := []rune(strings.Join(strings.Fields(prompt), " "))
	if len(r) <= PromptKept {
		return string(r)
	}
	return string(r[:PromptKept]) + "…"
}
