package companion

import (
	"context"
	"net/http"
	"time"
)

// signInWithin bounds one look at the sign-ins: sbx can wait on a locked system wallet for as long as it is left, and the
// page is shown that it didn't answer rather than waiting with it.
var signInWithin = 20 * time.Second

var signInFeature = feature{func(s *Server) bool { return s.cfg.SignIn != nil },
	"this page has no sign-in to show", 1 << 10}

// signInState asks the host afresh each time: it is read when the Admin tab is opened and when the user presses Check
// now, which wants what is so now.
func (s *Server) signInState(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), signInWithin)
	defer cancel()
	writeJSON(w, http.StatusOK, s.cfg.SignIn(ctx))
}
