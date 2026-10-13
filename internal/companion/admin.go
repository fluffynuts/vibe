package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Admin is what the page's Admin tab can do to the sandbox; nil means the
// page has no such tab. Everything it does, the CLI can too: the page only
// saves going to a terminal for it.
type Admin interface {
	// Status is the sandbox as the tab shows it. It may be slow (it asks
	// sbx, and a running sandbox for its memory and disk use).
	Status() AdminStatus
	// Upgrades lists newer releases of vibe and Docker SBX, checking
	// GitHub afresh when fresh, and otherwise saying what the last check
	// found. It may be slow.
	Upgrades(fresh bool) []string
	// Do carries out an action, saying what it does as it goes through log.
	// ctx ending (the tab's Cancel, or 'vibe --cancel') ends it.
	Do(ctx context.Context, action AdminAction, log func(string)) error
}

// What the tab can ask for.
const (
	AdminStart     = "start"     // start the sandbox, and attach the terminal again
	AdminStop      = "stop"      // stop it, keeping vibe and this page up
	AdminReInit    = "reinit"    // rebuild it from its profile, as vibe -r
	AdminReCompose = "recompose" // compose its profile afresh, then rebuild, as vibe -C
	AdminRestore   = "restore"   // put a backup of its profile back, then re-compose
	AdminMemories  = "memories"  // copy the agent's memories out now
)

// adminKinds are the actions, and whether each takes the sandbox away from
// under the agent.
var adminKinds = map[string]bool{
	AdminStart: false, AdminStop: true, AdminReInit: true, AdminReCompose: true, AdminRestore: true, AdminMemories: false,
}

// adminSlow is, for each action, many times how long it normally takes.
// Past it, the tab says the action may be stuck and what to check — and
// keeps waiting: stopping sbx half-way through a rebuild could leave a
// broken sandbox, worse than a slow one. A variable for tests.
var adminSlow = map[string]time.Duration{
	AdminStart: 3 * time.Minute, AdminStop: 3 * time.Minute, AdminMemories: 3 * time.Minute,
	AdminReInit: 15 * time.Minute, AdminReCompose: 15 * time.Minute, AdminRestore: 15 * time.Minute,
}

// AdminAction is an action the tab asked for.
type AdminAction struct {
	Kind string `json:"kind"`
	// Features is what to compose the profile from, for AdminReCompose.
	Features []string `json:"features,omitempty"`
	// Backup is which backup to put back, for AdminRestore: an ID from
	// AdminStatus.Backups.
	Backup string `json:"backup,omitempty"`
}

// AdminStatus is the sandbox as the Admin tab shows it.
type AdminStatus struct {
	Exists  bool `json:"exists"`
	Running bool `json:"running"`
	// Info is what vibe --info prints for it.
	Info    string `json:"info"`
	Profile string `json:"profile"`
	// Guided says the profile records the features it was composed from,
	// which is what re-composing needs.
	Guided   bool           `json:"guided"`
	Features []AdminFeature `json:"features"`
	Backups  []AdminBackup  `json:"backups"`
	GitToken AdminGitToken  `json:"gitToken"`
	// Commands are the CLI's ways to do what the tab does, by action
	// (and "upgrade").
	Commands map[string]string `json:"commands"`
	Error    string            `json:"error,omitempty"`
}

// AdminFeature is a feature a profile can be composed from.
type AdminFeature struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// On says the profile has it now.
	On bool `json:"on"`
}

// AdminBackup is a backup of the profile.
type AdminBackup struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// AdminGitToken is what the tab shows of the sandbox's GitHub token. It is
// only ever shown: a token is given to vibe in the terminal, not here.
type AdminGitToken struct {
	Has      bool     `json:"has"`
	Source   string   `json:"source,omitempty"`
	Rules    []string `json:"rules,omitempty"`
	Expiry   string   `json:"expiry,omitempty"`
	Expired  bool     `json:"expired,omitempty"`
	Expiring bool     `json:"expiring,omitempty"`
	// NewTokenURL is GitHub's form for a new token, filled in as far as a
	// URL can; RenewURL the page of tokens to regenerate one on.
	NewTokenURL string `json:"newTokenUrl,omitempty"`
	RenewURL    string `json:"renewUrl,omitempty"`
	// TokenName is what the token is called on GitHub, Days how long the
	// form's token lasts, and Repo the repository to pick there ("" when
	// the folder's remote isn't on GitHub): what the steps the tab gives
	// name.
	TokenName string `json:"tokenName,omitempty"`
	Days      int    `json:"days,omitempty"`
	Repo      string `json:"repo,omitempty"`
	Command   string `json:"command"`
}

// maxJobLines bounds what an action's log keeps.
const maxJobLines = 500

// adminJob is the action running or last run.
type adminJob struct {
	Kind    string   `json:"kind"`
	Running bool     `json:"running"`
	Lines   []string `json:"lines"`
	Error   string   `json:"error,omitempty"`
	// Warning says the action has run far longer than it should.
	Warning string `json:"warning,omitempty"`
	// Cancelled says it was cancelled (the tab's Cancel, or --cancel).
	Cancelled bool  `json:"cancelled,omitempty"`
	Started   int64 `json:"started"`
	Ended     int64 `json:"ended,omitempty"`
}

// admin is the server's side of the tab: the action in hand, and what the
// upgrade check last found.
type admin struct {
	mu       sync.Mutex
	job      *adminJob
	cancel   context.CancelFunc // ends job, while it runs
	upgrades []string
	checked  bool
	checking bool
}

// startUpgradeCheck checks for upgrades in the background, unless a check
// is under way.
func (s *Server) startUpgradeCheck(fresh bool) {
	s.admin.mu.Lock()
	if s.admin.checking {
		s.admin.mu.Unlock()
		return
	}
	s.admin.checking = true
	s.admin.mu.Unlock()
	go func() {
		found := s.cfg.Admin.Upgrades(fresh)
		s.admin.mu.Lock()
		s.admin.upgrades, s.admin.checked, s.admin.checking = found, true, false
		s.admin.mu.Unlock()
	}()
}

func (s *Server) adminState(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Admin == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page has no admin tab"})
		return
	}
	// The status is asked for every few seconds, by every tab showing it;
	// it costs several sbx calls, so they share one.
	status := cached(s.cache, "admin-status", 3*time.Second, s.cfg.Admin.Status)
	s.admin.mu.Lock()
	var job *adminJob
	if s.admin.job != nil {
		copied := *s.admin.job
		copied.Lines = append([]string{}, s.admin.job.Lines...)
		job = &copied
	}
	upgrades := map[string]interface{}{
		"found": append([]string{}, s.admin.upgrades...), "checked": s.admin.checked, "checking": s.admin.checking,
	}
	s.admin.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": status, "job": job, "upgrades": upgrades})
}

func (s *Server) adminCheckUpgrades(w http.ResponseWriter, r *http.Request) {
	s.startUpgradeCheck(true)
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) adminDo(w http.ResponseWriter, r *http.Request) {
	var action AdminAction
	if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected {\"kind\": ...}"})
		return
	}
	destructive, known := adminKinds[action.Kind]
	if !known {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "there is no such action"})
		return
	}
	if err := s.beginAdmin(action, destructive); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// beginAdmin starts action in the background, unless another is running or
// the agent is answering: taking the sandbox from under it would lose what
// it was doing, so it has to be stopped first. What the agent left running
// after answering is ended, quietly, as the next prompt would.
func (s *Server) beginAdmin(action AdminAction, destructive bool) error {
	s.admin.mu.Lock()
	defer s.admin.mu.Unlock()
	if s.admin.job != nil && s.admin.job.Running {
		return errors.New("something is already being done: wait for it to finish")
	}
	if destructive && s.convo.answering() {
		return errors.New("the agent is answering: stop it first")
	}
	if destructive && !s.convo.supersede() {
		return errors.New("the agent is answering: stop it first")
	}
	job := &adminJob{Kind: action.Kind, Running: true, Lines: []string{}, Started: time.Now().UnixMilli()}
	ctx, cancel := context.WithCancel(context.Background())
	s.admin.job, s.admin.cancel = job, cancel
	go s.runAdmin(ctx, action, job)
	return nil
}

// cancelAdmin cancels the action under way, saying whether there was one.
func (s *Server) cancelAdmin() (string, bool) {
	s.admin.mu.Lock()
	defer s.admin.mu.Unlock()
	if s.admin.job == nil || !s.admin.job.Running || s.admin.cancel == nil {
		return "", false
	}
	s.admin.job.Cancelled = true
	s.admin.job.Lines = append(s.admin.job.Lines, "", "cancelling…")
	s.admin.cancel()
	return s.admin.job.Kind, true
}

func (s *Server) adminCancel(w http.ResponseWriter, r *http.Request) {
	kind, ok := s.cancelAdmin()
	writeJSON(w, http.StatusOK, map[string]interface{}{"cancelled": ok, "kind": kind})
}

// Cancel ends whatever the page has under way — the agent's turn, an Admin
// action — as its Stop and Cancel buttons do, and says what it ended (in
// words, for 'vibe --cancel' to print); none, when nothing was running.
func (s *Server) Cancel() []string {
	var ended []string
	if s.convo.stop() {
		ended = append(ended, "the agent's turn")
	}
	if kind, ok := s.cancelAdmin(); ok {
		ended = append(ended, "the Admin tab's "+adminWords[kind])
	}
	return ended
}

// adminWords name the actions for people.
var adminWords = map[string]string{
	AdminStart: "start", AdminStop: "stop", AdminReInit: "rebuild", AdminReCompose: "re-compose",
	AdminRestore: "restore", AdminMemories: "memory save",
}

func (s *Server) runAdmin(ctx context.Context, action AdminAction, job *adminJob) {
	log := func(line string) {
		line = strings.TrimRight(line, "\n")
		s.admin.mu.Lock()
		defer s.admin.mu.Unlock()
		job.Lines = append(job.Lines, line)
		if over := len(job.Lines) - maxJobLines; over > 0 {
			job.Lines = append([]string{}, job.Lines[over:]...)
		}
	}
	slow := time.AfterFunc(adminSlow[action.Kind], func() {
		s.admin.mu.Lock()
		defer s.admin.mu.Unlock()
		if !job.Running {
			return
		}
		job.Warning = fmt.Sprintf("This has been running for %s, far longer than it should — sbx may be stuck. "+
			"vibe is still waiting for it; 'sbx ls' in a terminal shows the sandbox's state.", adminSlow[action.Kind])
		job.Lines = append(job.Lines, "", "⚠ "+job.Warning)
	})
	defer slow.Stop()
	err := s.cfg.Admin.Do(ctx, action, log)
	s.admin.mu.Lock()
	job.Running = false
	job.Ended = time.Now().UnixMilli()
	if err != nil {
		job.Error = err.Error()
	}
	s.admin.mu.Unlock()
	// What the action changed shows at once, not when the cache runs out.
	s.cache.forget("admin-status")
}
