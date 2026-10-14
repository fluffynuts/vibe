package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Manager is what the Profiles tab works on: every profile and sandbox on
// the machine, not just this page's. Nil means the page has no such tab.
// Everything it does, 'vibe --cleanup' does too.
type Manager interface {
	// Inventory takes stock. It may be slow (it asks sbx), but should give
	// up within seconds, saying why in its Error.
	Inventory() ManageInventory
	// Plan says, line by line, everything deleting what ids pick would
	// delete, by name and path; or why it can't be deleted.
	Plan(ids []string) ([]string, error)
	// Delete deletes what ids pick, saying what it does through log. ctx
	// ending (Cancel, or 'vibe --cancel') ends it.
	Delete(ctx context.Context, ids []string, log func(string)) error
}

// ManageInventory is every profile, with its sandboxes, and the sandboxes
// vibe has no record of.
type ManageInventory struct {
	Profiles []ManageProfile `json:"profiles"`
	Unknown  []ManageUnknown `json:"unknown"`
	Error    string          `json:"error,omitempty"`
}

// ManageProfile is a profile, as the Profiles tab shows it.
type ManageProfile struct {
	// ID is what picks it for deleting.
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Features []string `json:"features"`
	// Flags are what's worth knowing at a glance when deciding whether
	// it's trash.
	Flags []string `json:"flags"`
	// Trash says it's likely trash: Select likely trash ticks it.
	Trash bool `json:"trash"`
	// Kept says why it can't be deleted; "" when it can.
	Kept      string          `json:"kept,omitempty"`
	Sandboxes []ManageSandbox `json:"sandboxes"`
}

// ManageSandbox is a sandbox vibe has a record of.
type ManageSandbox struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Folder string   `json:"folder"`
	Flags  []string `json:"flags"`
	Trash  bool     `json:"trash"`
	Kept   string   `json:"kept,omitempty"`
	// Own is the sandbox this page is for.
	Own bool `json:"own,omitempty"`
}

// ManageUnknown is a sandbox sbx has that vibe has no record of.
type ManageUnknown struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

// manageSlow is far longer than deleting even many sandboxes should take.
const manageSlow = 10 * time.Minute

func (s *Server) manageState(w http.ResponseWriter, r *http.Request) {
	// Asked for every few seconds by each tab showing it, and it asks sbx:
	// they share one.
	inv := cached(s.cache, "manage", 3*time.Second, s.cfg.Manage.Inventory)
	writeJSON(w, http.StatusOK, map[string]interface{}{"inventory": inv, "job": s.jobSnapshot()})
}

// picked reads the IDs a call picks.
func picked(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected {\"ids\": [...]}, picking at least one"})
		return nil, false
	}
	return body.IDs, true
}

func (s *Server) managePlan(w http.ResponseWriter, r *http.Request) {
	ids, ok := picked(w, r)
	if !ok {
		return
	}
	lines, err := s.cfg.Manage.Plan(ids)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"lines": lines})
}

func (s *Server) manageDelete(w http.ResponseWriter, r *http.Request) {
	ids, ok := picked(w, r)
	if !ok {
		return
	}
	// Refused before it starts, with why, rather than failing in the log.
	if _, err := s.cfg.Manage.Plan(ids); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	s.admin.mu.Lock()
	defer s.admin.mu.Unlock()
	if s.admin.job != nil && s.admin.job.Running {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "something is already being done: wait for it to finish"})
		return
	}
	s.startJob("delete", "the Profiles tab's delete", manageSlow, func(ctx context.Context, log func(string)) error {
		return s.cfg.Manage.Delete(ctx, ids, log)
	})
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// jobSnapshot is a copy of the page's job, safe to send.
func (s *Server) jobSnapshot() *adminJob {
	s.admin.mu.Lock()
	defer s.admin.mu.Unlock()
	if s.admin.job == nil {
		return nil
	}
	copied := *s.admin.job
	copied.Lines = append([]string{}, s.admin.job.Lines...)
	return &copied
}

// managed answers only on a page with a Profiles tab.
func (s *Server) managed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Manage == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page has no Profiles tab"})
			return
		}
		next(w, r)
	}
}

// managing wraps a change from the Profiles tab: the same protections as
// administering().
func (s *Server) managing(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Manage == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page has no Profiles tab"})
			return
		}
		if !sameOriginJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		next(w, r)
	})
}
