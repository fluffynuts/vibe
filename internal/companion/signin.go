package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// signInWithin bounds one look at the sign-ins: sbx can wait on a locked system wallet for as long as it is left, and the
// page is shown that it didn't answer rather than waiting with it.
var signInWithin = 20 * time.Second

var signInFeature = feature{func(s *Server) bool { return s.cfg.SignIn != nil },
	"this page has no sign-in to show", 8 << 10}

// signInState asks the host afresh each time: it is read when the Admin tab is opened and when the user presses Check
// now, which wants what is so now.
func (s *Server) signInState(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), signInWithin)
	defer cancel()
	writeJSON(w, http.StatusOK, s.cfg.SignIn(ctx))
}

// maxAPIKey is the longest key taken: they are about a hundred characters.
const maxAPIKey = 4096

// signInAPIKey gives the host the API key the user typed on the card, to keep in sbx. The key is in nothing said back, and
// is taken out of an error should the host's carry it.
func (s *Server) signInAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `expected {"key": "..."}`)
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" || len(key) > maxAPIKey || strings.ContainsAny(key, "\r\n") {
		writeError(w, http.StatusBadRequest, "expected the key on one line")
		return
	}
	if s.cfg.SignInAPIKey == nil {
		writeError(w, http.StatusNotFound, "this page has no way to store a key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), signInWithin)
	defer cancel()
	if err := s.cfg.SignInAPIKey(ctx, key); err != nil {
		writeError(w, http.StatusInternalServerError, strings.ReplaceAll(err.Error(), key, "…"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"set": true})
}
