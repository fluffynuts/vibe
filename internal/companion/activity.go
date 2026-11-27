package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vibe/internal/activity"
)

// The Activity tab: how the day's time split between repositories and branches, for a timesheet, from what vibe saw and
// what git recorded in every folder vibe has opened (see docs/plan-activity.md).

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
}

// activityWithin bounds reading a day: a helper process for each folder's git, the first time.
const activityWithin = 60 * time.Second

var activityFeature = feature{func(s *Server) bool { return s.cfg.Activity != nil },
	"this page has no Activity tab", 1 << 10}

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
func (s *Server) mark(kind string) {
	if s.cfg.Mark == nil {
		return
	}
	if git := s.gitInfo(); git != nil {
		s.cfg.Mark(kind, git.Branch)
	}
}

// markGitChange marks the folder's git status having changed since it was last read.
func (s *Server) markGitChange(was, now *GitInfo) {
	if s.cfg.Mark == nil || was == nil || now == nil || *was == *now {
		return
	}
	s.cfg.Mark(activity.KindGit, now.Branch)
}
