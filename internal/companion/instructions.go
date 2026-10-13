package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// The user's instructions to the agent, as the Admin tab edits them: a
// universal file and the profile's own (see internal/instructions). The
// Admin implementation does the work; this is the page's way in.

// AdminInstructions is both files, and what the sandbox's agent does with
// them.
type AdminInstructions struct {
	Files []AdminInstructionsFile `json:"files"`
	// Agent is the sandbox's agent; Applies says it loads these files at
	// all, and CanOptimise that it can be asked for a tidier version.
	Agent       string `json:"agent"`
	Applies     bool   `json:"applies"`
	CanOptimise bool   `json:"canOptimise"`
	// Running says the sandbox is up, which optimising needs.
	Running bool `json:"running"`
}

// AdminInstructionsFile is one of the files.
type AdminInstructionsFile struct {
	Which string `json:"which"` // "universal" or "project"
	Label string `json:"label"`
	Path  string `json:"path"`
	Text  string `json:"text"`
	// Version identifies the text as read, so a save can tell whether the
	// file has changed on disk since.
	Version  string `json:"version"`
	Editable bool   `json:"editable"`
	// Note says why a file isn't editable, or anything else to know.
	Note string `json:"note,omitempty"`
}

// AdminSave is a save the page asks for.
type AdminSave struct {
	Which string `json:"which"`
	Text  string `json:"text"`
	// Base is the text as the page loaded it, and Version its version:
	// what a merge works from, and how a change on disk is noticed.
	Base    string `json:"base"`
	Version string `json:"version"`
	// Resolve settles a change on disk the page was told of: "merge" or
	// "overwrite" ("" saves only if there was none).
	Resolve string `json:"resolve,omitempty"`
}

// AdminSaved is how a save went.
type AdminSaved struct {
	Saved   bool   `json:"saved"`
	Text    string `json:"text,omitempty"`
	Version string `json:"version,omitempty"`
	// Conflict says the file changed on disk since the page loaded it:
	// DiskText and DiskVersion are what's there, CanMerge whether the
	// page's changes merge into it, and Why why not.
	Conflict    bool   `json:"conflict,omitempty"`
	DiskText    string `json:"diskText,omitempty"`
	DiskVersion string `json:"diskVersion,omitempty"`
	CanMerge    bool   `json:"canMerge,omitempty"`
	Why         string `json:"why,omitempty"`
	// The sandboxes the change was given to, those that will get it when
	// they next start, and those it failed for (with why).
	Updated    []string          `json:"updated,omitempty"`
	NotRunning []string          `json:"notRunning,omitempty"`
	Failed     map[string]string `json:"failed,omitempty"`
}

// AdminInstructor is the part of Admin that handles the instructions. An
// Admin that is one gets the tab's editors.
type AdminInstructor interface {
	Instructions() AdminInstructions
	SaveInstructions(save AdminSave) (AdminSaved, error)
	// Optimise asks the agent for a tidier version of text, one of the
	// files (which); ctx ending stops it.
	Optimise(ctx context.Context, which, text string) (string, error)
}

// maxInstructions bounds what the page may send of one file.
const maxInstructions = 256 * 1024

// optimiseLimit is a backstop, not a limit on the work: optimising takes
// seconds, and Cancel is the user's to press. It is there so a stuck sbx or
// agent ends in an error that says so, not a spinner that never stops.
const optimiseLimit = 10 * time.Minute

func (s *Server) instructor(w http.ResponseWriter) (AdminInstructor, bool) {
	in, ok := s.cfg.Admin.(AdminInstructor)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page can't edit the agent's instructions"})
	}
	return in, ok
}

func (s *Server) instructionsState(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Admin == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page has no admin tab"})
		return
	}
	in, ok := s.instructor(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, in.Instructions())
}

func (s *Server) instructionsSave(w http.ResponseWriter, r *http.Request) {
	in, ok := s.instructor(w)
	if !ok {
		return
	}
	var save AdminSave
	if err := json.NewDecoder(r.Body).Decode(&save); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected {\"which\": ..., \"text\": ...}"})
		return
	}
	if save.Which != "universal" && save.Which != "project" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "there is no such file"})
		return
	}
	saved, err := in.SaveInstructions(save)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	status := http.StatusOK
	if saved.Conflict {
		status = http.StatusConflict
	}
	writeJSON(w, status, saved)
}

func (s *Server) instructionsOptimise(w http.ResponseWriter, r *http.Request) {
	in, ok := s.instructor(w)
	if !ok {
		return
	}
	var req struct {
		Which string `json:"which"`
		Text  string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected {\"which\": ..., \"text\": ...}"})
		return
	}
	// The page cancelling the request (its Cancel button) ends it too.
	ctx, cancel := context.WithTimeout(r.Context(), optimiseLimit)
	defer cancel()
	text, err := in.Optimise(ctx, req.Which, req.Text)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "the agent didn't answer within 10 minutes, so vibe stopped waiting — nothing changed. " +
			"sbx or the agent may be stuck: 'sbx ls' shows the sandbox's state, and Stop then Start on this tab restarts it."})
	case err != nil:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"text": text})
	}
}

// editing wraps a call that sends a file's text: as administering, with
// room for the file.
func (s *Server) editing(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Admin == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page has no admin tab"})
			return
		}
		if !sameOriginJSON(w, r) {
			return
		}
		// Room for the text and the base it was loaded as, escaped.
		r.Body = http.MaxBytesReader(w, r.Body, 3*maxInstructions)
		next(w, r)
	})
}
