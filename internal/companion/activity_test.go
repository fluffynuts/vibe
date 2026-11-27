package companion

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vibe/internal/activity"
)

// marks is a page's Config.Mark: what it was told, as "kind branch", and " prompt" after when it was given one.
type marks struct {
	mu   sync.Mutex
	seen []string
}

func (m *marks) mark(kind, branch, prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prompt != "" {
		branch += " " + prompt
	}
	m.seen = append(m.seen, kind+" "+branch)
}

func (m *marks) said() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.seen...)
}

func activityServer(t *testing.T) (*Server, *marks, *string) {
	t.Helper()
	s := newServer(&fakeSbx{})
	kept := ""
	colourBlind := false
	m := &marks{}
	s.cfg.Workspace = t.TempDir()
	s.cfg.Git = func(string) *GitInfo { return &GitInfo{Branch: "feat/x"} }
	s.cfg.Mark = m.mark
	s.cfg.Activity = &ActivityTab{
		Desk:           &activity.Desk{Log: activity.Log{Dir: t.TempDir()}, Folders: func() []string { return nil }},
		Rounding:       func() string { return kept },
		SetRounding:    func(r string) error { kept = r; return nil },
		Fills:          &activity.Fills{Path: t.TempDir() + "/fills.json"},
		ColourBlind:    func() bool { return colourBlind },
		SetColourBlind: func(on bool) error { colourBlind = on; return nil },
	}
	return s, m, &kept
}

func TestTheActivityIsTheDayWithItsRounding(t *testing.T) {
	s, _, _ := activityServer(t)
	rec := do(t, s, req{path: "/api/activity", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got struct {
		Report    activity.Report      `json:"report"`
		Attention []activity.Attention `json:"attention"`
		Rounding  string               `json:"rounding"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Rounding != "30m" || got.Report.Day.Date != got.Report.Today || got.Attention == nil {
		t.Fatalf("want today at the default half hour, got %s", rec.Body)
	}
	if rec := do(t, s, req{path: "/api/activity?day=1999-01-01", headers: authed(nil)}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "outside the days kept") {
		t.Fatalf("want a day long gone refused, got %d %s", rec.Code, rec.Body)
	}
}

func TestReadingTheActivityPrunesTheDaysNoLongerKept(t *testing.T) {
	s, _, _ := activityServer(t)
	dir := s.cfg.Activity.Desk.Log.Dir
	old := filepath.Join(dir, "2000-01-01")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, req{path: "/api/activity", headers: authed(nil)}); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("want a day long gone pruned, got %v", err)
	}
}

func TestTheRoundingIsOneOfThoseOfferedAndIsKept(t *testing.T) {
	s, _, kept := activityServer(t)
	if rec := do(t, s, post("/api/activity/rounding", `{"rounding":"7m"}`)); rec.Code != http.StatusBadRequest || *kept != "" {
		t.Fatalf("want 7m refused and nothing kept, got %d %s, kept %q", rec.Code, rec.Body, *kept)
	}
	if rec := do(t, s, post("/api/activity/rounding", `{"rounding":"1h"}`)); rec.Code != http.StatusOK || *kept != "1h" {
		t.Fatalf("want 1h kept, got %d %s, kept %q", rec.Code, rec.Body, *kept)
	}
	if !strings.Contains(do(t, s, req{path: "/api/activity", headers: authed(nil)}).Body.String(), `"rounding":"1h"`) {
		t.Fatal("want the rounding kept used")
	}
}

func TestTheUserBusyOnThePageIsMarkedOnTheFoldersBranch(t *testing.T) {
	s, m, _ := activityServer(t)
	if rec := do(t, s, post("/api/activity/seen", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := m.said(); len(got) != 1 || got[0] != "page feat/x" {
		t.Fatalf("want the page marked on feat/x, got %v", got)
	}
}

func TestAPromptSentIsMarked(t *testing.T) {
	s := converseServer(&fakeAgent{})
	m := &marks{}
	s.cfg.Workspace = t.TempDir()
	s.cfg.Git = func(string) *GitInfo { return &GitInfo{Branch: "feat/x"} }
	s.cfg.Mark = m.mark
	send(t, s, "/api/conversation/send", `{"prompt":"hello"}`)
	idle(t, s)
	if got := m.said(); len(got) != 1 || got[0] != "prompt feat/x hello" {
		t.Fatalf("want the prompt marked on feat/x with what was asked, got %v", got)
	}
}

func TestTheFoldersGitStatusChangingIsMarkedButNotItsFirstReading(t *testing.T) {
	s, m, _ := activityServer(t)
	modified := 0
	s.cfg.Git = func(string) *GitInfo { return &GitInfo{Branch: "feat/x", Modified: modified} }
	s.gitInfo()
	s.cache.clear()
	s.gitInfo()
	if got := m.said(); len(got) != 0 {
		t.Fatalf("want nothing marked while nothing changes, got %v", got)
	}
	modified = 1
	s.cache.clear()
	s.gitInfo()
	if got := m.said(); len(got) != 1 || got[0] != "git feat/x" {
		t.Fatalf("want the change marked, got %v", got)
	}
}

func TestAPageWithNoMarksKeepsNone(t *testing.T) {
	s, _, _ := activityServer(t)
	s.cfg.Mark = nil
	if rec := do(t, s, post("/api/activity/seen", `{}`)); rec.Code != http.StatusOK {
		t.Fatalf("want the page's word taken, and nothing done, got %d %s", rec.Code, rec.Body)
	}
}

func TestTheActivitySaysEachBranchsSummaryAndARetryNeedsAKey(t *testing.T) {
	s, _, _ := activityServer(t)
	dir := t.TempDir()
	s.cfg.Activity.Summaries = &activity.Summariser{Dir: dir, Rules: dir + "/rules.md", Unavailable: "not here"}
	if body := do(t, s, req{path: "/api/activity", headers: authed(nil)}).Body.String(); !strings.Contains(body, `"summaries":[]`) {
		t.Fatalf("want the summaries said, one for each branch (none today), got %s", body)
	}
	if rec := do(t, s, post("/api/activity/summary/retry", `{"key":"../x"}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("want a key that isn't one refused, got %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/activity/summary/retry", `{"key":"0123456789abcdef0123456789abcdef"}`)); rec.Code != http.StatusOK {
		t.Fatalf("want a retry of one not kept taken quietly, got %d %s", rec.Code, rec.Body)
	}
}

func TestATicketLinkIsKeptForABranchAndOnlyAWebAddress(t *testing.T) {
	s, _, _ := activityServer(t)
	s.cfg.Activity.Tickets = &activity.Tickets{Path: t.TempDir() + "/tickets.json"}
	if rec := do(t, s, post("/api/activity/ticket", `{"repo":"r","branch":"b","url":"javascript:alert(1)"}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("want a link that isn't a web address refused, got %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/activity/ticket", `{"repo":"r","branch":"b","url":"https://trello.com/c/abc"}`)); rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if got := s.cfg.Activity.Tickets.All()[activity.TicketKey("r", "b")]; got != "https://trello.com/c/abc" {
		t.Fatalf("want it kept, got %q", got)
	}
	if rec := do(t, s, post("/api/activity/ticket", `{"repo":"r","url":"https://x"}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("want a missing branch refused, got %d", rec.Code)
	}
}

func TestTheWeekIsTheReportWithEachRepositorysFillAndTheColourBlindChoice(t *testing.T) {
	s, _, _ := activityServer(t)
	rec := do(t, s, req{path: "/api/activity/week", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got struct {
		Week        activity.WeekReport `json:"week"`
		Fills       map[string]int      `json:"fills"`
		ColourBlind bool                `json:"colourBlind"`
		Rounding    string              `json:"rounding"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"summaries":{}`) {
		t.Fatalf("want the week's summaries said, by branch (none this week), got %s", rec.Body)
	}
	if len(got.Week.Days) != 7 || got.Fills == nil || got.ColourBlind || got.Rounding != "30m" {
		t.Fatalf("want this week, its fills (none yet), not colour-blind, at the half hour, got %s", rec.Body)
	}
	if rec := do(t, s, req{path: "/api/activity/week?day=1999-01-01", headers: authed(nil)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("want a week long gone refused, got %d %s", rec.Code, rec.Body)
	}
}

func TestTheColourBlindChoiceIsKept(t *testing.T) {
	s, _, _ := activityServer(t)
	if rec := do(t, s, post("/api/activity/colour-blind", `{"on":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(do(t, s, req{path: "/api/activity/week", headers: authed(nil)}).Body.String(), `"colourBlind":true`) {
		t.Fatal("want the choice kept and said")
	}
	if rec := do(t, s, post("/api/activity/colour-blind", `{"on":"yes"}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("want a choice that isn't true or false refused, got %d", rec.Code)
	}
}

func TestTheSummaryRulesAreReadAndSavedUnlessChangedOnDisk(t *testing.T) {
	s, _, _ := activityServer(t)
	if rec := do(t, s, req{path: "/api/activity/rules", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Fatalf("want no rules on a page with no summaries, got %d", rec.Code)
	}
	dir := t.TempDir()
	s.cfg.Activity.Summaries = &activity.Summariser{Dir: dir, Rules: dir + "/day.md", WeekRules: dir + "/week.md"}
	rec := do(t, s, req{path: "/api/activity/rules", headers: authed(nil)})
	var got struct {
		Files []activity.RulesFile `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Files) != 2 {
		t.Fatalf("want the day's and the week's, got %d %s", rec.Code, rec.Body)
	}
	body, _ := json.Marshal(map[string]string{"which": "week", "text": "Short.", "base": got.Files[1].Text})
	if rec := do(t, s, post("/api/activity/rules", string(body))); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"text":"Short."`) {
		t.Fatalf("want it kept, got %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/activity/rules", string(body))); rec.Code != http.StatusConflict {
		t.Fatalf("want a save over rules since changed refused, got %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, post("/api/activity/rules", `{"which":"month","text":"x","base":""}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("want other rules refused, got %d", rec.Code)
	}
}
