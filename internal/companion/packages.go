package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vibe/internal/nugetfetch"
	"vibe/internal/packagereq"
)

// PackageConfig is the nuget.config in the sandbox's folder, which the user
// approves before vibe uses it for a fetch (see nugetfetch.WorkspaceConfig).
type PackageConfig interface {
	// Look is the config as it is now; nil when there's none.
	Look() (*nugetfetch.ConfigInfo, error)
	// Approve approves the content with hash, if it's still the content.
	Approve(hash string) error
}

// PackageDesk is where a sandbox's requests for packages are held for the
// user, and decided (see internal/packagereq).
type PackageDesk interface {
	// Poll takes in the requests the agent has made and fetches the ones
	// approved; it is called every so often, on one goroutine.
	Poll(ctx context.Context)
	Pending() []packagereq.Pending
	// Decide approves the requests with the keys in allow and answers those in notSelected as
	// such; one in neither is still held.
	Decide(allow, notSelected []string)
	// DeclineAll answers every request not yet approved as declined.
	DeclineAll()
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
	Approved  bool   `json:"approved,omitempty"`
	Fetching  bool   `json:"fetching,omitempty"`
}

func (s *Server) packageRequests() []packageRequest {
	out := []packageRequest{}
	for _, p := range s.cfg.Packages.Pending() {
		out = append(out, packageRequest{
			Key: p.Key, Ecosystem: p.Request.Ecosystem, ID: p.Request.ID, Version: p.Request.Version,
			Framework: p.Request.Framework, Note: p.Note, Approved: p.Approved, Fetching: p.Fetching,
		})
	}
	return out
}

// packagesWaitingCount is how many requests wait for the user to decide:
// one approved has been decided, whether it's being fetched yet or not.
func (s *Server) packagesWaitingCount() int {
	if s.cfg.Packages == nil {
		return 0
	}
	waiting := 0
	for _, p := range s.cfg.Packages.Pending() {
		if !p.Approved {
			waiting++
		}
	}
	return waiting
}

// packagesWaiting says, for the Admin tab's dot, how many requests wait
// for the user; "" when none do.
func (s *Server) packagesWaiting() string {
	switch waiting := s.packagesWaitingCount(); waiting {
	case 0:
		return ""
	case 1:
		return "1 package request is waiting for your approval — see the Admin tab"
	default:
		return fmt.Sprintf("%d package requests are waiting for your approval — see the Admin tab", waiting)
	}
}

func (s *Server) packagesState(w http.ResponseWriter, r *http.Request) {
	body := map[string]interface{}{"requests": s.packageRequests(), "config": (*nugetfetch.ConfigInfo)(nil)}
	if s.cfg.PackageConfig != nil {
		info, err := s.cfg.PackageConfig.Look()
		if err != nil {
			body["configProblem"] = "the folder's nuget.config can't be looked at: " + err.Error()
		}
		body["config"] = info
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) packagesApproveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || s.cfg.PackageConfig == nil {
		writeError(w, http.StatusBadRequest, "expected {\"hash\": ...}")
		return
	}
	if err := s.cfg.PackageConfig.Approve(body.Hash); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.packagesState(w, r)
}

// packagesDecide is the user's word on the held requests: the keys approved, and the keys they left out.
func (s *Server) packagesDecide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Allow       []string `json:"allow"`
		NotSelected []string `json:"notSelected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "expected {\"allow\": [...], \"notSelected\": [...]}")
		return
	}
	s.cfg.Packages.Decide(body.Allow, body.NotSelected)
	s.packagesState(w, r)
}

func (s *Server) packagesDeclineAll(w http.ResponseWriter, r *http.Request) {
	s.cfg.Packages.DeclineAll()
	s.packagesState(w, r)
}
