package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"vibe/internal/activity"
)

// The Activity tab: how the day's time split between repositories and branches, for a timesheet, from what vibe saw and
// what git recorded in every folder vibe has opened (see docs/adr/2026-11-26-activity.md).

// ActivityTab is what the Activity tab reads, and the rounding it is shown with.
type ActivityTab struct {
	Desk *activity.Desk
	// Rounding is the step chosen ("15m", "30m", "1h"; "" for the default); SetRounding keeps a new one.
	Rounding    func() string
	SetRounding func(string) error
	// Summaries makes and keeps the branches' summaries; nil means none are shown.
	Summaries *activity.Summariser
	// Tickets keeps the branches' ticket links; nil means none can be set.
	Tickets *activity.Tickets
	// Fills keeps the fill each repository is drawn with in the week's charts; nil means each is given its place in the week.
	Fills *activity.Fills
	// ColourBlind says whether the week is drawn in the fills that stay apart for colour-blind readers; SetColourBlind keeps a
	// new choice. Nil means not, and the choice can't be kept.
	ColourBlind    func() bool
	SetColourBlind func(bool) error

	pruning  sync.Mutex
	prunedOn string
}

// prune removes the log's days and the summaries older than activity.KeepDays, the first time the tab is read on today (a
// date of the report's: "YYYY-MM-DD"), saying what couldn't be removed.
func (a *ActivityTab) prune(today string) []string {
	a.pruning.Lock()
	defer a.pruning.Unlock()
	if a.prunedOn == today {
		return nil
	}
	day, err := time.ParseInLocation(time.DateOnly, today, time.Local)
	if err != nil {
		return []string{"couldn't prune the activity log: " + err.Error()}
	}
	a.prunedOn = today
	first := activity.FirstKept(day)
	var problems []string
	if err := a.Desk.Log.Prune(first); err != nil {
		problems = append(problems, "couldn't prune the activity log: "+err.Error())
	}
	if a.Summaries != nil {
		if err := a.Summaries.Prune(first); err != nil {
			problems = append(problems, "couldn't prune the summaries: "+err.Error())
		}
	}
	return problems
}

// activityWithin bounds reading a day: a helper process for each folder's git, the first time.
const activityWithin = 60 * time.Second

var activityFeature = feature{func(s *Server) bool { return s.cfg.Activity != nil },
	"this page has no Activity tab", 1 << 10}

// maxSummaryRules is the longest the summaries' rules may be. A save sends them twice (the text, and the text it was loaded as),
// so its request may be three times that.
const maxSummaryRules = 64 << 10

var activityRulesFeature = feature{activityFeature.present, activityFeature.missing, 3 * maxSummaryRules}

// activityTicketFeature bounds a request to keep a ticket link by the longest link kept, and room for the repository's and
// branch's names around it.
var activityTicketFeature = feature{activityFeature.present, activityFeature.missing, activity.MaxTicketLink + 4<<10}

func (s *Server) rounding() string {
	r := ""
	if s.cfg.Activity.Rounding != nil {
		r = s.cfg.Activity.Rounding()
	}
	if _, ok := activity.Roundings[r]; !ok {
		return activity.DefaultRounding
	}
	return r
}

func (s *Server) activityState(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), activityWithin)
	defer cancel()
	rounding := s.rounding()
	report, err := s.cfg.Activity.Desk.Day(ctx, r.URL.Query().Get("day"), activity.Roundings[rounding])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	attention, problems := s.cfg.Activity.Desk.Attention(ctx)
	problems = append(problems, s.cfg.Activity.prune(report.Today)...)
	summaries := []activity.SummaryState{}
	if s.cfg.Activity.Summaries != nil {
		summaries = s.cfg.Activity.Summaries.States(report.Day)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"report":    report,
		"summaries": summaries,
		"attention": attention,
		"problems":  append(report.Problems, problems...),
		"rounding":  rounding,
	})
}

// activityWeek is the week (Monday to Sunday) a day is in, with each repository's fill.
func (s *Server) activityWeek(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), activityWithin)
	defer cancel()
	tab := s.cfg.Activity
	rounding := s.rounding()
	week, err := tab.Desk.Week(ctx, r.URL.Query().Get("day"), activity.Roundings[rounding])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fills := map[string]int{}
	if tab.Fills != nil {
		first, _ := time.ParseInLocation(time.DateOnly, week.Earliest, time.Local)
		got, err := tab.Fills.Assign(week.Repos, week.Days, first)
		if err != nil {
			week.Problems = append(week.Problems, "couldn't keep the charts' colours: "+err.Error())
		}
		if got != nil {
			fills = got
		}
	} else {
		for i, repo := range week.Repos {
			fills[repo.Repo] = i
		}
	}
	week.Problems = append(week.Problems, tab.prune(week.Today)...)
	attention, problems := tab.Desk.Attention(ctx)
	week.Problems = append(week.Problems, problems...)
	summaries := map[string]activity.SummaryState{}
	if tab.Summaries != nil {
		summaries = tab.Summaries.WeekStates(week)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"week":        week,
		"summaries":   summaries,
		"attention":   attention,
		"fills":       fills,
		"colourBlind": tab.ColourBlind != nil && tab.ColourBlind(),
		"rounding":    rounding,
	})
}

func (s *Server) activityColourBlind(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On *bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.On == nil {
		writeError(w, http.StatusBadRequest, "expected {\"on\": true or false}")
		return
	}
	if s.cfg.Activity.SetColourBlind == nil {
		writeError(w, http.StatusNotFound, "the choice can't be kept here")
		return
	}
	if err := s.cfg.Activity.SetColourBlind(*req.On); err != nil {
		writeError(w, http.StatusInternalServerError, "the choice couldn't be kept: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"on": *req.On})
}

// activityRules are the summaries' rules, the day's and the week's, for the Admin tab to edit.
func (s *Server) activityRules(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Activity.Summaries == nil {
		writeError(w, http.StatusNotFound, "this page makes no summaries")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": s.cfg.Activity.Summaries.RulesFiles()})
}

// activityRulesSave keeps the day's or the week's rules, unless they changed since the page read them.
func (s *Server) activityRulesSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Which string `json:"which"`
		Text  string `json:"text"`
		Base  string `json:"base"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the rules are too long to keep: at most %d KB", maxSummaryRules>>10))
			return
		}
		writeError(w, http.StatusBadRequest, "expected {which, text, base}: the rules to keep, and the text they were loaded as")
		return
	}
	if s.cfg.Activity.Summaries == nil {
		writeError(w, http.StatusNotFound, "this page makes no summaries")
		return
	}
	if len(req.Text) > maxSummaryRules {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the rules are too long to keep: at most %d KB", maxSummaryRules>>10))
		return
	}
	if req.Which != "day" && req.Which != "week" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%q isn't the day's or the week's rules", req.Which))
		return
	}
	saved, err := s.cfg.Activity.Summaries.SaveRules(req.Which, req.Text, req.Base)
	if errors.Is(err, activity.ErrRulesChanged) {
		writeError(w, http.StatusConflict, "Not saved: the file was changed outside this page after it was loaded here (by hand, or from another vibe's page), "+
			"and saving would undo that change. Reload to see the file as it is now, then make your change again.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the rules couldn't be kept: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) activityRounding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rounding string `json:"rounding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "expected a rounding")
		return
	}
	if _, ok := activity.Roundings[req.Rounding]; !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%q isn't a rounding: 15m, 30m or 1h", req.Rounding))
		return
	}
	if s.cfg.Activity.SetRounding == nil {
		writeError(w, http.StatusNotFound, "the rounding can't be changed here")
		return
	}
	if err := s.cfg.Activity.SetRounding(req.Rounding); err != nil {
		writeError(w, http.StatusInternalServerError, "the rounding couldn't be kept: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"rounding": req.Rounding})
}

// activitySummaryRetry forgets a summary that failed, so the next look makes it again.
func (s *Server) activitySummaryRetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "expected a summary's key")
		return
	}
	if s.cfg.Activity.Summaries == nil {
		writeError(w, http.StatusNotFound, "this page shows no summaries")
		return
	}
	if err := s.cfg.Activity.Summaries.Retry(req.Key); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// activityTicket keeps (or, with no link, forgets) the link to a branch's ticket.
func (s *Server) activityTicket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Repo == "" || req.Branch == "" {
		writeError(w, http.StatusBadRequest, "expected a repository, a branch and a link")
		return
	}
	if s.cfg.Activity.Tickets == nil {
		writeError(w, http.StatusNotFound, "ticket links can't be kept here")
		return
	}
	if err := s.cfg.Activity.Tickets.Set(req.Repo, req.Branch, req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// activitySeen is the user busy on the page (typing, clicking, scrolling), which the page says at most once a minute.
func (s *Server) activitySeen(w http.ResponseWriter, r *http.Request) {
	s.mark(activity.KindPage)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// mark records kind of work on the folder's branch as it is now, when the page keeps marks (Config.Mark).
func (s *Server) mark(kind string) { s.markWith(kind, "") }

// markPrompt records the user's prompt, sent from the page, on the folder's branch.
func (s *Server) markPrompt(prompt string) { s.markWith(activity.KindPrompt, prompt) }

func (s *Server) markWith(kind, prompt string) {
	if s.cfg.Mark == nil {
		return
	}
	if git := s.gitInfo(); git != nil {
		s.cfg.Mark(kind, git.Branch, prompt)
	}
}

// markGitChange marks the folder's git status having changed since it was last read.
func (s *Server) markGitChange(was, now *GitInfo) {
	if s.cfg.Mark == nil || was == nil || now == nil || *was == *now {
		return
	}
	s.cfg.Mark(activity.KindGit, now.Branch, "")
}
