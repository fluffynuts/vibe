package companion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"

	"vibe/internal/review"
)

// A repository's default branch is what origin's HEAD names. When origin
// can't say (there's no origin yet, or nothing pushed), the user can set
// it for the repository, in its git config, and vibe remembers the name to
// suggest it next time (see docs/plan-new-conversation.md).

// defaultBranching wraps the default branch's calls: the same protections
// as reviewing, for the sandbox's folder.
func (s *Server) defaultBranching(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !sameOriginJSON(w, r) {
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		}
		if !s.repositoryHere() {
			writeError(w, http.StatusNotFound, "this sandbox's folder isn't a git repository")
			return
		}
		next(w, r)
	})
}

// defaultBranchState is the folder's default branch, with what to suggest
// for setting one when it can't be told.
func (s *Server) defaultBranchState(w http.ResponseWriter, r *http.Request) {
	mainline, err := s.defaultBranch(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.describeDefaultBranch(mainline))
}

func (s *Server) describeDefaultBranch(m review.Mainline) map[string]interface{} {
	body := map[string]interface{}{"name": m.Name, "set": m.Set, "why": m.Why, "branches": m.Branches}
	if s.cfg.PreferredDefaultBranch != nil {
		body["preferred"] = s.cfg.PreferredDefaultBranch()
	}
	return body
}

// defaultBranchSet sets the folder's default branch, when origin can't say
// which it is, and remembers the name for next time.
func (s *Server) defaultBranchSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "expected a branch name")
		return
	}
	// Anything starting with - would be taken for one of git's options.
	if req.Name == "" || strings.HasPrefix(req.Name, "-") || plumbing.NewBranchReferenceName(req.Name).Validate() != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%q isn't a branch name (no spaces, and no ending in / or .lock, among git's rules).", req.Name))
		return
	}
	ctx := r.Context()
	mainline, err := s.defaultBranch(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if mainline.Name != "" && !mainline.Set {
		writeError(w, http.StatusConflict, "origin says the default branch is "+mainline.Name+", and that's what's used.")
		return
	}
	if err := s.wake(ctx); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if _, err := s.sandboxGit(ctx, gitWithin, "config", review.DefaultBranchKey, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "the default branch couldn't be set: "+err.Error())
		return
	}
	if mainline, err = s.defaultBranch(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := s.describeDefaultBranch(mainline)
	if s.cfg.RememberDefaultBranch != nil {
		if err := s.cfg.RememberDefaultBranch(req.Name); err != nil {
			body["warning"] = "Set for this repository, but it couldn't be remembered for next time: " + err.Error()
		} else {
			body["preferred"] = req.Name
		}
	}
	writeJSON(w, http.StatusOK, body)
}
