package companion

import (
	"encoding/json"
	"errors"
	"net/http"
)

// GitAuthorState is the question of giving the agent the user's git author,
// as the page shows it. Phase is "ask" (a dialog the user has to answer),
// "reminder" (a line they can dismiss, after saying "later") or "" (nothing
// to show). Shown is the author that would be given; Push says whether the
// agent can push, which is what makes a commit it makes one the user can
// still take back.
type GitAuthorState struct {
	Phase string `json:"phase,omitempty"`
	Shown string `json:"shown,omitempty"`
	Push  string `json:"push,omitempty"`
}

// GitAuthor is what the page asks of vibe about the agent's git author.
type GitAuthor struct {
	State func() GitAuthorState
	// Decide takes the user's answer: give, leave, later, fix or dismiss.
	// An answer that makes no sense now returns an error wrapping
	// ErrAnswerNotNow.
	Decide func(answer string) error
}

// ErrAnswerNotNow is an answer to a question that isn't being asked (or isn't
// asked that way now): it was answered in another tab, say.
var ErrAnswerNotNow = errors.New("that answer doesn't fit now")

var gitAuthorFeature = feature{func(s *Server) bool { return s.cfg.GitAuthor != nil },
	"this sandbox has no git author to ask about", 1 << 10}

var gitAuthorAnswers = map[string]bool{"give": true, "leave": true, "later": true, "fix": true, "dismiss": true}

func (s *Server) gitAuthorState() GitAuthorState {
	if s.cfg.GitAuthor == nil {
		return GitAuthorState{}
	}
	return s.cfg.GitAuthor.State()
}

func (s *Server) gitAuthorAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !gitAuthorAnswers[req.Answer] {
		writeError(w, http.StatusBadRequest, "expected {\"answer\": \"give\", \"leave\", \"later\", \"fix\" or \"dismiss\"}")
		return
	}
	switch err := s.cfg.GitAuthor.Decide(req.Answer); {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]interface{}{"gitAuthor": s.gitAuthorState()})
	case errors.Is(err, ErrAnswerNotNow):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
