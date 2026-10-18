package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vibe/internal/packagereq"
)

// PackageDesk is where a sandbox's requests for packages are held for the
// user, and decided (see internal/packagereq).
type PackageDesk interface {
	// Poll takes in the requests the agent has made and fetches the ones
	// approved; it is called every so often, on one goroutine.
	Poll(ctx context.Context)
	Pending() []packagereq.Pending
	Allow(keys []string)
	Deny(keys []string)
	DenyAll()
}

// packagesPollEvery is how often the agent's requests are taken in. A
// variable for tests.
var packagesPollEvery = time.Second

// pollPackages takes in the agent's package requests every so often, until
// done is closed. It runs whether the page is open or not: a request the
// user allows by setting waits for nothing.
func (s *Server) pollPackages(done <-chan struct{}, every time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			s.cfg.Packages.Poll(ctx)
		}
	}
}

// packageRequest is a held request as the page shows it.
type packageRequest struct {
	Key       string `json:"key"`
	Ecosystem string `json:"ecosystem"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Framework string `json:"framework,omitempty"`
	Note      string `json:"note,omitempty"`
	Fetching  bool   `json:"fetching,omitempty"`
}

func (s *Server) packageRequests() []packageRequest {
	out := []packageRequest{}
	for _, p := range s.cfg.Packages.Pending() {
		out = append(out, packageRequest{
			Key: p.Key, Ecosystem: p.Request.Ecosystem, ID: p.Request.ID, Version: p.Request.Version,
			Framework: p.Request.Framework, Note: p.Note, Fetching: p.Fetching,
		})
	}
	return out
}

// packagesWaiting says, for the Admin tab's dot, how many requests wait
// for the user; "" when none do.
func (s *Server) packagesWaiting() string {
	if s.cfg.Packages == nil {
		return ""
	}
	waiting := 0
	for _, p := range s.cfg.Packages.Pending() {
		if !p.Fetching {
			waiting++
		}
	}
	switch waiting {
	case 0:
		return ""
	case 1:
		return "1 package request is waiting for your approval — see the Admin tab"
	}
	return fmt.Sprintf("%d package requests are waiting for your approval — see the Admin tab", waiting)
}

func (s *Server) packagesState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"requests": s.packageRequests()})
}

func (s *Server) packagesDecide(decide func(keys []string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Keys []string `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "expected {\"keys\": [...]}")
			return
		}
		decide(body.Keys)
		s.packagesState(w, r)
	}
}

func (s *Server) packagesDenyAll(w http.ResponseWriter, r *http.Request) {
	s.cfg.Packages.DenyAll()
	s.packagesState(w, r)
}
